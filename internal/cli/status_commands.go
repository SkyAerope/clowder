package cli

import (
	"fmt"

	"github.com/SkyAerope/clowder/internal/app"
	"github.com/SkyAerope/clowder/internal/config"
)

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
