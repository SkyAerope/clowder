// Package health 实现两层健康状态机（provider 级 + model 级），内存为主。
//
// [healthy] ──连接失败─────→ [unhealthy: 30s]
// [healthy] ──全key失败────→ [unhealthy: 60s]
// [unhealthy] ──时间到─────→ [probing]
// [probing] ──1次成功──────→ [healthy]
// [probing] ──1次失败──────→ [unhealthy: 2× backoff, max 5min]
package health

import (
	"sync"
	"time"
)

// 状态。
const (
	Healthy   = "healthy"
	Unhealthy = "unhealthy"
	Probing   = "probing"
)

const (
	connFailDuration    = 30 * time.Second
	allKeysFailDuration = 60 * time.Second
	maxBackoff          = 5 * time.Minute
)

type entry struct {
	status       string
	until        time.Time // unhealthy 到期时间
	backoffCount int
	lastError    time.Time
	lastSuccess  time.Time
	errorCount   int
	lastSlug     string

	// TTFT 指数移动平均
	avgTTFTMs int64
	ttftN     int64
}

// Tracker 管理所有 (provider, native) 与 provider 级（native=""）的健康状态。
type Tracker struct {
	mu   sync.Mutex
	m    map[[2]string]*entry
	save func(PersistedState)
}

// New 创建 Tracker。
func New() *Tracker {
	return NewPersistent(nil, nil)
}

func (t *Tracker) get(provider, model string) *entry {
	k := [2]string{provider, model}
	e, ok := t.m[k]
	if !ok {
		e = &entry{status: Probing}
		t.m[k] = e
	}
	return e
}

// resolve 推进时间相关状态（unhealthy 到期 → probing）。
func (e *entry) resolve(now time.Time) string {
	if e.status == Unhealthy && now.After(e.until) {
		e.status = Probing
	}
	return e.status
}

// Status 返回当前状态（自动推进 unhealthy → probing）。
func (t *Tracker) Status(provider, model string) string {
	t.mu.Lock()
	defer t.mu.Unlock()
	e := t.get(provider, model)
	before := e.status
	status := e.resolve(time.Now())
	if status != before {
		t.persist(provider, model, e)
	}
	return status
}

// Routable 该 (provider, native) 是否参与路由：provider 与 native 都需可用。
func (t *Tracker) Routable(provider, model string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := time.Now()
	providerEntry := t.get(provider, "")
	modelEntry := t.get(provider, model)
	providerBefore := providerEntry.status
	modelBefore := modelEntry.status
	ps := providerEntry.resolve(now)
	ms := modelEntry.resolve(now)
	if ps != providerBefore {
		t.persist(provider, "", providerEntry)
	}
	if ms != modelBefore {
		t.persist(provider, model, modelEntry)
	}
	ok := func(s string) bool { return s == Healthy || s == Probing }
	return ok(ps) && ok(ms)
}

// MarkConnFailure provider 级基础设施故障（DNS / 拒连 / 证书）。
func (t *Tracker) MarkConnFailure(provider, slug string) {
	t.markFailure(provider, "", connFailDuration, slug)
}

// MarkModelDown 标记 provider 下的原生模型故障。
func (t *Tracker) MarkModelDown(provider, model, slug string) {
	t.markFailure(provider, model, allKeysFailDuration, slug)
}

// MarkProviderDown 显式标记 provider 级 unhealthy（规则动作）。
func (t *Tracker) MarkProviderDown(provider, slug string) {
	t.markFailure(provider, "", connFailDuration, slug)
}

func (t *Tracker) markFailure(provider, model string, base time.Duration, slug string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := time.Now()
	e := t.get(provider, model)
	st := e.resolve(now)
	e.errorCount++
	e.lastError = now
	e.lastSlug = slug
	switch st {
	case Probing:
		e.backoffCount++
	default:
		if e.backoffCount == 0 {
			e.backoffCount = 1
		}
	}
	backoff := base * time.Duration(1<<min(e.backoffCount-1, 30))
	if backoff > maxBackoff {
		backoff = maxBackoff
	}
	e.status = Unhealthy
	e.until = now.Add(backoff)
	t.persist(provider, model, e)
}

// MarkSuccess 一次成功：probing/unhealthy → healthy，并记录 TTFT。
func (t *Tracker) MarkSuccess(provider, model string, ttftMs int64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := time.Now()
	for _, e := range []*entry{t.get(provider, ""), t.get(provider, model)} {
		e.status = Healthy
		e.backoffCount = 0
		e.until = time.Time{}
		e.lastSuccess = now
			e.lastSlug = ""
	}
	if ttftMs > 0 {
		e := t.get(provider, model)
		e.ttftN++
		if e.avgTTFTMs == 0 {
			e.avgTTFTMs = ttftMs
		} else {
			// EMA，α=0.3
			e.avgTTFTMs = (e.avgTTFTMs*7 + ttftMs*3) / 10
		}
	}
	t.persist(provider, "", t.get(provider, ""))
	t.persist(provider, model, t.get(provider, model))
}

func (t *Tracker) Ensure(provider, model string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	key := [2]string{provider, model}
	if _, exists := t.m[key]; exists {
		return
	}
	e := t.get(provider, model)
	t.persist(provider, model, e)
}

// AvgTTFT 返回 (provider, native) 的平均 TTFT；无数据返回 (0, false)。
func (t *Tracker) AvgTTFT(provider, model string) (int64, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	e := t.get(provider, model)
	return e.avgTTFTMs, e.ttftN > 0
}

// Snapshot 一条健康快照（API 展示用）。
type Snapshot struct {
	Provider    string `json:"provider"`
	Model       string `json:"model"` // "" = provider 级
	Status      string `json:"status"`
	AvgTTFTMs   int64  `json:"avg_ttft_ms"`
	ErrorCount  int    `json:"error_count"`
	LastError   string `json:"last_error_at,omitempty"`
	LastSuccess string `json:"last_success_at,omitempty"`
	LastSlug    string `json:"last_slug,omitempty"`
}

// Snapshots 导出全部健康状态。
func (t *Tracker) Snapshots() []Snapshot {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := time.Now()
	var out []Snapshot
	for k, e := range t.m {
		before := e.status
		s := Snapshot{
			Provider:   k[0],
			Model:      k[1],
			Status:     e.resolve(now),
			AvgTTFTMs:  e.avgTTFTMs,
			ErrorCount: e.errorCount,
				LastSlug:   e.lastSlug,
		}
		if s.Status != before {
			t.persist(k[0], k[1], e)
		}
		if !e.lastError.IsZero() {
			s.LastError = e.lastError.UTC().Format(time.RFC3339)
		}
		if !e.lastSuccess.IsZero() {
			s.LastSuccess = e.lastSuccess.UTC().Format(time.RFC3339)
		}
		out = append(out, s)
	}
	return out
}
