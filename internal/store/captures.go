package store

import (
	"database/sql"
	"fmt"
	"strings"
)

// Capture 对应 captures 表一行。
type Capture struct {
	ID              int64  `json:"id"`
	RequestID       int64  `json:"request_id"`
	MatchedSlug     string `json:"matched_slug"`
	Fingerprint     string `json:"fingerprint,omitempty"`
	Provider        string `json:"provider"`
	Model           string `json:"model,omitempty"`
	RequestHeaders  string `json:"request_headers,omitempty"`
	RequestBody     string `json:"request_body,omitempty"`
	ResponseHeaders string `json:"response_headers,omitempty"`
	ResponseBody    string `json:"response_body,omitempty"`
	Redacted        bool   `json:"redacted"`
	CreatedAt       string `json:"created_at"`
}

// InsertCapture 写入抓包并执行环形淘汰（每个指纹至少保留 1 条）。
func (s *Store) InsertCapture(c *Capture, maxRecords int) (int64, error) {
	res, err := s.db.Exec(`INSERT INTO captures
		(request_id, matched_slug, fingerprint, provider, model,
		 request_headers, request_body, response_headers, response_body, redacted)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		c.RequestID, c.MatchedSlug, nullable(c.Fingerprint), c.Provider, nullable(c.Model),
		nullable(c.RequestHeaders), nullable(c.RequestBody),
		nullable(c.ResponseHeaders), nullable(c.ResponseBody), boolInt(c.Redacted))
	if err != nil {
		return 0, fmt.Errorf("insert capture: %w", err)
	}
	id, _ := res.LastInsertId()
	if maxRecords > 0 {
		s.evictCaptures(maxRecords)
	}
	return id, nil
}

// evictCaptures 超出上限时按时间淘汰，但每个错误指纹至少保留最新 1 条。
func (s *Store) evictCaptures(maxRecords int) {
	s.db.Exec(`DELETE FROM captures WHERE id IN (
		SELECT id FROM captures
		WHERE id NOT IN (
			SELECT MAX(id) FROM captures WHERE fingerprint IS NOT NULL GROUP BY fingerprint
		)
		ORDER BY id ASC
		LIMIT MAX(0, (SELECT COUNT(*) FROM captures) - ?)
	)`, maxRecords)
}

// CapturesByRequest 批量返回每个请求最新的一条抓包；无抓包的请求不在结果中。
func (s *Store) CapturesByRequest(requestIDs []int64) (map[int64]*Capture, error) {
	result := make(map[int64]*Capture, len(requestIDs))
	if len(requestIDs) == 0 {
		return result, nil
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(requestIDs)), ",")
	query := `SELECT id, request_id, matched_slug, COALESCE(fingerprint,''), provider, COALESCE(model,''),
		COALESCE(request_headers,''), COALESCE(request_body,''),
		COALESCE(response_headers,''), COALESCE(response_body,''), redacted, COALESCE(created_at,'')
		FROM captures
		WHERE id IN (SELECT MAX(id) FROM captures GROUP BY request_id)
			AND request_id IN (` + placeholders + `)`
	args := make([]any, len(requestIDs))
	for i, id := range requestIDs {
		args[i] = id
	}
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		c := &Capture{}
		var redacted int
		if err := rows.Scan(&c.ID, &c.RequestID, &c.MatchedSlug, &c.Fingerprint, &c.Provider, &c.Model,
			&c.RequestHeaders, &c.RequestBody, &c.ResponseHeaders, &c.ResponseBody, &redacted, &c.CreatedAt); err != nil {
			return nil, err
		}
		c.Redacted = redacted != 0
		result[c.RequestID] = c
	}
	return result, rows.Err()
}

// AvgTTFT 查询某 (provider, model) 最近 n 条成功请求的平均 TTFT；无数据返回 false。
func (s *Store) AvgTTFT(provider, model string, n int) (int64, bool) {
	var avg sql.NullFloat64
	err := s.db.QueryRow(`SELECT AVG(ttft_ms) FROM (
		SELECT ttft_ms FROM requests
		WHERE provider=? AND model=? AND ttft_ms IS NOT NULL AND error_fingerprint IS NULL
		ORDER BY id DESC LIMIT ?)`, provider, model, n).Scan(&avg)
	if err != nil || !avg.Valid {
		return 0, false
	}
	return int64(avg.Float64), true
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
