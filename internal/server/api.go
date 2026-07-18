package server

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/SkyAerope/clowder/internal/app"
	"github.com/SkyAerope/clowder/internal/config"
	"github.com/SkyAerope/clowder/internal/models"
	"github.com/SkyAerope/clowder/internal/provider"
	"github.com/SkyAerope/clowder/internal/rules"
	"github.com/SkyAerope/clowder/internal/store"
	"gopkg.in/yaml.v3"
)

// sessions 内存 session 池（admin 登录）。
var sessions = struct {
	sync.Mutex
	m map[string]time.Time
}{m: map[string]time.Time{}}

const sessionCookie = "clowder_session"
const sessionTTL = 24 * time.Hour

func newSession() string {
	b := make([]byte, 16)
	rand.Read(b)
	tok := hex.EncodeToString(b)
	sessions.Lock()
	sessions.m[tok] = time.Now().Add(sessionTTL)
	sessions.Unlock()
	return tok
}

func validSession(r *http.Request) bool {
	c, err := r.Cookie(sessionCookie)
	if err != nil {
		return false
	}
	sessions.Lock()
	defer sessions.Unlock()
	exp, ok := sessions.m[c.Value]
	if !ok || time.Now().After(exp) {
		delete(sessions.m, c.Value)
		return false
	}
	return true
}

