// Package app 组装运行态：providers、模型映射、key 池、健康追踪、规则引擎。
package app

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/SkyAerope/clowder/internal/config"
	"github.com/SkyAerope/clowder/internal/health"
	"github.com/SkyAerope/clowder/internal/models"
	"github.com/SkyAerope/clowder/internal/pool"
	"github.com/SkyAerope/clowder/internal/provider"
	"github.com/SkyAerope/clowder/internal/rules"
	"github.com/SkyAerope/clowder/internal/script"
	"github.com/SkyAerope/clowder/internal/store"
)

// ProviderState 是 provider 的运行态（manifest + 过滤后的模型列表 + 映射）。
type ProviderState struct {
	Manifest *provider.Manifest
	// Models 黑白名单过滤后的 native 模型名
	Models []string
	// NativeByCanonical canonical → native（本 provider 视角）
	NativeByCanonical map[string]string
	// ModelsFetchedAt 上次成功拉取 /models 的时间（零值 = 未拉到，用的白名单 fallback）
	ModelsFetchedAt time.Time
	// 注册机状态
	LastRegisterAt       time.Time
	LastRegisterDuration time.Duration
	LastRegisterError    string
}

// App 是全局运行态容器。
type App struct {
	mu sync.RWMutex

	Cfg     *config.Config
	Store   *store.Store
	Pool    *pool.Pool
	Health  *health.Tracker
	Runner  *script.Runner
	Mapping *models.Mapping
	Engine  *rules.Engine

	providers map[string]*ProviderState
	scheduler *rules.Scheduler

	Log *slog.Logger
}

// New 加载全部组件。
func New(cfg *config.Config) (*App, error) {
	dir, err := config.Dir()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	st, err := store.Open(dir)
	if err != nil {
		return nil, err
	}
	a := &App{
		Cfg:       cfg,
		Store:     st,
		Health:    health.New(),
		providers: map[string]*ProviderState{},
		Log:       slog.Default(),
	}
	a.Runner = script.NewRunner(
		time.Duration(cfg.Pool.RegisterBackoffBaseSeconds)*time.Second,
		time.Duration(cfg.Pool.RegisterBackoffMaxSeconds)*time.Second,
	)
	a.Pool, err = pool.New(st,
		time.Duration(cfg.Backoff.BaseSeconds)*time.Second,
		time.Duration(cfg.Backoff.MaxSeconds)*time.Second)
	if err != nil {
		return nil, err
	}
	if err := a.Reload(); err != nil {
		return nil, err
	}
	return a, nil
}

// Reload 重新扫描 providers、models.yaml、rules.yaml（热加载入口）。
func (a *App) Reload() error {
	dir, err := config.Dir()
	if err != nil {
		return err
	}
	// 1. models.yaml
	mapping, err := models.Load(filepath.Join(dir, "models.yaml"))
	if err != nil {
		return fmt.Errorf("models.yaml: %w", err)
	}
	// 2. providers
	pdir, err := a.Cfg.ProvidersPath()
	if err != nil {
		return err
	}
	os.MkdirAll(pdir, 0o755)
	manifests, scanErrs := provider.ScanDir(pdir)
	for _, e := range scanErrs {
		a.Log.Warn("provider scan", "error", e)
	}
	states := map[string]*ProviderState{}
	var manifestRules []*rules.Rule
	for _, m := range manifests {
		if !m.Enabled {
			continue
		}
		ps := &ProviderState{Manifest: m}
		// 保留旧状态里的模型列表（reload 不丢已拉取数据）
		a.mu.RLock()
		if old, ok := a.providers[m.Name]; ok {
			ps.Models = old.Models
			ps.ModelsFetchedAt = old.ModelsFetchedAt
			ps.LastRegisterAt = old.LastRegisterAt
			ps.LastRegisterDuration = old.LastRegisterDuration
			ps.LastRegisterError = old.LastRegisterError
		}
		a.mu.RUnlock()
		if ps.Models == nil {
			ps.Models = m.FilterModels(nil) // 冷启动先用白名单 fallback
		}
		states[m.Name] = ps
		a.Pool.SetStrategy(m.Name, m.KeyStrategy)
		// static keys
		if m.KeySource == provider.SourceStatic {
			keys, err := m.StaticKeys()
			if err != nil {
				a.Log.Warn("load static keys", "provider", m.Name, "error", err)
			}
			for _, k := range keys {
				if !a.Pool.HasKey(m.Name, k) {
					if err := a.Pool.AddKey(&store.Key{Provider: m.Name, APIKey: k, Status: store.KeyIdle}); err != nil {
						a.Log.Warn("add static key", "provider", m.Name, "error", err)
					}
				}
			}
		}
		// manifest 规则
		rs, err := rules.ParseManifestRules(m.Rules, m.Name)
		if err != nil {
			a.Log.Warn("manifest rules", "provider", m.Name, "error", err)
		} else {
			manifestRules = append(manifestRules, rs...)
		}
	}
	// 3. rules.yaml
	rulesPath := filepath.Join(dir, "rules.yaml")
	if err := rules.EnsureUserRulesFile(rulesPath); err != nil {
		return err
	}
	userRules, err := rules.LoadUserRules(rulesPath)
	if err != nil {
		return fmt.Errorf("rules.yaml: %w", err)
	}
	all := append(append(manifestRules, userRules...), rules.BuiltinRules()...)
	engine := rules.NewEngine(all)

	// 4. 重建 canonical 映射 & scheduler
	a.mu.Lock()
	a.Mapping = mapping
	a.providers = states
	a.Engine = engine
	for _, ps := range states {
		a.rebuildCanonicalLocked(ps)
	}
	if a.scheduler != nil {
		a.scheduler.Stop()
	}
	a.mu.Unlock()

	sched, err := rules.NewScheduler(all, a.fireScheduleRule)
	if err != nil {
		return fmt.Errorf("schedule rules: %w", err)
	}
	a.mu.Lock()
	a.scheduler = sched
	a.mu.Unlock()
	sched.Start()
	return nil
}

