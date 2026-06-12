package app

import (
	"context"
	"encoding/json"
	"time"

	"github.com/SkyAerope/clowder/internal/provider"
	"github.com/SkyAerope/clowder/internal/rules"
	"github.com/SkyAerope/clowder/internal/store"
)

// ActionContext 执行 do 动作所需的上下文。
type ActionContext struct {
	Rule      *rules.Rule
	Event     *rules.Event
	KeyID     int64  // 当前 attempt 使用的 key；0 = 无
	RequestID int64  // requests 表行 id（capture 关联）
	Provider  string // 实际 provider
	Model     string // canonical
	// 报文（capture 用；上游结果触发才有）
	ReqHeaders, ReqBody, RespHeaders, RespBody string
	StatusCode                                 int
	Fingerprint                                string
}

// RunActions 后台并行执行规则的 do 列表（不阻塞请求主线）。
func (a *App) RunActions(actx *ActionContext) {
	for _, act := range actx.Rule.Do {
		act := act
		go func() {
			if err := a.runAction(act, actx); err != nil {
				a.Log.Warn("rule action failed", "rule", actx.Rule.Name, "action", act.Kind, "error", err)
			}
		}()
	}
}

func (a *App) runAction(act rules.Action, actx *ActionContext) error {
	switch act.Kind {
	case "mark_key":
		if actx.KeyID == 0 {
			return nil
		}
		switch act.MarkStatus {
		case "invalid":
			return a.Pool.MarkInvalid(actx.KeyID)
		case "rate_limited":
			until := time.Now().Add(time.Duration(a.Cfg.Backoff.BaseSeconds) * time.Second)
			k := a.Pool.FindByID(actx.KeyID)
			count := 0
			if k != nil {
				count = k.RateLimitCount + 1
			}
			return a.Pool.MarkRateLimited(actx.KeyID, until, count)
		case "active":
			return a.Pool.MarkActive(actx.KeyID)
		}
	case "backoff_key":
		if actx.KeyID == 0 {
			return nil
		}
		return a.Pool.BackoffKey(actx.KeyID)
	case "mark_model_unhealthy":
		a.Health.MarkModelDown(actx.Provider, actx.Model)
	case "mark_provider_unhealthy":
		a.Health.MarkProviderDown(actx.Provider)
	case "refresh_models":
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		return a.RefreshModels(ctx, actx.Provider)
	case "check_key":
		if actx.KeyID == 0 {
			return nil
		}
		return a.CheckKey(context.Background(), actx.KeyID, act.Chain)
	case "check_keys":
		scope := actx.Provider
		if actx.Rule.When.ScopeProvider != "" {
			scope = actx.Rule.When.ScopeProvider
		}
		return a.CheckKeys(context.Background(), scope, act.Chain)
	case "capture":
		if actx.Event.Type != "upstream" {
			return nil // check/schedule 下 no-op
		}
		a.saveCapture(act, actx)
	}
	return nil
}

// saveCapture 按 fields 裁剪保存抓包（可选脱敏）。
func (a *App) saveCapture(act rules.Action, actx *ActionContext) {
	want := func(f string) bool {
		if len(act.Fields) == 0 {
			return true // 默认全抓
		}
		for _, x := range act.Fields {
			if x == f {
				return true
			}
		}
		return false
	}
	c := &store.Capture{
		RequestID:   actx.RequestID,
		MatchedRule: actx.Rule.Name,
		Fingerprint: actx.Fingerprint,
		Provider:    actx.Provider,
		Model:       actx.Model,
	}
	if want("request_headers") {
		c.RequestHeaders = actx.ReqHeaders
	}
	if want("request_body") {
		c.RequestBody = actx.ReqBody
	}
	if want("response_headers") {
		c.ResponseHeaders = actx.RespHeaders
	}
	if want("response_body") {
		c.ResponseBody = actx.RespBody
	}
	if a.Cfg.Capture.Redact {
		c.RequestHeaders = redactSecrets(c.RequestHeaders)
		c.ResponseHeaders = redactSecrets(c.ResponseHeaders)
		c.Redacted = true
	}
	if _, err := a.Store.InsertCapture(c, a.Cfg.Capture.MaxRecords); err != nil {
		a.Log.Warn("save capture", "error", err)
	}
}

// CheckKey 对单个 key 跑 check 脚本；chain=true 时结果回灌引擎。
func (a *App) CheckKey(ctx context.Context, keyID int64, chain bool) error {
	k := a.Pool.FindByID(keyID)
	if k == nil {
		return nil
	}
	ps := a.Provider(k.Provider)
	if ps == nil || ps.Manifest.Scripts.Check == "" {
		return nil // 无 check 脚本（规则校验阶段已警告）
	}
	res, err := a.Runner.Check(ctx, ps.Manifest.Dir, ps.Manifest.Scripts.Check, k.APIKey,
		time.Duration(ps.Manifest.Scripts.CheckTimeout)*time.Second)
	if err != nil {
		a.Log.Warn("check key", "key", keyID, "error", err)
		return err
	}
	if res.Metadata != nil {
		if b, err := json.Marshal(res.Metadata); err == nil {
			a.Store.UpdateKeyMetadata(keyID, string(b))
		}
	}
	a.Log.Info("check key result", "key", keyID, "provider", k.Provider, "status", res.Status)
	if chain {
		a.FeedCheckResult(k.Provider, keyID, res.Status)
	}
	return nil
}

