package app

import (
	"math/rand"
	"sort"
)

// Candidate 一个可路由的 (provider, native model) 组合。
type Candidate struct {
	Provider            string `json:"provider"`
	ProviderDisplayName string `json:"provider_display_name"`
	Native              string `json:"native"` // provider 原生模型名
	Canonical           string `json:"canonical"`
	Priority            int    `json:"priority"`
	Weight              int    `json:"weight"`
	TTFTMs              int64  `json:"ttft_ms"`
	HasTTFT             bool   `json:"has_ttft"`
	Hedge               bool   `json:"hedge"`
}

// CandidatesFor 返回支持指定 canonical 的健康候选，按设计排序：
// priority 小者优先 → 同优先级：无 TTFT 数据的探索优先 → 有数据按 TTFT（权重扰动）。
func (a *App) CandidatesFor(canonical string) []Candidate {
	a.mu.RLock()
	defer a.mu.RUnlock()
	var out []Candidate
	for name, ps := range a.providers {
		for _, native := range ps.NativesByCanonical[canonical] {
			if a.Health.Routable(name, native) {
				out = append(out, a.candidate(name, ps, canonical, native))
			}
		}
	}
	sortCandidates(out)
	return out
}

// CandidatesForProvider 返回指定 provider 对 canonical 的健康候选及该映射是否存在。
func (a *App) CandidatesForProvider(canonical, providerName string) ([]Candidate, bool) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	ps := a.providers[providerName]
	if ps == nil {
		return nil, false
	}
	natives := ps.NativesByCanonical[canonical]
	if len(natives) == 0 {
		return nil, false
	}
	var out []Candidate
	for _, native := range natives {
		if a.Health.Routable(providerName, native) {
			out = append(out, a.candidate(providerName, ps, canonical, native))
		}
	}
	sortCandidates(out)
	return out, true
}

// CandidatesForNative 返回提供精确原生模型名的全部健康候选及该模型是否存在。
func (a *App) CandidatesForNative(native string) ([]Candidate, bool) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	var out []Candidate
	found := false
	for providerName, ps := range a.providers {
		if !providerHasNative(ps, native) {
			continue
		}
		found = true
		canonical := a.primaryCanonical(native)
		if a.Health.Routable(providerName, native) {
			out = append(out, a.candidate(providerName, ps, canonical, native))
		}
	}
	sortCandidates(out)
	return out, found
}

// CandidatesForNativeProvider 返回指定 provider 对精确原生模型名的健康候选及该模型是否存在。
func (a *App) CandidatesForNativeProvider(native, providerName string) ([]Candidate, bool) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	ps := a.providers[providerName]
	if ps == nil {
		return nil, false
	}
	if !providerHasNative(ps, native) {
		return nil, false
	}
	canonical := a.primaryCanonical(native)
	if !a.Health.Routable(providerName, native) {
		return nil, true
	}
	return []Candidate{a.candidate(providerName, ps, canonical, native)}, true
}

func providerHasNative(ps *ProviderState, native string) bool {
	for _, model := range ps.Models {
		if model == native {
			return true
		}
	}
	return false
}

func (a *App) primaryCanonical(native string) string {
	if canonicals := a.Mapping.CanonicalsOf(native); len(canonicals) > 0 {
		return canonicals[0]
	}
	return native
}

func (a *App) candidate(providerName string, ps *ProviderState, canonical, native string) Candidate {
	c := Candidate{
		Provider:            providerName,
		ProviderDisplayName: ps.Manifest.DisplayName,
		Native:              native,
		Canonical:           canonical,
		Priority:            ps.Manifest.Routing.Priority,
		Weight:              ps.Manifest.Routing.Weight,
		Hedge:               ps.Manifest.Routing.Hedge,
	}
	c.TTFTMs, c.HasTTFT = a.Health.AvgTTFT(providerName, native)
	return c
}

// CandidatesAuto auto 模式：所有健康 (provider, native) 组合按 TTFT 排序。
func (a *App) CandidatesAuto() []Candidate {
	a.mu.RLock()
	defer a.mu.RUnlock()
	var out []Candidate
	for name, ps := range a.providers {
		seen := map[string]bool{}
		for _, native := range ps.Models {
			if seen[native] || !a.Health.Routable(name, native) {
				continue
			}
			seen[native] = true
			out = append(out, a.candidate(name, ps, a.primaryCanonical(native), native))
		}
	}
	// auto：纯按 TTFT（冷启动语义见 sortByTTFT）
	sortByTTFT(out)
	return out
}

// sortCandidates 具体模型路由：priority → (探索优先 / TTFT)。
func sortCandidates(cs []Candidate) {
	// 同优先级组内先 weight 加权随机扰动，再稳定排序
	rand.Shuffle(len(cs), func(i, j int) { cs[i], cs[j] = cs[j], cs[i] })
	sort.SliceStable(cs, func(i, j int) bool {
		if cs[i].Priority != cs[j].Priority {
			return cs[i].Priority < cs[j].Priority
		}
		return ttftLess(cs[i], cs[j])
	})
	// weight：同优先级且都有 TTFT 时，权重大者小概率前移（轻量实现：
	// shuffle + stable sort 已给同分组随机性，weight 作为 TTFT 平手时的次序）
}

// sortByTTFT auto 模式排序 + 冷启动：无数据健康者优先（探索），全无数据时随机。
func sortByTTFT(cs []Candidate) {
	rand.Shuffle(len(cs), func(i, j int) { cs[i], cs[j] = cs[j], cs[i] })
	sort.SliceStable(cs, func(i, j int) bool { return ttftLess(cs[i], cs[j]) })
}

// ttftLess 冷启动语义：无数据者排前（探索优先，尽快补齐数据）；都有数据按 TTFT 升序。
func ttftLess(a, b Candidate) bool {
	if a.HasTTFT != b.HasTTFT {
		return !a.HasTTFT // 无数据优先
	}
	if !a.HasTTFT {
		return false // 都无数据：保持 shuffle 后的随机序
	}
	if a.TTFTMs != b.TTFTMs {
		return a.TTFTMs < b.TTFTMs
	}
	return a.Weight > b.Weight
}

// CanonicalModels 返回当前至少有一个健康 provider 支持的 canonical 列表（/v1/models 用）。
func (a *App) CanonicalModels() []string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	seen := map[string]bool{}
	var out []string
	for name, ps := range a.providers {
		for canonical, natives := range ps.NativesByCanonical {
			if seen[canonical] {
				continue
			}
			for _, native := range natives {
				if a.Health.Routable(name, native) {
					seen[canonical] = true
					out = append(out, canonical)
					break
				}
			}
		}
	}
	sort.Strings(out)
	return out
}

// ModelNames 返回当前可用 canonical、配置别名和 provider 原生模型名的展平列表。
func (a *App) ModelNames() []string {
	a.mu.RLock()
	defer a.mu.RUnlock()

	seen := map[string]bool{}
	var out []string
	for providerName, ps := range a.providers {
		for _, native := range ps.Models {
			if native == "" || seen[native] || !a.Health.Routable(providerName, native) {
				continue
			}
			seen[native] = true
			out = append(out, native)
		}
	}
	for _, canonical := range a.Mapping.Canonicals() {
		available := false
		for name, ps := range a.providers {
			for _, native := range ps.NativesByCanonical[canonical] {
				if a.Health.Routable(name, native) {
					available = true
					break
				}
			}
			if available {
				break
			}
		}
		if !available {
			continue
		}

		if !seen[canonical] {
			seen[canonical] = true
			out = append(out, canonical)
		}
	}
	sort.Strings(out)
	return out
}
