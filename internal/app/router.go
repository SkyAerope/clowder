package app

import (
	"math/rand"
	"sort"
)

// Candidate 一个可路由的 (provider, native model) 组合。
type Candidate struct {
	Provider  string
	Native    string // provider 原生模型名
	Canonical string
	Priority  int
	Weight    int
	TTFTMs    int64
	HasTTFT   bool
	Hedge     bool
}

// CandidatesFor 返回支持指定 canonical 的健康候选，按设计排序：
// priority 小者优先 → 同优先级：无 TTFT 数据的探索优先 → 有数据按 TTFT（权重扰动）。
func (a *App) CandidatesFor(canonical string) []Candidate {
	a.mu.RLock()
	defer a.mu.RUnlock()
	var out []Candidate
	for name, ps := range a.providers {
		native, ok := ps.NativeByCanonical[canonical]
		if !ok {
			continue
		}
		if !a.Health.Routable(name, canonical) {
			continue
		}
		c := Candidate{
			Provider:  name,
			Native:    native,
			Canonical: canonical,
			Priority:  ps.Manifest.Routing.Priority,
			Weight:    ps.Manifest.Routing.Weight,
			Hedge:     ps.Manifest.Routing.Hedge,
		}
		c.TTFTMs, c.HasTTFT = a.Health.AvgTTFT(name, canonical)
		out = append(out, c)
	}
	sortCandidates(out)
	return out
}

// CandidatesAuto auto 模式：所有健康 (provider, canonical) 组合按 TTFT 排序。
func (a *App) CandidatesAuto() []Candidate {
	a.mu.RLock()
	defer a.mu.RUnlock()
	var out []Candidate
	for name, ps := range a.providers {
		for canonical, native := range ps.NativeByCanonical {
			if !a.Health.Routable(name, canonical) {
				continue
			}
			c := Candidate{
				Provider:  name,
				Native:    native,
				Canonical: canonical,
				Priority:  ps.Manifest.Routing.Priority,
				Weight:    ps.Manifest.Routing.Weight,
				Hedge:     ps.Manifest.Routing.Hedge,
			}
			c.TTFTMs, c.HasTTFT = a.Health.AvgTTFT(name, canonical)
			out = append(out, c)
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
		for canonical := range ps.NativeByCanonical {
			if seen[canonical] {
				continue
			}
			if a.Health.Routable(name, canonical) {
				seen[canonical] = true
				out = append(out, canonical)
			}
		}
	}
	sort.Strings(out)
	return out
}
