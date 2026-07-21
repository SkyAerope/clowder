package server

import (
	"net/http"

	"github.com/SkyAerope/clowder/internal/app"
)

// RegisterAPI 挂载 /api/* 接口。
func RegisterAPI(mux *http.ServeMux, a *app.App) {
	public := publicAccess(a)
	admin := adminAccess(a)

	registerSessionAPI(mux, a)
	registerPublicAPI(mux, public, a)
	registerKeyAPI(mux, admin, a)
	registerProviderAPI(mux, admin, a)
	registerActivityAPI(mux, admin, a)
	registerRulesAPI(mux, admin, a)
	registerConfigAPI(mux, admin, a)
}
