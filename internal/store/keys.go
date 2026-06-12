package store

import (
	"database/sql"
	"fmt"
	"time"
)

// KeyStatus 取值。
const (
	KeyIdle        = "idle"
	KeyActive      = "active"
	KeyInvalid     = "invalid"
	KeyRateLimited = "rate_limited"
)

// Key 对应 keys 表一行。
// 注意：APIKey/Email/Password 不参与 JSON 序列化，API 返回时只暴露掩码版本。
type Key struct {
	ID             int64      `json:"id"`
	Provider       string     `json:"provider"`
	APIKey         string     `json:"-"`
	Email          string     `json:"-"`
	Password       string     `json:"-"`
	Status         string     `json:"status"`
	RateLimitUntil *time.Time `json:"rate_limit_until,omitempty"`
	RateLimitCount int        `json:"rate_limit_count"`
	FirstRequestAt *time.Time `json:"first_request_at,omitempty"`
	LastUsedAt     *time.Time `json:"last_used_at,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	Metadata       string     `json:"metadata,omitempty"` // JSON
}

// MaskedKey 返回 key 的掩码形式（前 6 后 4）。
func (k *Key) MaskedKey() string {
	if len(k.APIKey) <= 12 {
		return "****"
	}
	return k.APIKey[:6] + "..." + k.APIKey[len(k.APIKey)-4:]
}

// MaskedEmail 返回 email 的掩码形式。
func (k *Key) MaskedEmail() string {
	if k.Email == "" {
		return ""
	}
	at := 0
	for i, c := range k.Email {
		if c == '@' {
			at = i
			break
		}
	}
	if at <= 2 {
		return "**" + k.Email[at:]
	}
	return k.Email[:2] + "***" + k.Email[at:]
}

const timeLayout = "2006-01-02 15:04:05"

func fmtTime(t time.Time) string { return t.UTC().Format(timeLayout) }

func parseTime(s sql.NullString) *time.Time {
	if !s.Valid || s.String == "" {
		return nil
	}
	for _, layout := range []string{timeLayout, time.RFC3339} {
		if t, err := time.Parse(layout, s.String); err == nil {
			return &t
		}
	}
	return nil
}

// InsertKey 新增 key，返回自增 ID。
func (s *Store) InsertKey(k *Key) (int64, error) {
	res, err := s.db.Exec(`INSERT INTO keys (provider, api_key, email, password, status, metadata)
		VALUES (?, ?, ?, ?, ?, ?)`,
		k.Provider, k.APIKey, nullable(k.Email), nullable(k.Password),
		orDefault(k.Status, KeyIdle), nullable(k.Metadata))
	if err != nil {
		return 0, fmt.Errorf("insert key: %w", err)
	}
	return res.LastInsertId()
}

// ListKeys 列出 key；provider 为空 = 全部。
func (s *Store) ListKeys(provider string) ([]*Key, error) {
	q := `SELECT id, provider, api_key, COALESCE(email,''), COALESCE(password,''), status,
		rate_limit_until, COALESCE(rate_limit_count,0), first_request_at, last_used_at,
		COALESCE(created_at,''), COALESCE(metadata,'')
		FROM keys`
	var args []any
	if provider != "" {
		q += ` WHERE provider = ?`
		args = append(args, provider)
	}
	q += ` ORDER BY id`
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Key
	for rows.Next() {
		k := &Key{}
		var rlUntil, firstReq, lastUsed sql.NullString
		var created string
		if err := rows.Scan(&k.ID, &k.Provider, &k.APIKey, &k.Email, &k.Password, &k.Status,
			&rlUntil, &k.RateLimitCount, &firstReq, &lastUsed, &created, &k.Metadata); err != nil {
			return nil, err
		}
		k.RateLimitUntil = parseTime(rlUntil)
		k.FirstRequestAt = parseTime(firstReq)
		k.LastUsedAt = parseTime(lastUsed)
		if t := parseTime(sql.NullString{String: created, Valid: true}); t != nil {
			k.CreatedAt = *t
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

// UpdateKeyStatus 更新状态；status=rate_limited 时写入 until 与 count。
func (s *Store) UpdateKeyStatus(id int64, status string, until *time.Time, count int) error {
	var u any
	if until != nil {
		u = fmtTime(*until)
	}
	_, err := s.db.Exec(`UPDATE keys SET status=?, rate_limit_until=?, rate_limit_count=? WHERE id=?`,
		status, u, count, id)
	return err
}

// ResetKeyBackoff 请求成功后清零退避计数。
func (s *Store) ResetKeyBackoff(id int64) error {
	_, err := s.db.Exec(`UPDATE keys SET rate_limit_count=0, rate_limit_until=NULL WHERE id=?`, id)
	return err
}

// TouchKey 记录使用时间（首次使用时同时填 first_request_at）。
func (s *Store) TouchKey(id int64) error {
	now := fmtTime(time.Now())
	_, err := s.db.Exec(`UPDATE keys SET last_used_at=?,
		first_request_at=COALESCE(first_request_at, ?) WHERE id=?`, now, now, id)
	return err
}

// UpdateKeyMetadata 覆盖 metadata JSON。
func (s *Store) UpdateKeyMetadata(id int64, metadata string) error {
	_, err := s.db.Exec(`UPDATE keys SET metadata=? WHERE id=?`, metadata, id)
	return err
}

// CountKeys 统计某 provider 各状态 key 数。
func (s *Store) CountKeys(provider string) (total, usable int, err error) {
	err = s.db.QueryRow(`SELECT COUNT(*),
		SUM(CASE WHEN status IN ('idle','active') THEN 1 ELSE 0 END)
		FROM keys WHERE provider=?`, provider).Scan(&total, &usable)
	return
}

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}
