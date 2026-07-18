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
	// Models manifest 中声明的 native 模型名
	Models []string
	// NativesByCanonical canonical → natives（本 provider 视角）
	NativesByCanonical map[string][]string
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

	providers           map[string]*ProviderState
	configuredProviders map[string]*ProviderState
	scheduler           *rules.Scheduler
	actionsWG           sync.WaitGroup

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
		Cfg:                 cfg,
		Store:               st,
		Health:              health.New(),
		providers:           map[string]*ProviderState{},
		configuredProviders: map[string]*ProviderState{},
		Log:                 slog.Default(),
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
	configuredStates := map[string]*ProviderState{}
	var manifestRules []*rules.Rule
	for _, m := range manifests {
		ps := &ProviderState{Manifest: m}
		ps.Models = append([]string(nil), m.Models...)
		// 保留注册机运行状态。
		a.mu.RLock()
		if old, ok := a.configuredProviders[m.Name]; ok {
			ps.LastRegisterAt = old.LastRegisterAt
			ps.LastRegisterDuration = old.LastRegisterDuration
			ps.LastRegisterError = old.LastRegisterError
		}
		a.mu.RUnlock()
		configuredStates[m.Name] = ps
		if !m.Enabled {
			continue
		}
		states[m.Name] = ps
		a.Pool.SetStrategy(m.Name, m.KeyStrategy)
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
	rulesRaw, err := os.ReadFile(rulesPath)
	if err != nil {
		return fmt.Errorf("rules.yaml: %w", err)
	}
	userRules, err := rules.ParseRules(rulesRaw, rules.SourceUserFile)
	if err != nil {
		return fmt.Errorf("rules.yaml: %w", err)
	}
	builtins := rules.BuiltinRules()
	if disabled := rules.ParseDisabledBuiltins(rulesRaw); disabled != nil {
		for _, b := range builtins {
			if disabled[b.Name] {
				b.Enabled = false
			}
		}
	}
	all := append(append(manifestRules, userRules...), builtins...)
	engine := rules.NewEngine(all)

	// 4. 重建 canonical 映射 & scheduler
	a.mu.Lock()
	a.Mapping = mapping
	a.providers = states
	a.configuredProviders = configuredStates
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

// rebuildCanonicalLocked 根据当前模型列表重建 canonical → natives 映射（持写锁调用）。
func (a *App) rebuildCanonicalLocked(ps *ProviderState) {
	ps.NativesByCanonical = map[string][]string{}
	for _, native := range ps.Models {
		for _, canonical := range a.Mapping.CanonicalsOf(native) {
			ps.NativesByCanonical[canonical] = appendUniqueString(ps.NativesByCanonical[canonical], native)
		}
	}
}

func appendUniqueString(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

// Providers 返回已启用 provider 的运行态快照。
func (a *App) Providers() map[string]*ProviderState {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return cloneProviderStates(a.providers)
}

// ConfiguredProviders 返回全部已配置 provider，包括已禁用项。
func (a *App) ConfiguredProviders() map[string]*ProviderState {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return cloneProviderStates(a.configuredProviders)
}

func cloneProviderStates(states map[string]*ProviderState) map[string]*ProviderState {
	out := make(map[string]*ProviderState, len(states))
	for k, v := range states {
		manifest := *v.Manifest
		copyState := &ProviderState{
			Manifest:             &manifest,
			Models:               append([]string(nil), v.Models...),
			NativesByCanonical:   map[string][]string{},
			LastRegisterAt:       v.LastRegisterAt,
			LastRegisterDuration: v.LastRegisterDuration,
			LastRegisterError:    v.LastRegisterError,
		}
		for canonical, natives := range v.NativesByCanonical {
			copyState.NativesByCanonical[canonical] = append([]string(nil), natives...)
		}
		out[k] = copyState
	}
	return out
}

// MappingSnapshot 返回当前映射的独立副本。
func (a *App) MappingSnapshot() *models.Mapping {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.Mapping.Clone()
}

// Provider 按名取已启用 provider 状态。
func (a *App) Provider(name string) *ProviderState {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.providers[name]
}

// ConfiguredProvider 按名取 provider 配置，包括已禁用项。
func (a *App) ConfiguredProvider(name string) *ProviderState {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.configuredProviders[name]
}

// DiscoverModels 从 provider 拉取可选模型，不修改运行时或 manifest 配置。
func (a *App) DiscoverModels(ctx context.Context, name string) ([]string, error) {
	ps := a.ConfiguredProvider(name)
	if ps == nil {
		return nil, fmt.Errorf("provider %s not found", name)
	}
	return fetchModelList(ctx, ps.Manifest.BaseURL, a.firstKey(name))
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
	a.actionsWG.Wait()
	a.Store.Close()
}
