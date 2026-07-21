package pool

import (
	"fmt"
	"sort"
	"time"

	"github.com/SkyAerope/clowder/internal/store"
)

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
