package app

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/SkyAerope/clowder/internal/config"
	"github.com/SkyAerope/clowder/internal/models"
	"github.com/SkyAerope/clowder/internal/provider"
	"github.com/SkyAerope/clowder/internal/rules"
)

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
			if disabled[b.Slug] {
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
		a.Health.Ensure(ps.Manifest.Name, "")
		for _, native := range ps.Models {
			a.Health.Ensure(ps.Manifest.Name, native)
		}
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
