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
