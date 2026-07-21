package app

import (
	"time"

	"github.com/SkyAerope/clowder/internal/models"
	"github.com/SkyAerope/clowder/internal/provider"
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
