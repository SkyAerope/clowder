package server

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/SkyAerope/clowder/internal/app"
	"github.com/SkyAerope/clowder/internal/store"
)

func registerKeyAPI(mux *http.ServeMux, admin func(http.HandlerFunc) http.HandlerFunc, a *app.App) {
	mux.HandleFunc("GET /api/admin/keys", admin(func(w http.ResponseWriter, r *http.Request) {
		provider := r.URL.Query().Get("provider")
		jsonOK(w, keyViews(a.Pool.Keys(provider)))
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
		probe, err := a.CheckKeyWithProbe(r.Context(), id, true)
		if err != nil {
			jsonErr(w, http.StatusBadGateway, err.Error())
			return
		}
		k := a.Pool.FindByID(id)
		if k == nil {
			jsonErr(w, http.StatusNotFound, "key not found")
			return
		}
		jsonOK(w, map[string]any{
			"check_status":   probe.Status,
			"metadata":       k.Metadata,
			"model_response": probe.ModelResponse,
			"ttft_ms":        probe.TTFTMS,
		})
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
}
