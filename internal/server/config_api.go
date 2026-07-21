package server

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"

	"github.com/SkyAerope/clowder/internal/app"
	"github.com/SkyAerope/clowder/internal/config"
	"github.com/SkyAerope/clowder/internal/models"
	"gopkg.in/yaml.v3"
)

func registerConfigAPI(mux *http.ServeMux, admin func(http.HandlerFunc) http.HandlerFunc, a *app.App) {
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
