package pool

import (
	"fmt"
	"time"

	"github.com/SkyAerope/clowder/internal/store"
)

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