// CheckKeys 对某 provider（空 = 全部）的所有 key 逐个跑 check。
func (a *App) CheckKeys(ctx context.Context, providerName string, chain bool) error {
	var provs []string
	if providerName != "" {
		provs = []string{providerName}
	} else {
		for name := range a.Providers() {
			provs = append(provs, name)
		}
	}
	for _, p := range provs {
		for _, k := range a.Pool.Keys(p) {
			if k.Status == store.KeyInvalid {
				continue
			}
			a.CheckKey(ctx, k.ID, chain)
		}
	}
	return nil
}

// FeedCheckResult check 结果回灌引擎，触发 when:check 规则（route 忽略）。
func (a *App) FeedCheckResult(providerName string, keyID int64, status string) {
	ev := &rules.Event{Type: "check", Provider: providerName, CheckStatus: status}
	r := a.Engine.Match(ev)
	if r == nil {
		return
	}
	a.RunActions(&ActionContext{Rule: r, Event: ev, KeyID: keyID, Provider: providerName})
}

// fireScheduleRule 定时规则到点：直接执行其 do（无请求主线，route 忽略）。
func (a *App) fireScheduleRule(r *rules.Rule) {
	a.Log.Info("schedule rule fired", "rule", r.Name)
	actx := &ActionContext{
		Rule:     r,
		Event:    &rules.Event{Type: "schedule"},
		Provider: r.When.ScopeProvider,
	}
	a.RunActions(actx)
}

// redactSecrets 脱敏：authorization / api-key 类 header 只留前 8 位。
func redactSecrets(headersJSON string) string {
	if headersJSON == "" {
		return ""
	}
	var m map[string]string
	if err := json.Unmarshal([]byte(headersJSON), &m); err != nil {
		return headersJSON
	}
	for k, v := range m {
		lk := lower(k)
		if lk == "authorization" || lk == "x-api-key" || lk == "api-key" {
			if len(v) > 8 {
				m[k] = v[:8] + "..."
			}
		}
	}
	b, _ := json.Marshal(m)
	return string(b)
}

func lower(s string) string {
	b := []byte(s)
	for i := range b {
		if b[i] >= 'A' && b[i] <= 'Z' {
			b[i] += 32
		}
	}
	return string(b)
}

// RegisterKey 调注册脚本拿新 key（manual 区分手动/自动）。
func (a *App) RegisterKey(ctx context.Context, providerName string, manual bool) (*store.Key, error) {
	ps := a.Provider(providerName)
	if ps == nil {
		return nil, errProviderNotFound(providerName)
	}
	m := ps.Manifest
	if m.KeySource != provider.SourceScript {
		return nil, errNotScriptProvider(providerName)
	}
	start := time.Now()
	res, err := a.Runner.Register(ctx, providerName, m.Dir, m.Scripts.Register,
		time.Duration(m.Scripts.RegisterTimeout)*time.Second, manual)
	a.mu.Lock()
	ps.LastRegisterAt = start
	ps.LastRegisterDuration = time.Since(start)
	if err != nil {
		ps.LastRegisterError = err.Error()
	} else {
		ps.LastRegisterError = ""
	}
	a.mu.Unlock()
	if err != nil {
		return nil, err
	}
	k := &store.Key{Provider: providerName, APIKey: res.APIKey, Status: store.KeyIdle}
	if res.Metadata != nil {
		if e, ok := res.Metadata["email"].(string); ok {
			k.Email = e
		}
		if p, ok := res.Metadata["password"].(string); ok {
			k.Password = p
		}
		if b, err := json.Marshal(res.Metadata); err == nil {
			k.Metadata = string(b)
		}
	}
	if err := a.Pool.AddKey(k); err != nil {
		return nil, err
	}
	a.Log.Info("registered new key", "provider", providerName, "key_id", k.ID)
	return k, nil
}

// AutoRegisterLoop 自动注册循环：可用 key 低于 min_active_keys 且总数低于 max_total_keys 时补号。
func (a *App) AutoRegisterLoop(ctx context.Context) {
	interval := time.Duration(a.Cfg.Pool.RegisterInterval) * time.Second
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if !a.Cfg.Pool.AutoRegister {
				continue
			}
			for name, ps := range a.Providers() {
				if ps.Manifest.KeySource != provider.SourceScript {
					continue
				}
				usableN, total := a.Pool.UsableCount(name)
				if usableN >= a.Cfg.Pool.MinActiveKeys || total >= a.Cfg.Pool.MaxTotalKeys {
					continue
				}
				if in, _ := a.Runner.InBackoff(name); in {
					continue
				}
				go func(n string) {
					if _, err := a.RegisterKey(ctx, n, false); err != nil {
						a.Log.Warn("auto register", "provider", n, "error", err)
					}
				}(name)
			}
		}
	}
}

func errProviderNotFound(name string) error { return &appError{"provider " + name + " not found"} }
func errNotScriptProvider(name string) error {
	return &appError{"provider " + name + " is not script-sourced"}
}

type appError struct{ msg string }

func (e *appError) Error() string { return e.msg }
