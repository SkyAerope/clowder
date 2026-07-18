// Package pool 实现 per-provider key 池：sticky / round-robin 选择、
// singleflight 切换、rate_limited 临时退避（指针可回到原 key）。
// 内存为主，SQLite 仅持久化。
package pool

import (
	"fmt"
	"math/rand"
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

// Pick 只选择 active key；没有 active 时，从 idle key 中随机晋升一个。
func (p *Pool) Pick(provider string, exclude map[int64]bool) (*store.Key, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	pk, ok := p.byPro[provider]
	if !ok || len(pk.keys) == 0 {
		return nil, fmt.Errorf("no keys for provider %s", provider)
	}
	active := make([]*store.Key, 0, len(pk.keys))
	idle := make([]*store.Key, 0, len(pk.keys))
	for _, k := range pk.keys {
		if exclude[k.ID] {
			continue
		}
		switch k.Status {
		case store.KeyActive:
			active = append(active, k)
		case store.KeyIdle:
			idle = append(idle, k)
		}
	}
	if len(active) == 0 && len(idle) > 0 {
		active = append(active, idle[rand.Intn(len(idle))])
	}
	if len(active) == 0 {
		return nil, fmt.Errorf("no active keys for provider %s", provider)
	}
	selected := active[rand.Intn(len(active))]
	if err := p.setActiveLocked(pk, selected); err != nil {
		return nil, err
	}
	return selected, nil
}

func (p *Pool) setActiveLocked(pk *providerKeys, selected *store.Key) error {
	for _, k := range pk.keys {
		if k == selected {
			if k.Status == store.KeyActive {
				continue
			}
			k.Status = store.KeyActive
			k.RateLimitUntil = nil
			if err := p.st.UpdateKeyStatus(k.ID, store.KeyActive, nil, k.RateLimitCount); err != nil {
				return err
			}
			continue
		}
		if k.Status != store.KeyActive {
			continue
		}
		k.Status = store.KeyIdle
		if err := p.st.UpdateKeyStatus(k.ID, store.KeyIdle, nil, k.RateLimitCount); err != nil {
			return err
		}
	}
	return nil
}

// MarkInvalid 永久禁用 key；sticky 下若是 current key，singleflight 推进指针。
func (p *Pool) MarkInvalid(keyID int64) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	k, _ := p.find(keyID)
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
	return nil
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
	k, pk := p.find(keyID)
	if k == nil {
		return fmt.Errorf("key %d not found", keyID)
	}
	return p.setActiveLocked(pk, k)
}

// SetStatus 手动设置 key 状态。
func (p *Pool) SetStatus(keyID int64, status string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	k, pk := p.find(keyID)
	if k == nil {
		return fmt.Errorf("key %d not found", keyID)
	}
	if status == store.KeyActive {
		return p.setActiveLocked(pk, k)
	}
	k.Status = status
	k.RateLimitUntil = nil
	return p.st.UpdateKeyStatus(keyID, status, nil, k.RateLimitCount)
}

// DeleteKey 从持久化存储和内存池中删除 key。
func (p *Pool) DeleteKey(keyID int64) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	for provider, pk := range p.byPro {
		for i, k := range pk.keys {
			if k.ID != keyID {
				continue
			}
			if err := p.st.DeleteKey(keyID); err != nil {
				return err
			}
			pk.keys = append(pk.keys[:i], pk.keys[i+1:]...)
			if len(pk.keys) == 0 {
				delete(p.byPro, provider)
			} else if pk.cursor >= len(pk.keys) {
				pk.cursor = 0
			}
			return nil
		}
	}
	return fmt.Errorf("key %d not found", keyID)
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
	k, pk := p.find(keyID)
	if k != nil {
		k.RateLimitCount = 0
		k.RateLimitUntil = nil
		if k.Status != store.KeyInvalid {
			k.Status = store.KeyActive
		}
		if k.Status == store.KeyActive {
			_ = p.setActiveLocked(pk, k)
		}
	}
	p.mu.Unlock()
	if k != nil && k.Status != store.KeyInvalid {
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
	for _, k := range pk.keys {
		if k.Status == store.KeyActive || k.Status == store.KeyIdle {
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
