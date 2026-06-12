package server

import (
	"io/fs"
	"net/http"

	"github.com/SkyAerope/clowder/internal/app"
	"github.com/SkyAerope/clowder/web"
)

// RegisterDashboard 挂载 Web 面板静态文件（Go embed）。
// dashboard.public 与 dashboard.admin 都关闭时不提供 UI。
func RegisterDashboard(mux *http.ServeMux, a *app.App) {
	if !a.Cfg.Dashboard.Public && !a.Cfg.Dashboard.Admin {
		return
	}
	dist, err := fs.Sub(web.Dist, "dist")
	if err != nil {
		return
	}
	fileServer := http.FileServer(http.FS(dist))
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		// SPA fallback：非文件路径一律回 index.html
		if r.URL.Path != "/" {
			if _, err := fs.Stat(dist, r.URL.Path[1:]); err != nil {
				r.URL.Path = "/"
			}
		}
		fileServer.ServeHTTP(w, r)
	})
}
