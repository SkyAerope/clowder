// Package server 组装 HTTP 服务：/v1 代理 + /api 看板接口 + 静态面板。
package server

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/SkyAerope/clowder/internal/app"
	"github.com/SkyAerope/clowder/internal/proxy"
)

// Server 顶层 HTTP 服务。
type Server struct {
	App   *app.App
	Proxy *proxy.Server
	http  *http.Server
}

// New 构建 Server。
func New(a *app.App) *Server {
	s := &Server{App: a, Proxy: proxy.New(a)}
	return s
}

// Handler 组装路由。
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	s.Proxy.Routes(mux)
	RegisterAPI(mux, s.App)
	RegisterDashboard(mux, s.App)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})
	return mux
}

// ListenAndServe 启动监听（阻塞）。
func (s *Server) ListenAndServe(ctx context.Context) error {
	addr := fmt.Sprintf("%s:%d", s.App.Cfg.BindAddr, s.App.Cfg.Port)
	s.http = &http.Server{
		Addr:              addr,
		Handler:           s.Handler(),
		ReadHeaderTimeout: 30 * time.Second,
		// 不设 WriteTimeout：流式响应可以很长
	}
	go func() {
		<-ctx.Done()
		shutCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		s.http.Shutdown(shutCtx)
	}()
	s.App.Log.Info("clowder listening", "addr", addr)
	err := s.http.ListenAndServe()
	if err == http.ErrServerClosed {
		return nil
	}
	return err
}
