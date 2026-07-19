// Package pool 实现 per-provider key 池：sticky / round-robin 选择、
// singleflight 切换、rate_limited 临时退避（指针可回到原 key）。
// 内存为主，SQLite 仅持久化。
package pool

import (
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/SkyAerope/clowder/internal/store"
)

// Pool 管理所有 provider 的 key。
type Pool struct {
	mu    sync.RWMutex
	st    *store.Store
	byPro map[string]*providerKeys
	byID  map[int64]*keyRef

	BackoffBase time.Duration // backoff_key 退避基数
	BackoffMax  time.Duration // 0 = 不设上限
}

type providerKeys struct {
	mu         sync.Mutex
	strategy   string // sticky | round-robin
	keys       []*store.Key
	cursor     int // sticky: 当前 key 下标；round-robin: 下一个下标
	recovering bool
}

type keyRef struct {
	key      *store.Key
	provider *providerKeys
}

// New 从 store 加载全部 key 构建内存池。
func New(st *store.Store, base, max time.Duration) (*Pool, error) {
	p := &Pool{
		st: st, byPro: map[string]*providerKeys{}, byID: map[int64]*keyRef{},
		BackoffBase: base, BackoffMax: max,
	}
	keys, err := st.ListKeys("")
	if err != nil {
		return nil, err
	}
	for _, k := range keys {
		pk := p.ensure(k.Provider)
		pk.keys = append(pk.keys, k)
		p.byID[k.ID] = &keyRef{key: k, provider: pk}
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
	pk := p.ensure(provider)
	pk.mu.Lock()
	pk.strategy = strategy
	pk.mu.Unlock()
	p.mu.Unlock()
}

// AddKey 注册新 key（写库 + 进内存池）。
func (p *Pool) AddKey(k *store.Key) error {
	id, err := p.st.InsertKey(k)
	if err != nil {
		return err
	}
	k.ID = id
	p.mu.Lock()
	pk := p.ensure(k.Provider)
	pk.mu.Lock()
	pk.keys = append(pk.keys, k)
	p.byID[k.ID] = &keyRef{key: k, provider: pk}
	pk.mu.Unlock()
	p.mu.Unlock()
	return nil
}

// Pick 优先选择 active key；没有 active 时，恢复到期限流 key，并晋升 ID 最小的 idle key。
func (p *Pool) Pick(provider string, exclude map[int64]bool) (*store.Key, error) {
	p.mu.RLock()
	pk, ok := p.byPro[provider]
	if ok {
		pk.mu.Lock()
	}
	p.mu.RUnlock()
	if !ok || len(pk.keys) == 0 {
		return nil, fmt.Errorf("no keys for provider %s", provider)
	}
	var active, idle *store.Key
	hasExpired := false
	now := time.Now()
	for _, k := range pk.keys {
		if k.Status == store.KeyRateLimited && k.RateLimitUntil != nil && !now.Before(*k.RateLimitUntil) {
			hasExpired = true
		}
		if exclude[k.ID] {
			continue
		}
		switch k.Status {
		case store.KeyActive:
			if active == nil || k.ID < active.ID {
				active = k
			}
		case store.KeyIdle:
			if idle == nil || k.ID < idle.ID {
				idle = k
			}
		}
	}
	if active == nil {
		if err := p.resolveExpiredLocked(pk, now); err != nil {
			pk.mu.Unlock()
			return nil, err
		}
		for _, k := range pk.keys {
			if !exclude[k.ID] && k.Status == store.KeyIdle && (idle == nil || k.ID < idle.ID) {
				idle = k
			}
		}
		active = idle
	}
	if active == nil {
		pk.mu.Unlock()
		return nil, fmt.Errorf("no active keys for provider %s", provider)
	}
	if err := p.setActiveLocked(pk, active); err != nil {
		pk.mu.Unlock()
		return nil, err
	}
	selected := cloneKey(active)
	startRecovery := hasExpired && !pk.recovering
	if startRecovery {
		pk.recovering = true
	}
	pk.mu.Unlock()
	if startRecovery {
		go p.recoverExpired(pk)
	}
	return selected, nil
}

func (p *Pool) recoverExpired(pk *providerKeys) {
	pk.mu.Lock()
	_ = p.resolveExpiredLocked(pk, time.Now())
	pk.recovering = false
	pk.mu.Unlock()
}

func (p *Pool) resolveExpiredLocked(pk *providerKeys, now time.Time) error {
	for _, k := range pk.keys {
		if k.Status != store.KeyRateLimited || k.RateLimitUntil == nil || now.Before(*k.RateLimitUntil) {
			continue
		}
		if err := p.st.UpdateKeyStatus(k.ID, store.KeyIdle, nil, k.RateLimitCount); err != nil {
			return err
		}
		k.Status = store.KeyIdle
		k.RateLimitUntil = nil
	}
	return nil
}

func cloneKey(k *store.Key) *store.Key {
	if k == nil {
		return nil
	}
	clone := *k
	if k.RateLimitUntil != nil {
		until := *k.RateLimitUntil
		clone.RateLimitUntil = &until
	}
	if k.FirstRequestAt != nil {
		first := *k.FirstRequestAt
		clone.FirstRequestAt = &first
	}
	if k.LastUsedAt != nil {
		last := *k.LastUsedAt
		clone.LastUsedAt = &last
	}
	return &clone
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
	k, pk := p.lockKey(keyID)
	if k == nil {
		return fmt.Errorf("key %d not found", keyID)
	}
	defer pk.mu.Unlock()
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
	k, pk := p.lockKey(keyID)
	if k == nil {
		return fmt.Errorf("key %d not found", keyID)
	}
	defer pk.mu.Unlock()
	k.Status = store.KeyRateLimited
	k.RateLimitUntil = &until
	k.RateLimitCount = count
	return p.st.UpdateKeyStatus(keyID, store.KeyRateLimited, &until, count)
}

// MarkActive 恢复 key 为可用。
func (p *Pool) MarkActive(keyID int64) error {
	k, pk := p.lockKey(keyID)
	if k == nil {
		return fmt.Errorf("key %d not found", keyID)
	}
	defer pk.mu.Unlock()
	return p.setActiveLocked(pk, k)
}

// SetStatus 手动设置 key 状态。
func (p *Pool) SetStatus(keyID int64, status string) error {
	k, pk := p.lockKey(keyID)
	if k == nil {
		return fmt.Errorf("key %d not found", keyID)
	}
	defer pk.mu.Unlock()
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
	ref := p.byID[keyID]
	if ref == nil {
		p.mu.Unlock()
		return fmt.Errorf("key %d not found", keyID)
	}
	pk := ref.provider
	pk.mu.Lock()
	defer pk.mu.Unlock()
	defer p.mu.Unlock()
	if err := p.st.DeleteKey(keyID); err != nil {
		return err
	}
	for i, k := range pk.keys {
		if k.ID == keyID {
			pk.keys = append(pk.keys[:i], pk.keys[i+1:]...)
			break
		}
	}
	delete(p.byID, keyID)
	if len(pk.keys) == 0 {
		delete(p.byPro, ref.key.Provider)
	} else if pk.cursor >= len(pk.keys) {
		pk.cursor = 0
	}
	return nil
}

// BackoffKey 实现 backoff_key 动作：
// 已在退避中 → no-op（同一波并发 429 只推进一档）；
// 否则按当前 count 计算退避时长并 count+1。
func (p *Pool) BackoffKey(keyID int64) error {
	k, pk := p.lockKey(keyID)
	if k == nil {
		return fmt.Errorf("key %d not found", keyID)
	}
	defer pk.mu.Unlock()
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
	k, pk := p.lockKey(keyID)
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
	active := k != nil && k.Status != store.KeyInvalid
	if pk != nil {
		pk.mu.Unlock()
	}
	if active {
		p.st.ResetKeyBackoff(keyID)
		p.st.UpdateKeyStatus(keyID, store.KeyActive, nil, 0)
		p.st.TouchKey(keyID)
	}
}

// UsableCount 某 provider 当前可用 key 数与总数。
func (p *Pool) UsableCount(provider string) (usableN, total int) {
	pk := p.lockProvider(provider)
	if pk == nil {
		return 0, 0
	}
	defer pk.mu.Unlock()
	_ = p.resolveExpiredLocked(pk, time.Now())
	for _, k := range pk.keys {
		if k.Status == store.KeyActive || k.Status == store.KeyIdle {
			usableN++
		}
	}
	return usableN, len(pk.keys)
}

// Keys 返回某 provider 的 key 快照（API 用）。
func (p *Pool) Keys(provider string) []*store.Key {
	if provider == "" {
		p.mu.RLock()
		providers := make([]*providerKeys, 0, len(p.byPro))
		for _, pk := range p.byPro {
			providers = append(providers, pk)
		}
		p.mu.RUnlock()

		var out []*store.Key
		for _, pk := range providers {
			pk.mu.Lock()
			_ = p.resolveExpiredLocked(pk, time.Now())
			for _, k := range pk.keys {
				out = append(out, cloneKey(k))
			}
			pk.mu.Unlock()
		}
		sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
		return out
	}
	pk := p.lockProvider(provider)
	if pk == nil {
		return nil
	}
	defer pk.mu.Unlock()
	_ = p.resolveExpiredLocked(pk, time.Now())
	out := make([]*store.Key, len(pk.keys))
	for i, k := range pk.keys {
		out[i] = cloneKey(k)
	}
	return out
}

// FindByID 按 id 查 key。
func (p *Pool) FindByID(keyID int64) *store.Key {
	k, pk := p.lockKey(keyID)
	if k == nil {
		return nil
	}
	defer pk.mu.Unlock()
	_ = p.resolveExpiredLocked(pk, time.Now())
	return cloneKey(k)
}

// UpdateKeyMetadata 同步更新 key 的内存快照与持久化数据。
func (p *Pool) UpdateKeyMetadata(keyID int64, metadata string) error {
	k, pk := p.lockKey(keyID)
	if k == nil {
		return fmt.Errorf("key %d not found", keyID)
	}
	defer pk.mu.Unlock()
	if err := p.st.UpdateKeyMetadata(keyID, metadata); err != nil {
		return err
	}
	k.Metadata = metadata
	return nil
}

// HasKey 判断 api_key 是否已存在（static 加载去重用）。
func (p *Pool) HasKey(provider, apiKey string) bool {
	pk := p.lockProvider(provider)
	if pk == nil {
		return false
	}
	defer pk.mu.Unlock()
	for _, k := range pk.keys {
		if k.APIKey == apiKey {
			return true
		}
	}
	return false
}

func (p *Pool) lockProvider(provider string) *providerKeys {
	p.mu.RLock()
	pk := p.byPro[provider]
	if pk != nil {
		pk.mu.Lock()
	}
	p.mu.RUnlock()
	return pk
}

func (p *Pool) lockKey(keyID int64) (*store.Key, *providerKeys) {
	p.mu.RLock()
	ref := p.byID[keyID]
	if ref == nil {
		p.mu.RUnlock()
		return nil, nil
	}
	ref.provider.mu.Lock()
	p.mu.RUnlock()
	return ref.key, ref.provider
}
