package cli

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	"github.com/SkyAerope/clowder/internal/app"
	"github.com/SkyAerope/clowder/internal/config"
	"github.com/SkyAerope/clowder/internal/server"
)

// cmdServe 前台运行（start 的实际工作进程）。
func cmdServe(args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, nil)))
	a, err := app.New(cfg)
	if err != nil {
		return err
	}
	defer a.Close()

	// 写 pid 文件
	pp, err := pidPath()
	if err != nil {
		return err
	}
	os.WriteFile(pp, []byte(strconv.Itoa(os.Getpid())), 0o644)
	defer os.Remove(pp)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	defer stop()

	// SIGHUP = reload
	hup := make(chan os.Signal, 1)
	signal.Notify(hup, syscall.SIGHUP)
	go func() {
		for range hup {
			a.Log.Info("SIGHUP: reloading")
			if cfg, err := config.Load(); err == nil {
				*a.Cfg = *cfg
			}
			if err := a.Reload(); err != nil {
				a.Log.Error("reload failed", "error", err)
			}
		}
	}()

	// 自动注册循环
	go a.AutoRegisterLoop(ctx)

	return server.New(a).ListenAndServe(ctx)
}
