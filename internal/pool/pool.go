// Package pool 实现 per-provider key 池：sticky / round-robin 选择、
// singleflight 切换、rate_limited 临时退避（指针可回到原 key）。
// 内存为主，SQLite 仅持久化。
package pool

import (
	"fmt"
	"sync"
	"time"

	"github.com/SkyAerope/clowder/internal/store"
)

// Pool 管理所有 provider 的 key。
type Pool struct {
	mu    sync.Mutex
	st    *store.Store
	byPro map[string]*providerKeys

	BackoffBase time.Duration // backoff_key 退避基数
	BackoffMax  time.Duration // 0 = 不设上限
}

type providerKeys struct {
	strategy string // sticky | round-robin
	keys     []*store.Key
	cursor   int // sticky: 当前 key 下标；round-robin: 下一个下标
	// singleflight：切换进行中时其他 goroutine 等待
	switching bool
	switchCh  chan struct{}
}

// New 从 store 加载全部 key 构建内存池。
func New(st *store.Store, base, max time.Duration) (*Pool, error) {
	p := &Pool{st: st, byPro: map[string]*providerKeys{}, BackoffBase: base, BackoffMax: max}
	keys, err := st.ListKeys("")
	if err != nil {
		return nil, err
	}
	for _, k := range keys {
		pk := p.ensure(k.Provider)
		pk.keys = append(pk.keys, k)
	}
	return p, nil
}

func (p *Pool) ensure(provider string) *providerKeys {
	pk, ok := p.byPro[provider]
	if !ok {
		pk = &providerKeys{strategy: "sticky"}
		p.byPro[provider] = pk
	}
	return pk
}

// SetStrategy 设置某 provider 的 key 策略（加载 manifest 时调用）。
func (p *Pool) SetStrategy(provider, strategy string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.ensure(provider).strategy = strategy
}

// AddKey 注册新 key（写库 + 进内存池）。
func (p *Pool) AddKey(k *store.Key) error {
	id, err := p.st.InsertKey(k)
	if err != nil {
		return err
	}
	k.ID = id
	p.mu.Lock()
	defer p.mu.Unlock()
	p.ensure(k.Provider).keys = append(p.ensure(k.Provider).keys, k)
	return nil
}

// usable 判断 key 当前是否可用（invalid 永久排除；rate_limited 看到期时间）。
func usable(k *store.Key, now time.Time) bool {
	switch k.Status {
	case store.KeyInvalid:
		return false
	case store.KeyRateLimited:
		return k.RateLimitUntil != nil && now.After(*k.RateLimitUntil)
	default:
		return true
	}
}

// Pick 按策略选一个可用 key；exclude 为本链已用过的 key id（换 key 时跳过）。
func (p *Pool) Pick(provider string, exclude map[int64]bool) (*store.Key, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	pk, ok := p.byPro[provider]
	if !ok || len(pk.keys) == 0 {
		return nil, fmt.Errorf("no keys for provider %s", provider)
	}
	now := time.Now()
	n := len(pk.keys)
	switch pk.strategy {
	case "round-robin":
		for i := 0; i < n; i++ {
			k := pk.keys[(pk.cursor+i)%n]
			if usable(k, now) && !exclude[k.ID] {
				pk.cursor = (pk.cursor + i + 1) % n
				return k, nil
			}
		}
	default: // sticky：固定从 cursor 开始（用完才换），退避中临时跳后面的 key
		for i := 0; i < n; i++ {
			k := pk.keys[(pk.cursor+i)%n]
			if usable(k, now) && !exclude[k.ID] {
				// 不推进 cursor：退避结束后指针回到原 key
				return k, nil
			}
		}
	}
	return nil, fmt.Errorf("no usable key for provider %s", provider)
}

// MarkInvalid 永久禁用 key；sticky 下若是 current key，singleflight 推进指针。
func (p *Pool) MarkInvalid(keyID int64) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	k, pk := p.find(keyID)
	if k == nil {
		return fmt.Errorf("key %d not found", keyID)
	}
	if k.Status == store.KeyInvalid {
		return nil // 已禁用，幂等（singleflight 效果：并发 401 只烧一次）
	}
	k.Status = store.KeyInvalid
	if err := p.st.UpdateKeyStatus(keyID, store.KeyInvalid, nil, k.RateLimitCount); err != nil {
		return err
	}
	// sticky：current 被烧掉 → 指针推进到下一个可用 key
	if pk.strategy != "round-robin" && pk.keys[pk.cursor%len(pk.keys)].ID == keyID {
		p.advanceLocked(pk)
	}
	return nil
}