// rebuildCanonicalLocked 根据当前模型列表重建 canonical → native 映射（持写锁调用）。
func (a *App) rebuildCanonicalLocked(ps *ProviderState) {
	ps.NativeByCanonical = map[string]string{}
	for _, native := range ps.Models {
		if c, ok := a.Mapping.CanonicalOf(native); ok {
			if _, exists := ps.NativeByCanonical[c]; !exists {
				ps.NativeByCanonical[c] = native
			}
		}
	}
}

// Providers 返回 provider 运行态快照。
func (a *App) Providers() map[string]*ProviderState {
	a.mu.RLock()
	defer a.mu.RUnlock()
	out := make(map[string]*ProviderState, len(a.providers))
	for k, v := range a.providers {
		out[k] = v
	}
	return out
}

// Provider 按名取 provider 状态。
func (a *App) Provider(name string) *ProviderState {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.providers[name]
}

// RefreshModels 实时拉取某 provider 的 /models 并应用黑白名单。
func (a *App) RefreshModels(ctx context.Context, name string) error {
	ps := a.Provider(name)
	if ps == nil {
		return fmt.Errorf("provider %s not found", name)
	}
	fetched, err := fetchModelList(ctx, ps.Manifest.BaseURL, a.firstKey(name))
	a.mu.Lock()
	defer a.mu.Unlock()
	if err != nil {
		a.Log.Warn("fetch models failed, fallback to whitelist", "provider", name, "error", err)
		ps.Models = ps.Manifest.FilterModels(nil)
	} else {
		ps.Models = ps.Manifest.FilterModels(fetched)
		ps.ModelsFetchedAt = time.Now()
	}
	a.rebuildCanonicalLocked(ps)
	return err
}

// RefreshAllModels 启动时 / 定期刷新所有 provider 模型列表。
func (a *App) RefreshAllModels(ctx context.Context) {
	for name := range a.Providers() {
		if err := a.RefreshModels(ctx, name); err != nil {
			a.Log.Warn("refresh models", "provider", name, "error", err)
		}
	}
}

// firstKey 取该 provider 任一可用 key 用于拉模型列表（无 key 返回空 = 匿名请求）。
func (a *App) firstKey(name string) string {
	k, err := a.Pool.Pick(name, nil)
	if err != nil {
		return ""
	}
	return k.APIKey
}

// fetchModelList GET {base}/v1/models（base 已含 /v1 时不重复拼）。
func fetchModelList(ctx context.Context, baseURL, apiKey string) ([]string, error) {
	url := JoinURL(baseURL, "/v1/models")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("GET %s: %d %s", url, resp.StatusCode, string(body))
	}
	var parsed struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, err
	}
	out := make([]string, 0, len(parsed.Data))
	for _, d := range parsed.Data {
		out = append(out, d.ID)
	}
	return out, nil
}

// JoinURL 拼接 base_url 与 /v1/xxx 路径；base 已以 /v1 结尾时去重。
func JoinURL(base, path string) string {
	base = strings.TrimRight(base, "/")
	if strings.HasSuffix(base, "/v1") {
		path = strings.TrimPrefix(path, "/v1")
	}
	return base + path
}

// Close 释放资源。
func (a *App) Close() {
	a.mu.Lock()
	if a.scheduler != nil {
		a.scheduler.Stop()
	}
	a.mu.Unlock()
	a.Store.Close()
}
