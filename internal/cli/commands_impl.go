package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/SkyAerope/clowder/internal/app"
	"github.com/SkyAerope/clowder/internal/config"
	"github.com/SkyAerope/clowder/internal/server"
)

func pidPath() (string, error) {
	d, err := config.Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "clowder.pid"), nil
}

func readPID() (int, error) {
	p, err := pidPath()
	if err != nil {
		return 0, err
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(string(data))
}

func processAlive(pid int) bool {
	return syscall.Kill(pid, 0) == nil
}

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

// cmdStart 后台启动：自我 fork 出 serve 进程。
func cmdStart(args []string) error {
	if pid, err := readPID(); err == nil && processAlive(pid) {
		return fmt.Errorf("clowder already running (pid %d)", pid)
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	d, err := config.Dir()
	if err != nil {
		return err
	}
	os.MkdirAll(d, 0o755)
	logFile, err := os.OpenFile(filepath.Join(d, "clowder.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer logFile.Close()
	cmd := exec.Command(exe, "serve")
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	fmt.Printf("clowder started (pid %d), log: %s\n", cmd.Process.Pid, logFile.Name())
	return nil
}

// cmdStop 按 pid 文件停止。
func cmdStop(args []string) error {
	pid, err := readPID()
	if err != nil {
		return fmt.Errorf("not running (no pid file)")
	}
	if !processAlive(pid) {
		p, _ := pidPath()
		os.Remove(p)
		return fmt.Errorf("not running (stale pid %d removed)", pid)
	}
	if err := syscall.Kill(pid, syscall.SIGTERM); err != nil {
		return err
	}
	fmt.Printf("sent SIGTERM to pid %d\n", pid)
	return nil
}

// cmdReload 给运行中的进程发 SIGHUP。
func cmdReload(args []string) error {
	pid, err := readPID()
	if err != nil || !processAlive(pid) {
		return fmt.Errorf("not running")
	}
	if err := syscall.Kill(pid, syscall.SIGHUP); err != nil {
		return err
	}
	fmt.Println("reload signal sent")
	return nil
}

// cmdStatus 状态一览（直读本地数据，不要求服务在线）。
func cmdStatus(args []string) error {
	pid, err := readPID()
	running := err == nil && processAlive(pid)
	if running {
		fmt.Printf("clowder: running (pid %d)\n", pid)
	} else {
		fmt.Println("clowder: stopped")
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	fmt.Printf("listen: %s:%d\n", cfg.BindAddr, cfg.Port)
	a, err := app.New(cfg)
	if err != nil {
		return err
	}
	defer a.Close()
	fmt.Println("\nproviders:")
	for name, ps := range a.ConfiguredProviders() {
		usable, total := a.Pool.UsableCount(name)
		streaming := cfg.Routing.UpstreamStreaming
		if ps.Manifest.UpstreamStreaming != nil {
			streaming = *ps.Manifest.UpstreamStreaming
		}
		fmt.Printf("  %-20s enabled=%v streaming=%v keys=%d/%d models=%d\n",
			name, ps.Manifest.Enabled, streaming, usable, total, len(ps.Models))
	}
	return nil
}

// cmdRegister 手动注册 N 个 key。
func cmdRegister(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: clowder register <provider> [N]")
	}
	providerName := args[0]
	n := 1
	if len(args) >= 2 {
		v, err := strconv.Atoi(args[1])
		if err != nil || v < 1 {
			return fmt.Errorf("invalid count %q", args[1])
		}
		n = v
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	a, err := app.New(cfg)
	if err != nil {
		return err
	}
	defer a.Close()
	for i := 0; i < n; i++ {
		fmt.Printf("registering %s (%d/%d)...\n", providerName, i+1, n)
		k, err := a.RegisterKey(context.Background(), providerName, true)
		if err != nil {
			return err
		}
		fmt.Printf("  ok: key id=%d %s\n", k.ID, maskKey(k.APIKey))
	}
	return nil
}

func cmdImport(args []string) error {
	if len(args) != 2 {
		return fmt.Errorf("usage: clowder import <provider> <file>")
	}
	data, err := os.ReadFile(args[1])
	if err != nil {
		return err
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	a, err := app.New(cfg)
	if err != nil {
		return err
	}
	defer a.Close()
	added, skipped, err := a.ImportKeys(args[0], strings.Split(string(data), "\n"))
	if err != nil {
		return err
	}
	fmt.Printf("imported %d keys, skipped %d\n", added, skipped)
	return nil
}

// cmdKeys 列出 key。
func cmdKeys(args []string) error {
	providerName := ""
	if len(args) >= 1 {
		providerName = args[0]
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	a, err := app.New(cfg)
	if err != nil {
		return err
	}
	defer a.Close()
	keys, err := a.Store.ListKeys(providerName)
	if err != nil {
		return err
	}
	if len(keys) == 0 {
		fmt.Println("no keys")
		return nil
	}
	fmt.Printf("%-5s %-15s %-14s %-24s %s\n", "ID", "PROVIDER", "STATUS", "KEY", "META")
	for _, k := range keys {
		meta := ""
		if k.Metadata != "" {
			var m map[string]any
			if json.Unmarshal([]byte(k.Metadata), &m) == nil {
				if v, ok := m["tokens_used"]; ok {
					meta = fmt.Sprintf("used=%v", v)
				}
			}
		}
		fmt.Printf("%-5d %-15s %-14s %-24s %s\n", k.ID, k.Provider, k.Status, maskKey(k.APIKey), meta)
	}
	return nil
}

// cmdProviders 列出 provider 及状态。
func cmdProviders(args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	a, err := app.New(cfg)
	if err != nil {
		return err
	}
	defer a.Close()
	fmt.Printf("%-20s %-10s %-8s %s\n", "NAME", "STRATEGY", "KEYS", "MODELS")
	for name, ps := range a.Providers() {
		usable, total := a.Pool.UsableCount(name)
		fmt.Printf("%-20s %-10s %d/%-6d %d\n",
			name, ps.Manifest.KeyStrategy, usable, total, len(ps.Models))
	}
	return nil
}

func maskKey(k string) string {
	if len(k) <= 12 {
		return k[:min(4, len(k))] + "..."
	}
	return k[:8] + "..." + k[len(k)-4:]
}
