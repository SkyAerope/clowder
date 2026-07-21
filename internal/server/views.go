package server

import (
	"os"
	"path/filepath"
	"time"

	"github.com/SkyAerope/clowder/internal/app"
	"github.com/SkyAerope/clowder/internal/provider"
	"github.com/SkyAerope/clowder/internal/rules"
	"github.com/SkyAerope/clowder/internal/store"
)

// keyView 序列化单个 key（本地单用户工具，返回完整凭据）。
func keyView(k *store.Key) map[string]any {
	v := map[string]any{
		"id":               k.ID,
		"provider":         k.Provider,
		"api_key":          k.APIKey,
		"email":            k.Email,
		"metadata":         k.Metadata,
		"status":           k.Status,
		"rate_limit_count": k.RateLimitCount,
	}
	if k.RateLimitUntil != nil {
		v["rate_limit_until"] = k.RateLimitUntil.UTC().Format(time.RFC3339)
	}
	if k.LastUsedAt != nil {
		v["last_used_at"] = k.LastUsedAt.UTC().Format(time.RFC3339)
	}
	return v
}

func keyViews(keys []*store.Key) []map[string]any {
	out := make([]map[string]any, 0, len(keys))
	for _, k := range keys {
		out = append(out, keyView(k))
	}
	return out
}

func validationCtx(a *app.App) *rules.ValidationContext {
	ctx := &rules.ValidationContext{
		Providers:      map[string]bool{},
		ProviderModels: map[string]map[string]bool{},
	}
	for name, ps := range a.Providers() {
		ctx.Providers[name] = ps.Manifest.Scripts.Check != ""
		ms := map[string]bool{}
		for c := range ps.NativesByCanonical {
			ms[c] = true
		}
		ctx.ProviderModels[name] = ms
	}
	return ctx
}

func ruleViews(rs []*rules.Rule) []map[string]any {
	var out []map[string]any
	for _, r := range rs {
		out = append(out, map[string]any{
			"name":        r.Name,
			"description": r.Description,
			"priority":    r.Priority,
			"source":      r.Source,
			"builtin":     r.Builtin,
			"enabled":     r.Enabled,
			"type":        r.When.Type(),
			"detail":      ruleDetail(r),
		})
	}
	return out
}

// ruleDetail 把规则结构展开成与前端 EditRule 对齐的可视化结构。
func ruleDetail(r *rules.Rule) map[string]any {
	w := &r.When
	body := w.BodyContains
	bodyRegex := false
	if src := w.BodyRegexSrc(); src != "" {
		body, bodyRegex = src, true
	}
	when := map[string]any{
		"provider":       w.Provider,
		"model":          w.Model,
		"status_code":    w.StatusCode,
		"body":           body,
		"body_regex":     bodyRegex,
		"transport":      w.Transport,
		"ttft_over_ms":   w.TTFTOverMs,
		"error":          w.ErrorAny,
		"check_status":   w.CheckStatus,
		"cron":           w.Cron,
		"scope_provider": w.ScopeProvider,
	}
	ifs := make([]map[string]any, 0, len(r.If))
	for _, c := range r.If {
		ifs = append(ifs, map[string]any{"field": c.Field, "op": c.Op, "value": c.Value})
	}
	dos := make([]map[string]any, 0, len(r.Do))
	for _, a := range r.Do {
		dos = append(dos, map[string]any{
			"kind": a.Kind, "mark_status": a.MarkStatus, "chain": a.Chain, "fields": a.Fields,
		})
	}
	route := map[string]any{"mode": "single", "single": r.Route.Single, "race": r.Route.Race}
	if r.Route.IsRace() {
		route["mode"] = "race"
	}
	return map[string]any{"when": when, "if": ifs, "do": dos, "route": route}
}

func manifestView(ps *app.ProviderState) map[string]any {
	m := ps.Manifest
	return map[string]any{
		"name":                m.Name,
		"display_name":        m.DisplayName,
		"base_url":            m.BaseURL,
		"enabled":             m.Enabled,
		"key_strategy":        m.KeyStrategy,
		"priority":            m.Routing.Priority,
		"weight":              m.Routing.Weight,
		"hedge":               m.Routing.Hedge,
		"has_register":        m.Scripts.Register != "",
		"has_check":           m.Scripts.Check != "",
		"default_check_model": m.DefaultCheckModel,
	}
}

// readManifestRaw 读取 provider 目录下 manifest.yaml 原文。
func readManifestRaw(ps *app.ProviderState) string {
	data, err := os.ReadFile(filepath.Join(ps.Manifest.Dir, "manifest.yaml"))
	if err != nil {
		return ""
	}
	return string(data)
}

// validateManifestYAML 在临时目录解析校验 manifest（含默认值填充与字段校验）。
func validateManifestYAML(text string) error {
	tmp, err := os.MkdirTemp("", "clowder-manifest-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	if err := os.WriteFile(filepath.Join(tmp, "manifest.yaml"), []byte(text), 0o600); err != nil {
		return err
	}
	_, err = provider.LoadManifest(tmp)
	return err
}

func timeOrNull(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t.UTC().Format(time.RFC3339)
}
