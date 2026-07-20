package store_test

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/SkyAerope/clowder/internal/store"
)

func TestHealthSnapshot_roundtrips_all_state(t *testing.T) {
	// Given
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	until := time.Date(2026, 7, 19, 18, 0, 0, 0, time.UTC)
	lastError := until.Add(-time.Minute)
	lastSuccess := until.Add(-time.Hour)
	want := store.HealthSnapshot{
		Provider: "mock", Model: "model", Status: "unhealthy",
		AvgTTFTMs: 125, ErrorCount: 4, BackoffCount: 3, LastRule: "model-gone",
		UnhealthyUntil: &until, LastErrorAt: &lastError, LastSuccessAt: &lastSuccess,
	}

	// When
	if err := st.SaveHealth(want); err != nil {
		t.Fatal(err)
	}
	got, err := st.ListHealth()

	// Then
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("health snapshots = %d, want 1", len(got))
	}
	if got[0].Provider != want.Provider || got[0].Model != want.Model || got[0].Status != want.Status ||
		got[0].AvgTTFTMs != want.AvgTTFTMs || got[0].ErrorCount != want.ErrorCount ||
		got[0].BackoffCount != want.BackoffCount || got[0].LastRule != want.LastRule || !got[0].UnhealthyUntil.Equal(until) ||
		!got[0].LastErrorAt.Equal(lastError) || !got[0].LastSuccessAt.Equal(lastSuccess) {
		t.Fatalf("health snapshot = %#v, want %#v", got[0], want)
	}
}

func TestOpen_migrates_legacy_provider_health_schema(t *testing.T) {
	// Given
	dir := t.TempDir()
	db, err := sql.Open("sqlite", filepath.Join(dir, "clowder.db"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE provider_health (
		provider TEXT NOT NULL, model TEXT NOT NULL DEFAULT '', status TEXT,
		avg_ttft_ms INTEGER, error_count INTEGER DEFAULT 0,
		last_error_at TEXT, last_success_at TEXT,
		PRIMARY KEY (provider, model)
	)`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	// When
	st, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	until := time.Now().Add(time.Minute)
	err = st.SaveHealth(store.HealthSnapshot{
		Provider: "mock", Status: "unhealthy", BackoffCount: 2, UnhealthyUntil: &until,
	})

	// Then
	if err != nil {
		t.Fatal(err)
	}
}
