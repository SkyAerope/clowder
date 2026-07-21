package server

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"

	"github.com/SkyAerope/clowder/internal/app"
	"github.com/SkyAerope/clowder/internal/config"
	"github.com/SkyAerope/clowder/internal/rules"
	"gopkg.in/yaml.v3"
)

func registerRulesAPI(mux *http.ServeMux, admin func(http.HandlerFunc) http.HandlerFunc, a *app.App) {
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
}