// RegisterAPI 挂载 /api/* 接口。
func RegisterAPI(mux *http.ServeMux, a *app.App) {
	// 公开看板数据（public 关闭且 admin 开启时需登录）
	public := func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if !a.Cfg.Dashboard.Public {
				if !a.Cfg.Dashboard.Admin || !validSession(r) {
					jsonErr(w, http.StatusUnauthorized, "login required")
					return
				}
			}
			next(w, r)
		}
	}
	// 管理接口
	admin := func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if !a.Cfg.Dashboard.Admin {
				jsonErr(w, http.StatusForbidden, "admin panel disabled")
				return
			}
			if !validSession(r) {
				jsonErr(w, http.StatusUnauthorized, "login required")
				return
			}
			next(w, r)
		}
	}

	// ---- 认证 ----
	mux.HandleFunc("POST /api/login", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			AdminKey string `json:"admin_key"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		if !a.Cfg.Dashboard.Admin ||
			subtle.ConstantTimeCompare([]byte(body.AdminKey), []byte(a.Cfg.Dashboard.AdminKey)) != 1 {
			jsonErr(w, http.StatusUnauthorized, "invalid admin key")
			return
		}
		tok := newSession()
		http.SetCookie(w, &http.Cookie{
			Name: sessionCookie, Value: tok, Path: "/",
			HttpOnly: true, SameSite: http.SameSiteLaxMode,
			MaxAge: int(sessionTTL.Seconds()),
		})
		jsonOK(w, map[string]any{"ok": true})
	})
	mux.HandleFunc("POST /api/logout", func(w http.ResponseWriter, r *http.Request) {
		if c, err := r.Cookie(sessionCookie); err == nil {
			sessions.Lock()
			delete(sessions.m, c.Value)
			sessions.Unlock()
		}
		http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", MaxAge: -1})
		jsonOK(w, map[string]any{"ok": true})
	})
	mux.HandleFunc("GET /api/session", func(w http.ResponseWriter, r *http.Request) {
		jsonOK(w, map[string]any{
			"admin":  a.Cfg.Dashboard.Admin && validSession(r),
			"public": a.Cfg.Dashboard.Public,
		})
	})

	// ---- 公开看板数据（绝不返回 key/email/password）----
	mux.HandleFunc("GET /api/providers", public(func(w http.ResponseWriter, r *http.Request) {
		type pv struct {
			Name        string `json:"name"`
			DisplayName string `json:"display_name"`
			Status      string `json:"status"`
			ActiveKeys  int    `json:"active_keys"`
			TotalKeys   int    `json:"total_keys"`
			AvgTTFTMs   int64  `json:"avg_ttft_ms"`
			Priority    int    `json:"priority"`
			ModelCount  int    `json:"model_count"`
		}
		var out []pv
		for name, ps := range a.ConfiguredProviders() {
			usable, total := a.Pool.UsableCount(name)
			var ttftSum, ttftN int64
			seenModels := map[string]bool{}
			for _, native := range ps.Models {
				if seenModels[native] {
					continue
				}
				seenModels[native] = true
				if t, ok := a.Health.AvgTTFT(name, native); ok {
					ttftSum += t
					ttftN++
				}
			}
			var avg int64
			if ttftN > 0 {
				avg = ttftSum / ttftN
			}
			status := "disabled"
			if ps.Manifest.Enabled {
				status = a.Health.Status(name, "")
			}
			out = append(out, pv{
				Name: name, DisplayName: ps.Manifest.DisplayName,
				Status: status, ActiveKeys: usable, TotalKeys: total,
				AvgTTFTMs: avg, Priority: ps.Manifest.Routing.Priority,
				ModelCount: len(ps.Models),
			})
		}
		jsonOK(w, out)
	}))

	mux.HandleFunc("GET /api/models", public(func(w http.ResponseWriter, r *http.Request) {
		// 模型路由图：canonical → 各 provider 的 native 名；空 canonical 表示未映射。
		type edge struct {
			Provider            string `json:"provider"`
			ProviderDisplayName string `json:"provider_display_name"`
			Native              string `json:"native"`
			TTFTMs              int64  `json:"ttft_ms"`
			HasTTFT             bool   `json:"has_ttft"`
			Healthy             bool   `json:"healthy"`
			Best                bool   `json:"best"`
		}
		out := map[string][]edge{}
		providers := a.Providers()
		mapping := a.MappingSnapshot()
		for _, canonical := range mapping.Canonicals() {
			cands := a.CandidatesFor(canonical)
			bestProvider, bestNative := "", ""
			if len(cands) > 0 {
				bestProvider, bestNative = cands[0].Provider, cands[0].Native
			}
			var edges []edge
			for name, ps := range providers {
				for _, native := range ps.NativesByCanonical[canonical] {
					t, has := a.Health.AvgTTFT(name, native)
					edges = append(edges, edge{
						Provider: name, ProviderDisplayName: ps.Manifest.DisplayName,
						Native: native, TTFTMs: t, HasTTFT: has,
						Healthy: a.Health.Routable(name, native),
						Best:    name == bestProvider && native == bestNative,
					})
				}
			}
			if len(edges) > 0 {
				out[canonical] = edges
			}
		}
		for name, ps := range providers {
			seenModels := map[string]bool{}
			for _, native := range ps.Models {
				if seenModels[native] || len(mapping.CanonicalsOf(native)) > 0 {
					continue
				}
				seenModels[native] = true
				t, has := a.Health.AvgTTFT(name, native)
				out[""] = append(out[""], edge{
					Provider:            name,
					ProviderDisplayName: ps.Manifest.DisplayName,
					Native:              native,
					TTFTMs:              t,
					HasTTFT:             has,
					Healthy:             a.Health.Routable(name, native),
				})
			}
		}
		jsonOK(w, out)
	}))

	mux.HandleFunc("GET /api/auto", public(func(w http.ResponseWriter, r *http.Request) {
		cands := a.CandidatesAuto()
		if len(cands) > 3 {
			cands = cands[:3]
		}
		jsonOK(w, cands)
	}))

	mux.HandleFunc("GET /api/health", public(func(w http.ResponseWriter, r *http.Request) {
		jsonOK(w, a.Health.Snapshots())
	}))

	// ---- 管理接口 ----
	mux.HandleFunc("GET /api/admin/keys", admin(func(w http.ResponseWriter, r *http.Request) {
		provider := r.URL.Query().Get("provider")
		keys, err := a.Store.ListKeys(provider)
		if err != nil {
			jsonErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		jsonOK(w, keyViews(keys))
	}))

	mux.HandleFunc("POST /api/admin/keys/{id}/disable", admin(func(w http.ResponseWriter, r *http.Request) {
		id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err := a.Pool.MarkInvalid(id); err != nil {
			jsonErr(w, http.StatusBadRequest, err.Error())
			return
		}
		jsonOK(w, map[string]any{"ok": true})
	}))
	mux.HandleFunc("POST /api/admin/keys/{id}/enable", admin(func(w http.ResponseWriter, r *http.Request) {
		id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err := a.Pool.MarkActive(id); err != nil {
			jsonErr(w, http.StatusBadRequest, err.Error())
			return
		}
		jsonOK(w, map[string]any{"ok": true})
	}))
	mux.HandleFunc("POST /api/admin/keys/{id}/check", admin(func(w http.ResponseWriter, r *http.Request) {
		id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
		// check：同步执行返回结果，同时 chain 回灌规则引擎
		status, err := a.CheckKey(r.Context(), id, true)
		if err != nil {
			jsonErr(w, http.StatusBadGateway, err.Error())
			return
		}
		k := a.Pool.FindByID(id)
		if k == nil {
			jsonErr(w, http.StatusNotFound, "key not found")
			return
		}
		v := keyView(k)
		v["check_status"] = status
		jsonOK(w, v)
	}))
	mux.HandleFunc("POST /api/admin/keys/{id}/status", admin(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Status string `json:"status"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || (body.Status != store.KeyActive && body.Status != store.KeyIdle && body.Status != store.KeyInvalid) {
			jsonErr(w, http.StatusBadRequest, "invalid key status")
			return
		}
		id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err := a.Pool.SetStatus(id, body.Status); err != nil {
			jsonErr(w, http.StatusBadRequest, err.Error())
			return
		}
		jsonOK(w, map[string]any{"ok": true})
	}))
	mux.HandleFunc("DELETE /api/admin/keys/{id}", admin(func(w http.ResponseWriter, r *http.Request) {
		id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err := a.Pool.DeleteKey(id); err != nil {
			jsonErr(w, http.StatusBadRequest, err.Error())
			return
		}
		jsonOK(w, map[string]any{"ok": true})
	}))

	mux.HandleFunc("POST /api/admin/register", admin(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Provider string `json:"provider"`
			Count    int    `json:"count"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		if body.Count <= 0 {
			body.Count = 1
		}
		var registered int
		var lastErr error
		for i := 0; i < body.Count; i++ {
			if _, err := a.RegisterKey(r.Context(), body.Provider, true); err != nil {
				lastErr = err
				break
			}
			registered++
		}
		resp := map[string]any{"registered": registered}
		if lastErr != nil {
			resp["error"] = lastErr.Error()
		}
		jsonOK(w, resp)
	}))

	mux.HandleFunc("POST /api/admin/providers/{name}/keys", admin(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Keys string `json:"keys"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			jsonErr(w, http.StatusBadRequest, err.Error())
			return
		}
		added, skipped, err := a.ImportKeys(r.PathValue("name"), strings.Split(body.Keys, "\n"))
		if err != nil {
			jsonErr(w, http.StatusBadRequest, err.Error())
			return
		}
		jsonOK(w, map[string]any{"added": added, "skipped": skipped})
	}))

	mux.HandleFunc("GET /api/admin/providers/{name}", admin(func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("name")
		ps := a.ConfiguredProvider(name)
		if ps == nil {
			jsonErr(w, http.StatusNotFound, "provider not found")
			return
		}
		keys, _ := a.Store.ListKeys(name)
		jsonOK(w, map[string]any{
			"manifest":               manifestView(ps),
			"manifest_yaml":          readManifestRaw(ps),
			"models":                 ps.Models,
			"keys":                   keyViews(keys),
			"last_register_at":       timeOrNull(ps.LastRegisterAt),
			"last_register_duration": ps.LastRegisterDuration.Milliseconds(),
			"last_register_error":    ps.LastRegisterError,
		})
	}))

	mux.HandleFunc("PUT /api/admin/providers/{name}/manifest", admin(func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("name")
		ps := a.ConfiguredProvider(name)
		if ps == nil {
			jsonErr(w, http.StatusNotFound, "provider not found")
			return
		}
		var body struct {
			YAML string `json:"yaml"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			jsonErr(w, http.StatusBadRequest, err.Error())
			return
		}
		dir := ps.Manifest.Dir
		// 校验：写入临时目录后用 LoadManifest 解析（含默认值填充与字段校验）
		if err := validateManifestYAML(body.YAML); err != nil {
			jsonErr(w, http.StatusBadRequest, "manifest.yaml invalid: "+err.Error())
			return
		}
		if err := os.WriteFile(filepath.Join(dir, "manifest.yaml"), []byte(body.YAML), 0o600); err != nil {
			jsonErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		if err := a.Reload(); err != nil {
			jsonErr(w, http.StatusInternalServerError, "saved but reload failed: "+err.Error())
			return
		}
		jsonOK(w, map[string]any{"ok": true})
	}))

	mux.HandleFunc("POST /api/admin/providers/{name}/discover-models", admin(func(w http.ResponseWriter, r *http.Request) {
		models, err := a.DiscoverModels(r.Context(), r.PathValue("name"))
		if err != nil {
			jsonErr(w, http.StatusBadGateway, err.Error())
			return
		}
		jsonOK(w, map[string]any{"models": models})
	}))

	mux.HandleFunc("GET /api/admin/requests", admin(func(w http.ResponseWriter, r *http.Request) {
		limit, offset := queryInt(r, "limit", 50), queryInt(r, "offset", 0)
		logs, err := a.Store.ListRequests(limit, offset)
		if err != nil {
			jsonErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		jsonOK(w, logs)
	}))
	mux.HandleFunc("GET /api/admin/trace/{id}", admin(func(w http.ResponseWriter, r *http.Request) {
		logs, err := a.Store.ListTrace(r.PathValue("id"))
		if err != nil {
			jsonErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		jsonOK(w, logs)
	}))

	mux.HandleFunc("GET /api/admin/errors", admin(func(w http.ResponseWriter, r *http.Request) {
		groups, err := a.Store.AggregateErrors(r.URL.Query().Get("rule"), queryInt(r, "limit", 100))
		if err != nil {
			jsonErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		jsonOK(w, groups)
	}))
	mux.HandleFunc("GET /api/admin/captures", admin(func(w http.ResponseWriter, r *http.Request) {
		caps, err := a.Store.ListCaptures(r.URL.Query().Get("rule"), r.URL.Query().Get("fingerprint"), queryInt(r, "limit", 20))
		if err != nil {
			jsonErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		jsonOK(w, caps)
	}))

	// ---- 规则管理 ----
	mux.HandleFunc("GET /api/admin/rules", admin(func(w http.ResponseWriter, r *http.Request) {
		dir, _ := config.Dir()
		raw, _ := os.ReadFile(filepath.Join(dir, "rules.yaml"))
		warns := rules.Validate(a.Engine.Rules(), validationCtx(a))
		jsonOK(w, map[string]any{
			"yaml":     string(raw),
			"rules":    ruleViews(a.Engine.Rules()),
			"warnings": warns,
		})
	}))
	mux.HandleFunc("PUT /api/admin/rules", admin(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			YAML string `json:"yaml"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		// 保存前解析校验（含 body_regex 编译）
		if _, err := rules.ParseRules([]byte(body.YAML), rules.SourceUserFile); err != nil {
			jsonErr(w, http.StatusBadRequest, err.Error())
			return
		}
		dir, _ := config.Dir()
		if err := os.WriteFile(filepath.Join(dir, "rules.yaml"), []byte(body.YAML), 0o644); err != nil {
			jsonErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		if err := a.Reload(); err != nil {
			jsonErr(w, http.StatusInternalServerError, "saved but reload failed: "+err.Error())
			return
		}
		jsonOK(w, map[string]any{"ok": true})
	}))
	mux.HandleFunc("GET /api/admin/rules/presets", admin(func(w http.ResponseWriter, r *http.Request) {
		existing := map[string]bool{}
		for _, ru := range a.Engine.Rules() {
			existing[ru.Name] = true
		}
		type pview struct {
			Rule   map[string]any `json:"rule"`
			YAML   string         `json:"yaml"`
			Exists bool           `json:"exists"`
		}
		var out []pview
		var doc struct {
			Rules []map[string]any `yaml:"rules"`
		}
		yaml.Unmarshal([]byte(rules.PresetRulesYAML), &doc)
		for _, rm := range doc.Rules {
			name, _ := rm["name"].(string)
			y, _ := yaml.Marshal([]map[string]any{rm})
			out = append(out, pview{Rule: rm, YAML: string(y), Exists: existing[name]})
		}
		jsonOK(w, out)
	}))

	// ---- 配置管理 ----
	mux.HandleFunc("GET /api/admin/config", admin(func(w http.ResponseWriter, r *http.Request) {
		dir, _ := config.Dir()
		cfgRaw, _ := os.ReadFile(filepath.Join(dir, "config.yaml"))
		modelsRaw, _ := os.ReadFile(filepath.Join(dir, "models.yaml"))
		jsonOK(w, map[string]any{
			"config_yaml": string(cfgRaw),
			"models_yaml": string(modelsRaw),
		})
	}))
	mux.HandleFunc("PUT /api/admin/config", admin(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			ConfigYAML *string `json:"config_yaml"`
			ModelsYAML *string `json:"models_yaml"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		dir, _ := config.Dir()
		if body.ConfigYAML != nil {
			var test config.Config
			if err := yaml.Unmarshal([]byte(*body.ConfigYAML), &test); err != nil {
				jsonErr(w, http.StatusBadRequest, "config.yaml invalid: "+err.Error())
				return
			}
			if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(*body.ConfigYAML), 0o600); err != nil {
				jsonErr(w, http.StatusInternalServerError, err.Error())
				return
			}
		}
		if body.ModelsYAML != nil {
			tmp := filepath.Join(dir, "models.yaml.check")
			os.WriteFile(tmp, []byte(*body.ModelsYAML), 0o644)
			_, err := models.Load(tmp)
			os.Remove(tmp)
			if err != nil {
				jsonErr(w, http.StatusBadRequest, "models.yaml invalid: "+err.Error())
				return
			}
			if err := os.WriteFile(filepath.Join(dir, "models.yaml"), []byte(*body.ModelsYAML), 0o644); err != nil {
				jsonErr(w, http.StatusInternalServerError, err.Error())
				return
			}
		}
		// 重载 config（仅热字段生效）+ 全量 reload
		if cfg, err := config.Load(); err == nil {
			*a.Cfg = *cfg
		}
		if err := a.Reload(); err != nil {
			jsonErr(w, http.StatusInternalServerError, "saved but reload failed: "+err.Error())
			return
		}
		jsonOK(w, map[string]any{"ok": true})
	}))

	mux.HandleFunc("POST /api/admin/reload", admin(func(w http.ResponseWriter, r *http.Request) {
		if cfg, err := config.Load(); err == nil {
			*a.Cfg = *cfg
		}
		if err := a.Reload(); err != nil {
			jsonErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		jsonOK(w, map[string]any{"ok": true})
	}))
}

// keyView 序列化单个 key（本地单用户工具，返回完整凭据）。
func keyView(k *store.Key) map[string]any {
	v := map[string]any{
		"id":               k.ID,
		"provider":         k.Provider,
		"api_key":          k.APIKey,
		"email":            k.Email,
		"password":         k.Password,
		"status":           k.Status,
		"rate_limit_count": k.RateLimitCount,
		"created_at":       k.CreatedAt.UTC().Format(time.RFC3339),
		"metadata":         k.Metadata,
	}
	if k.RateLimitUntil != nil {
		v["rate_limit_until"] = k.RateLimitUntil.UTC().Format(time.RFC3339)
	}
	if k.FirstRequestAt != nil {
		v["first_request_at"] = k.FirstRequestAt.UTC().Format(time.RFC3339)
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

func queryInt(r *http.Request, key string, def int) int {
	v := r.URL.Query().Get(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		return def
	}
	return n
}

func jsonOK(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func jsonErr(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	fmt.Fprintf(w, `{"error":%q}`, strings.ReplaceAll(msg, `"`, `'`))
}
