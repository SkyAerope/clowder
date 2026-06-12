package store

import (
	"database/sql"
	"fmt"
)

// RequestLog 对应 requests 表一行（一次上游 attempt）。
type RequestLog struct {
	ID               int64   `json:"id"`
	TraceID          string  `json:"trace_id"`
	AttemptNo        int     `json:"attempt_no"`
	KeyID            *int64  `json:"key_id,omitempty"`
	Provider         string  `json:"provider"`
	Model            string  `json:"model"`
	StatusCode       *int    `json:"status_code,omitempty"`
	TTFTMs           *int64  `json:"ttft_ms,omitempty"`
	LatencyMs        *int64  `json:"latency_ms,omitempty"`
	MatchedRule      string  `json:"matched_rule,omitempty"`
	ErrorMsg         string  `json:"error_msg,omitempty"`
	ErrorFingerprint string  `json:"error_fingerprint,omitempty"`
	CreatedAt        string  `json:"created_at"`
}

// InsertRequest 写入一条 attempt 日志。
func (s *Store) InsertRequest(r *RequestLog) (int64, error) {
	res, err := s.db.Exec(`INSERT INTO requests
		(trace_id, attempt_no, key_id, provider, model, status_code, ttft_ms, latency_ms, matched_rule, error_msg, error_fingerprint)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		r.TraceID, r.AttemptNo, r.KeyID, r.Provider, r.Model,
		r.StatusCode, r.TTFTMs, r.LatencyMs,
		nullable(r.MatchedRule), nullable(r.ErrorMsg), nullable(r.ErrorFingerprint))
	if err != nil {
		return 0, fmt.Errorf("insert request: %w", err)
	}
	return res.LastInsertId()
}

// ListRequests 分页查询（按 id 倒序）。
func (s *Store) ListRequests(limit, offset int) ([]*RequestLog, error) {
	rows, err := s.db.Query(`SELECT id, trace_id, attempt_no, key_id, provider, model,
		status_code, ttft_ms, latency_ms, COALESCE(matched_rule,''), COALESCE(error_msg,''),
		COALESCE(error_fingerprint,''), COALESCE(created_at,'')
		FROM requests ORDER BY id DESC LIMIT ? OFFSET ?`, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanRequests(rows)
}

// ListTrace 按 trace_id 查询完整重试链（按 attempt_no 升序）。
func (s *Store) ListTrace(traceID string) ([]*RequestLog, error) {
	rows, err := s.db.Query(`SELECT id, trace_id, attempt_no, key_id, provider, model,
		status_code, ttft_ms, latency_ms, COALESCE(matched_rule,''), COALESCE(error_msg,''),
		COALESCE(error_fingerprint,''), COALESCE(created_at,'')
		FROM requests WHERE trace_id=? ORDER BY attempt_no, id`, traceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanRequests(rows)
}

func scanRequests(rows *sql.Rows) ([]*RequestLog, error) {
	var out []*RequestLog
	for rows.Next() {
		r := &RequestLog{}
		var keyID sql.NullInt64
		var status sql.NullInt64
		var ttft, latency sql.NullInt64
		if err := rows.Scan(&r.ID, &r.TraceID, &r.AttemptNo, &keyID, &r.Provider, &r.Model,
			&status, &ttft, &latency, &r.MatchedRule, &r.ErrorMsg, &r.ErrorFingerprint, &r.CreatedAt); err != nil {
			return nil, err
		}
		if keyID.Valid {
			r.KeyID = &keyID.Int64
		}
		if status.Valid {
			v := int(status.Int64)
			r.StatusCode = &v
		}
		if ttft.Valid {
			r.TTFTMs = &ttft.Int64
		}
		if latency.Valid {
			r.LatencyMs = &latency.Int64
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ErrorGroup 错误聚合行。
type ErrorGroup struct {
	Fingerprint string `json:"fingerprint"`
	Provider    string `json:"provider"`
	StatusCode  *int   `json:"status_code,omitempty"`
	Models      string `json:"models"`       // 逗号拼接的去重模型列表
	Rules       string `json:"rules"`        // 逗号拼接的去重命中规则
	Count       int64  `json:"count"`
	FirstSeen   string `json:"first_seen"`
	LastSeen    string `json:"last_seen"`
	SampleError string `json:"sample_error"`
}

// AggregateErrors 按错误指纹全量聚合；rule 非空时只看命中该规则的。
func (s *Store) AggregateErrors(rule string, limit int) ([]*ErrorGroup, error) {
	q := `SELECT error_fingerprint, provider, status_code,
		GROUP_CONCAT(DISTINCT model), GROUP_CONCAT(DISTINCT matched_rule),
		COUNT(*), MIN(created_at), MAX(created_at), MAX(COALESCE(error_msg,''))
		FROM requests WHERE error_fingerprint IS NOT NULL`
	var args []any
	if rule != "" {
		q += ` AND matched_rule = ?`
		args = append(args, rule)
	}
	q += ` GROUP BY error_fingerprint ORDER BY MAX(created_at) DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*ErrorGroup
	for rows.Next() {
		g := &ErrorGroup{}
		var status sql.NullInt64
		var models, rules sql.NullString
		if err := rows.Scan(&g.Fingerprint, &g.Provider, &status, &models, &rules,
			&g.Count, &g.FirstSeen, &g.LastSeen, &g.SampleError); err != nil {
			return nil, err
		}
		if status.Valid {
			v := int(status.Int64)
			g.StatusCode = &v
		}
		g.Models = models.String
		g.Rules = rules.String
		out = append(out, g)
	}
	return out, rows.Err()
}
