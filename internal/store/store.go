// Package store 封装 SQLite 持久化：key 池、请求日志、抓包、健康快照。
// 使用 modernc.org/sqlite（纯 Go，无 cgo）。
package store

import (
	"database/sql"
	"fmt"
	"path/filepath"

	_ "modernc.org/sqlite"
)

const schema = `
CREATE TABLE IF NOT EXISTS keys (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    provider    TEXT NOT NULL,
    api_key     TEXT NOT NULL UNIQUE,
    email       TEXT,
    password    TEXT,
    status      TEXT NOT NULL DEFAULT 'idle',
    rate_limit_until TEXT,
    rate_limit_count INTEGER DEFAULT 0,
    first_request_at TEXT,
    last_used_at    TEXT,
    created_at      TEXT DEFAULT (datetime('now')),
    metadata    TEXT
);

CREATE TABLE IF NOT EXISTS requests (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    trace_id    TEXT NOT NULL,
    attempt_no  INTEGER NOT NULL DEFAULT 1,
    key_id      INTEGER,
    provider    TEXT NOT NULL,
    model       TEXT NOT NULL,
    status_code INTEGER,
    ttft_ms     INTEGER,
    latency_ms  INTEGER,
    matched_rule TEXT,
    error_msg   TEXT,
    error_fingerprint TEXT,
    created_at  TEXT DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_requests_trace ON requests(trace_id);
CREATE INDEX IF NOT EXISTS idx_requests_fingerprint ON requests(error_fingerprint);
CREATE INDEX IF NOT EXISTS idx_requests_created ON requests(created_at);

CREATE TABLE IF NOT EXISTS captures (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    request_id  INTEGER NOT NULL,
    matched_rule TEXT NOT NULL,
    fingerprint TEXT,
    provider    TEXT NOT NULL,
    model       TEXT,
    request_headers  TEXT,
    request_body     TEXT,
    response_headers TEXT,
    response_body    TEXT,
    redacted    INTEGER DEFAULT 0,
    created_at  TEXT DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_captures_rule ON captures(matched_rule);
CREATE INDEX IF NOT EXISTS idx_captures_fingerprint ON captures(fingerprint);

CREATE TABLE IF NOT EXISTS provider_health (
    provider   TEXT NOT NULL,
    model      TEXT NOT NULL DEFAULT '',
    status     TEXT,
    avg_ttft_ms INTEGER,
    error_count    INTEGER DEFAULT 0,
    last_error_at  TEXT,
    last_success_at     TEXT,
    PRIMARY KEY (provider, model)
);
`

// Store 持有 SQLite 连接。
type Store struct {
	db *sql.DB
}

// Open 打开（必要时创建）数据库并执行 schema 迁移。
func Open(dir string) (*Store, error) {
	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)",
		filepath.Join(dir, "clowder.db"))
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	// modernc/sqlite 写并发受限，单连接写最稳；读多时靠 WAL
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate schema: %w", err)
	}
	return &Store{db: db}, nil
}

// Close 关闭数据库。
func (s *Store) Close() error { return s.db.Close() }

// DB 暴露底层连接（仅供只读统计查询使用）。
func (s *Store) DB() *sql.DB { return s.db }
