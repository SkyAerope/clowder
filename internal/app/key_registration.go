package app

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/SkyAerope/clowder/internal/store"
)

// RegisterKey 调注册脚本拿新 key（manual 区分手动/自动）。
func (a *App) RegisterKey(ctx context.Context, providerName string, manual bool) (*store.Key, error) {
	ps := a.Provider(providerName)
	if ps == nil {
		return nil, errProviderNotFound(providerName)
	}
	m := ps.Manifest
	if m.Scripts.Register == "" {
		return nil, errNoRegisterScript(providerName)
	}
	start := time.Now()
	res, err := a.Runner.Register(ctx, providerName, m.Dir, m.Scripts.Register,
		time.Duration(m.Scripts.RegisterTimeout)*time.Second, manual)
	a.mu.Lock()
	ps.LastRegisterAt = start
	ps.LastRegisterDuration = time.Since(start)
	if err != nil {
		ps.LastRegisterError = err.Error()
	} else {
		ps.LastRegisterError = ""
	}
	a.mu.Unlock()
	if err != nil {
		return nil, err
	}
	k := &store.Key{Provider: providerName, APIKey: res.APIKey, Status: store.KeyIdle}
	if res.Metadata != nil {
		if e, ok := res.Metadata["email"].(string); ok {
			k.Email = e
		}
		if p, ok := res.Metadata["password"].(string); ok {
			k.Password = p
		}
		if b, err := json.Marshal(res.Metadata); err == nil {
			k.Metadata = string(b)
		}
	}
	if err := a.Pool.AddKey(k); err != nil {
		return nil, err
	}
	a.Log.Info("registered new key", "provider", providerName, "key_id", k.ID)
	return k, nil
}

// ImportKeys 将 key 写入数据库并同步加入内存池。
func (a *App) ImportKeys(providerName string, values []string) (added, skipped int, err error) {
	if a.Provider(providerName) == nil {
		return 0, 0, errProviderNotFound(providerName)
	}
	for _, value := range values {
		key := strings.TrimSpace(value)
		if key == "" || strings.HasPrefix(key, "#") {
			skipped++
			continue
		}
		if a.Pool.HasKey(providerName, key) {
			skipped++
			continue
		}
		if err := a.Pool.AddKey(&store.Key{Provider: providerName, APIKey: key, Status: store.KeyIdle}); err != nil {
			return added, skipped, err
		}
		added++
	}
	return added, skipped, nil
}

// AutoRegisterLoop 自动注册循环：可用 key 低于 min_active_keys 且总数低于 max_total_keys 时补号。
func (a *App) AutoRegisterLoop(ctx context.Context) {
	interval := time.Duration(a.Cfg.Pool.RegisterInterval) * time.Second
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if !a.Cfg.Pool.AutoRegister {
				continue
			}
			for name, ps := range a.Providers() {
				if ps.Manifest.Scripts.Register == "" {
					continue
				}
				usableN, total := a.Pool.UsableCount(name)
				if usableN >= a.Cfg.Pool.MinActiveKeys || total >= a.Cfg.Pool.MaxTotalKeys {
					continue
				}
				if in, _ := a.Runner.InBackoff(name); in {
					continue
				}
				go func(n string) {
					if _, err := a.RegisterKey(ctx, n, false); err != nil {
						a.Log.Warn("auto register", "provider", n, "error", err)
					}
				}(name)
			}
		}
	}
}

func errProviderNotFound(name string) error { return &appError{"provider " + name + " not found"} }
func errNoRegisterScript(name string) error {
	return &appError{"provider " + name + " has no register script"}
}

type appError struct{ msg string }

func (e *appError) Error() string { return e.msg }
