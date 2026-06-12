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
	status      string
	until       time.Time     // unhealthy 到期时间
	backoff     time.Duration // 当前退避档位
	lastError   time.Time
	lastSuccess time.Time
	errorCount  int

	// TTFT 指数移动平均
	avgTTFTMs int64
	ttftN     int64
}

// Tracker 管理所有 (provider, model) 与 provider 级（model=""）的健康状态。
type Tracker struct {
	mu sync.Mutex
	m  map[[2]string]*entry
}

// New 创建 Tracker。
func New() *Tracker {
	return &Tracker{m: map[[2]string]*entry{}}
}

func (t *Tracker) get(provider, model string) *entry {
	k := [2]string{provider, model}
	e, ok := t.m[k]
	if !ok {
		e = &entry{status: Healthy}
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
	return t.get(provider, model).resolve(time.Now())
}

// Routable 该 (provider, model) 是否参与路由：provider 级与 model 级都得是 healthy / probing。
func (t *Tracker) Routable(provider, model string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := time.Now()
	ps := t.get(provider, "").resolve(now)
	ms := t.get(provider, model).resolve(now)
	ok := func(s string) bool { return s == Healthy || s == Probing }
	return ok(ps) && ok(ms)
}

// MarkConnFailure provider 级基础设施故障（DNS / 拒连 / 证书）。
func (t *Tracker) MarkConnFailure(provider string) {
	t.markFailure(provider, "", connFailDuration)
}

// MarkModelDown model 级故障（404 / 全 key 失败）。
func (t *Tracker) MarkModelDown(provider, model string) {
	t.markFailure(provider, model, allKeysFailDuration)
}

// MarkProviderDown 显式标记 provider 级 unhealthy（规则动作）。
func (t *Tracker) MarkProviderDown(provider string) {
	t.markFailure(provider, "", connFailDuration)
}

func (t *Tracker) markFailure(provider, model string, base time.Duration) {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := time.Now()
	e := t.get(provider, model)
	st := e.resolve(now)
	e.errorCount++
	e.lastError = now
	switch st {
	case Probing:
		// probing 失败 → 2× 退避，上限 5min
		e.backoff *= 2
		if e.backoff > maxBackoff {
			e.backoff = maxBackoff
		}
		if e.backoff == 0 {
			e.backoff = base
		}
	default:
		e.backoff = base
	}
	e.status = Unhealthy
	e.until = now.Add(e.backoff)
}

// MarkSuccess 一次成功：probing/unhealthy → healthy，并记录 TTFT。
func (t *Tracker) MarkSuccess(provider, model string, ttftMs int64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := time.Now()
	for _, e := range []*entry{t.get(provider, ""), t.get(provider, model)} {
		e.status = Healthy
		e.backoff = 0
		e.lastSuccess = now
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
}

// AvgTTFT 返回 (provider, model) 的平均 TTFT；无数据返回 (0, false)。
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
}

// Snapshots 导出全部健康状态。
func (t *Tracker) Snapshots() []Snapshot {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := time.Now()
	var out []Snapshot
	for k, e := range t.m {
		s := Snapshot{
			Provider:   k[0],
			Model:      k[1],
			Status:     e.resolve(now),
			AvgTTFTMs:  e.avgTTFTMs,
			ErrorCount: e.errorCount,
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