// advanceLocked 推进 sticky 指针到下一个可用 key（调用方持锁）。
// 推进后的新 cursor key 即为 active：上一个请求成功 & 下一个请求将尝试的 key。
func (p *Pool) advanceLocked(pk *providerKeys) {
	now := time.Now()
	n := len(pk.keys)
	for i := 1; i <= n; i++ {
		idx := (pk.cursor + i) % n
		if usable(pk.keys[idx], now) {
			pk.cursor = idx
			k := pk.keys[idx]
			if k.Status != store.KeyActive {
				k.Status = store.KeyActive
				k.RateLimitUntil = nil
				_ = p.st.UpdateKeyStatus(k.ID, store.KeyActive, nil, k.RateLimitCount)
			}
			return
		}
	}
}

// MarkRateLimited 临时退避至 until（不推进 sticky 指针——退避结束回原 key）。
func (p *Pool) MarkRateLimited(keyID int64, until time.Time, count int) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	k, _ := p.find(keyID)
	if k == nil {
		return fmt.Errorf("key %d not found", keyID)
	}
	k.Status = store.KeyRateLimited
	k.RateLimitUntil = &until
	k.RateLimitCount = count
	return p.st.UpdateKeyStatus(keyID, store.KeyRateLimited, &until, count)
}

// MarkActive 恢复 key 为可用。
func (p *Pool) MarkActive(keyID int64) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	k, _ := p.find(keyID)
	if k == nil {
		return fmt.Errorf("key %d not found", keyID)
	}
	k.Status = store.KeyActive
	k.RateLimitUntil = nil
	return p.st.UpdateKeyStatus(keyID, store.KeyActive, nil, k.RateLimitCount)
}

// BackoffKey 实现 backoff_key 动作：
// 已在退避中 → no-op（同一波并发 429 只推进一档）；
// 否则按当前 count 计算退避时长并 count+1。
func (p *Pool) BackoffKey(keyID int64) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	k, _ := p.find(keyID)
	if k == nil {
		return fmt.Errorf("key %d not found", keyID)
	}
	now := time.Now()
	if k.Status == store.KeyRateLimited && k.RateLimitUntil != nil && now.Before(*k.RateLimitUntil) {
		return nil // 退避中：不叠加、不延长
	}
	d := p.BackoffBase * (1 << min(k.RateLimitCount, 30))
	if p.BackoffMax > 0 && d > p.BackoffMax {
		d = p.BackoffMax
	}
	until := now.Add(d)
	k.Status = store.KeyRateLimited
	k.RateLimitUntil = &until
	k.RateLimitCount++
	return p.st.UpdateKeyStatus(keyID, store.KeyRateLimited, &until, k.RateLimitCount)
}

// OnSuccess 请求成功：清零退避计数、恢复 active、记录使用时间。
func (p *Pool) OnSuccess(keyID int64) {
	p.mu.Lock()
	k, _ := p.find(keyID)
	if k != nil {
		k.RateLimitCount = 0
		k.RateLimitUntil = nil
		if k.Status != store.KeyInvalid {
			k.Status = store.KeyActive
		}
	}
	p.mu.Unlock()
	if k != nil {
		p.st.ResetKeyBackoff(keyID)
		p.st.UpdateKeyStatus(keyID, store.KeyActive, nil, 0)
		p.st.TouchKey(keyID)
	}
}

// UsableCount 某 provider 当前可用 key 数与总数。
func (p *Pool) UsableCount(provider string) (usableN, total int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	pk, ok := p.byPro[provider]
	if !ok {
		return 0, 0
	}
	now := time.Now()
	for _, k := range pk.keys {
		if usable(k, now) {
			usableN++
		}
	}
	return usableN, len(pk.keys)
}

// Keys 返回某 provider 的 key 快照（API 用）。
func (p *Pool) Keys(provider string) []*store.Key {
	p.mu.Lock()
	defer p.mu.Unlock()
	pk, ok := p.byPro[provider]
	if !ok {
		return nil
	}
	out := make([]*store.Key, len(pk.keys))
	copy(out, pk.keys)
	return out
}

// FindByID 按 id 查 key。
func (p *Pool) FindByID(keyID int64) *store.Key {
	p.mu.Lock()
	defer p.mu.Unlock()
	k, _ := p.find(keyID)
	return k
}

// HasKey 判断 api_key 是否已存在（static 加载去重用）。
func (p *Pool) HasKey(provider, apiKey string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	pk, ok := p.byPro[provider]
	if !ok {
		return false
	}
	for _, k := range pk.keys {
		if k.APIKey == apiKey {
			return true
		}
	}
	return false
}

func (p *Pool) find(keyID int64) (*store.Key, *providerKeys) {
	for _, pk := range p.byPro {
		for _, k := range pk.keys {
			if k.ID == keyID {
				return k, pk
			}
		}
	}
	return nil, nil
}
