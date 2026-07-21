package app

import (
	"context"
	"time"

	"github.com/SkyAerope/clowder/internal/rules"
)

// ActionContext 执行 do 动作所需的上下文。
type ActionContext struct {
	Rule      *rules.Rule
	Event     *rules.Event
	KeyID     int64  // 当前 attempt 使用的 key；0 = 无
	RequestID int64  // requests 表行 id（capture 关联）
	Provider  string // 实际 provider
	Model     string // canonical
	Native    string // provider 原生模型名
	// 报文（capture 用；上游结果触发才有）
	ReqHeaders, ReqBody, RespHeaders, RespBody string
	StatusCode                                 int
	Fingerprint                                string
}

// RunActions 同步提交 mark_key，其他 do 动作后台并行执行。
func (a *App) RunActions(actx *ActionContext) {
	for _, act := range actx.Rule.Do {
		if act.Kind == "mark_key" {
			if err := a.runAction(act, actx); err != nil {
				a.Log.Warn("rule action failed", "slug", actx.Rule.Slug, "action", act.Kind, "error", err)
			}
			continue
		}
		act := act
		a.actionsWG.Add(1)
		go func() {
			defer a.actionsWG.Done()
			if err := a.runAction(act, actx); err != nil {
				a.Log.Warn("rule action failed", "slug", actx.Rule.Slug, "action", act.Kind, "error", err)
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
		if actx.Native != "" {
			a.Health.MarkModelDown(actx.Provider, actx.Native, actx.Rule.Slug)
		}
	case "mark_provider_unhealthy":
		a.Health.MarkProviderDown(actx.Provider, actx.Rule.Slug)
	case "check_key":
		if actx.KeyID == 0 {
			return nil
		}
		_, err := a.CheckKey(context.Background(), actx.KeyID, act.Chain)
		return err
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
	a.Log.Info("schedule rule fired", "slug", r.Slug)
	actx := &ActionContext{
		Rule:     r,
		Event:    &rules.Event{Type: "schedule"},
		Provider: r.When.ScopeProvider,
	}
	a.RunActions(actx)
}
