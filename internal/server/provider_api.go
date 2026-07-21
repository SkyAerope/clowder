package server

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/SkyAerope/clowder/internal/app"
)

func registerProviderAPI(mux *http.ServeMux, admin func(http.HandlerFunc) http.HandlerFunc, a *app.App) {
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
		jsonOK(w, map[string]any{
			"manifest":               manifestView(ps),
			"manifest_yaml":          readManifestRaw(ps),
			"models":                 append([]string{}, ps.Models...),
			"keys":                   keyViews(a.Pool.Keys(name)),
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
	registerModelCheckAPI(mux, admin, a)
}
