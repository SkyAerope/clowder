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
	"github.com/SkyAerope/clowder/internal/rules"
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
		for name, ps := range a.Providers() {
			usable, total := a.Pool.UsableCount(name)
			var ttftSum, ttftN int64
			for canonical := range ps.NativeByCanonical {
				if t, ok := a.Health.AvgTTFT(name, canonical); ok {
					ttftSum += t
					ttftN++
				}
			}
			var avg int64
			if ttftN > 0 {
				avg = ttftSum / ttftN
			}
			out = append(out, pv{
				Name: name, DisplayName: ps.Manifest.DisplayName,
				Status: a.Health.Status(name, ""), ActiveKeys: usable, TotalKeys: total,
				AvgTTFTMs: avg, Priority: ps.Manifest.Routing.Priority,
				ModelCount: len(ps.Models),
			})
		}
		jsonOK(w, out)
	}))

	mux.HandleFunc("GET /api/models", public(func(w http.ResponseWriter, r *http.Request) {
		// 模型路由图：canonical → 各 provider 的 native 名 + TTFT + 是否最优
		type edge struct {
			Provider string `json:"provider"`
			Native   string `json:"native"`
			TTFTMs   int64  `json:"ttft_ms"`
			HasTTFT  bool   `json:"has_ttft"`
			Healthy  bool   `json:"healthy"`
			Best     bool   `json:"best"`
		}
		out := map[string][]edge{}
		for _, canonical := range a.Mapping.Canonicals() {
			cands := a.CandidatesFor(canonical)
			best := ""
			if len(cands) > 0 {
				best = cands[0].Provider
			}
			var edges []edge
			for name, ps := range a.Providers() {
				native, ok := ps.NativeByCanonical[canonical]
				if !ok {
					continue
				}
				t, has := a.Health.AvgTTFT(name, canonical)
				edges = append(edges, edge{
					Provider: name, Native: native, TTFTMs: t, HasTTFT: has,
					Healthy: a.Health.Routable(name, canonical),
					Best:    name == best,
				})
			}
			if len(edges) > 0 {
				out[canonical] = edges
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
		jsonOK(w, keys)
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
		// 一键 check：同步执行返回结果，同时 chain 回灌规则引擎
		if err := a.CheckKey(r.Context(), id, true); err != nil {
			jsonErr(w, http.StatusBadGateway, err.Error())
			return
		}
		k := a.Pool.FindByID(id)
		jsonOK(w, k)
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

	mux.HandleFunc("GET /api/admin/providers/{name}", admin(func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("name")
		ps := a.Provider(name)
		if ps == nil {
			jsonErr(w, http.StatusNotFound, "provider not found")
			return
		}
		keys, _ := a.Store.ListKeys(name)
		jsonOK(w, map[string]any{
			"manifest":               manifestView(ps),
			"models":                 ps.Models,
			"models_fetched_at":      timeOrNull(ps.ModelsFetchedAt),
			"keys":                   keys,
			"last_register_at":       timeOrNull(ps.LastRegisterAt),
			"last_register_duration": ps.LastRegisterDuration.Milliseconds(),
			"last_register_error":    ps.LastRegisterError,
		})
	}))

	mux.HandleFunc("POST /api/admin/providers/{name}/refresh-models", admin(func(w http.ResponseWriter, r *http.Request) {
		if err := a.RefreshModels(r.Context(), r.PathValue("name")); err != nil {
			jsonErr(w, http.StatusBadGateway, err.Error())
			return
		}
		jsonOK(w, map[string]any{"ok": true})
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

func validationCtx(a *app.App) *rules.ValidationContext {
	ctx := &rules.ValidationContext{
		Providers:      map[string]bool{},
		ProviderModels: map[string]map[string]bool{},
	}
	for name, ps := range a.Providers() {
		ctx.Providers[name] = ps.Manifest.Scripts.Check != ""
		ms := map[string]bool{}
		for c := range ps.NativeByCanonical {
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
			"type":        r.When.Type(),
			"specificity": r.Specificity(),
		})
	}
	return out
}

func manifestView(ps *app.ProviderState) map[string]any {
	m := ps.Manifest
	return map[string]any{
		"name":         m.Name,
		"display_name": m.DisplayName,
		"base_url":     m.BaseURL,
		"enabled":      m.Enabled,
		"key_source":   m.KeySource,
		"key_strategy": m.KeyStrategy,
		"priority":     m.Routing.Priority,
		"weight":       m.Routing.Weight,
		"hedge":        m.Routing.Hedge,
		"has_check":    m.Scripts.Check != "",
	}
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
