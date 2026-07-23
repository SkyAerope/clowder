package store

import (
	"database/sql"
	"time"
)

type HealthSnapshot struct {
	Provider       string
	Model          string
	Status         string
	AvgTTFTMs      int64
	ErrorCount     int
	BackoffCount   int
	UnhealthyUntil *time.Time
	LastErrorAt    *time.Time
	LastSuccessAt  *time.Time
	LastSlug       string
}

func (s *Store) SaveHealth(snapshot HealthSnapshot) error {
	_, err := s.db.Exec(`INSERT INTO provider_health
		(provider, model, status, avg_ttft_ms, error_count, backoff_count,
				 unhealthy_until, last_error_at, last_success_at, last_slug)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(provider, model) DO UPDATE SET
			status=excluded.status, avg_ttft_ms=excluded.avg_ttft_ms,
			error_count=excluded.error_count, backoff_count=excluded.backoff_count,
			unhealthy_until=excluded.unhealthy_until,
				last_error_at=excluded.last_error_at, last_success_at=excluded.last_success_at,
				last_slug=excluded.last_slug`,
		snapshot.Provider, snapshot.Model, snapshot.Status, snapshot.AvgTTFTMs,
		snapshot.ErrorCount, snapshot.BackoffCount, timeValue(snapshot.UnhealthyUntil),
		timeValue(snapshot.LastErrorAt), timeValue(snapshot.LastSuccessAt), snapshot.LastSlug)
	return err
}

func (s *Store) ListHealth() ([]HealthSnapshot, error) {
	rows, err := s.db.Query(`SELECT provider, model, COALESCE(status,''),
		COALESCE(avg_ttft_ms,0), COALESCE(error_count,0), COALESCE(backoff_count,0),
			unhealthy_until, last_error_at, last_success_at, COALESCE(last_slug,'')
		FROM provider_health ORDER BY provider, model`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var snapshots []HealthSnapshot
	for rows.Next() {
		var snapshot HealthSnapshot
		var until, lastError, lastSuccess sql.NullString
		if err := rows.Scan(&snapshot.Provider, &snapshot.Model, &snapshot.Status,
			&snapshot.AvgTTFTMs, &snapshot.ErrorCount, &snapshot.BackoffCount,
			&until, &lastError, &lastSuccess, &snapshot.LastSlug); err != nil {
			return nil, err
		}
		snapshot.UnhealthyUntil = parseTime(until)
		snapshot.LastErrorAt = parseTime(lastError)
		snapshot.LastSuccessAt = parseTime(lastSuccess)
		snapshots = append(snapshots, snapshot)
	}
	return snapshots, rows.Err()
}

func timeValue(value *time.Time) any {
	if value == nil {
		return nil
	}
	return fmtTime(*value)
}
