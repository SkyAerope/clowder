package pool

import (
	"fmt"
	"time"

	"github.com/SkyAerope/clowder/internal/store"
)

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
