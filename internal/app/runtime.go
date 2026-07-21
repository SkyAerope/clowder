// Package app 组装运行态：providers、模型映射、key 池、健康追踪、规则引擎。
package app

import (
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/SkyAerope/clowder/internal/config"
	"github.com/SkyAerope/clowder/internal/health"
	"github.com/SkyAerope/clowder/internal/models"
	"github.com/SkyAerope/clowder/internal/pool"
	"github.com/SkyAerope/clowder/internal/rules"
	"github.com/SkyAerope/clowder/internal/script"
	"github.com/SkyAerope/clowder/internal/store"
)

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
	healthSnapshots, err := st.ListHealth()
	if err != nil {
		st.Close()
		return nil, fmt.Errorf("load provider health: %w", err)
	}
	persistedHealth := make([]health.PersistedState, len(healthSnapshots))
	for i, snapshot := range healthSnapshots {
		persistedHealth[i] = health.PersistedState{
			Provider: snapshot.Provider, Model: snapshot.Model, Status: snapshot.Status,
			AvgTTFTMs: snapshot.AvgTTFTMs, ErrorCount: snapshot.ErrorCount,
			BackoffCount: snapshot.BackoffCount, UnhealthyUntil: snapshot.UnhealthyUntil,
			LastErrorAt: snapshot.LastErrorAt, LastSuccessAt: snapshot.LastSuccessAt,
			LastRule: snapshot.LastRule,
		}
	}
	a := &App{
		Cfg:                 cfg,
		Store:               st,
		providers:           map[string]*ProviderState{},
		configuredProviders: map[string]*ProviderState{},
		Log:                 slog.Default(),
	}
	a.Health = health.NewPersistent(persistedHealth, func(state health.PersistedState) {
		err := st.SaveHealth(store.HealthSnapshot{
			Provider: state.Provider, Model: state.Model, Status: state.Status,
			AvgTTFTMs: state.AvgTTFTMs, ErrorCount: state.ErrorCount,
			BackoffCount: state.BackoffCount, UnhealthyUntil: state.UnhealthyUntil,
			LastErrorAt: state.LastErrorAt, LastSuccessAt: state.LastSuccessAt,
			LastRule: state.LastRule,
		})
		if err != nil {
			a.Log.Warn("save provider health", "provider", state.Provider, "model", state.Model, "error", err)
		}
	})
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
