package server

import (
	"net/http"

	"github.com/SkyAerope/clowder/internal/app"
)

func registerActivityAPI(mux *http.ServeMux, admin func(http.HandlerFunc) http.HandlerFunc, a *app.App) {
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
}
