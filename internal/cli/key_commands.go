package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/SkyAerope/clowder/internal/app"
	"github.com/SkyAerope/clowder/internal/config"
)

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

func maskKey(k string) string {
	if len(k) <= 12 {
		return k[:min(4, len(k))] + "..."
	}
	return k[:8] + "..." + k[len(k)-4:]
}
