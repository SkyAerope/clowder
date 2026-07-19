package health

import (
	"testing"
	"time"
)

func TestNativeHealthIsIsolated(t *testing.T) {
	tracker := New()
	tracker.MarkSuccess("provider", "native-a", 120)
	tracker.MarkSuccess("provider", "native-b", 240)
	tracker.MarkModelDown("provider", "native-a")

	if tracker.Routable("provider", "native-a") {
		t.Fatal("failed native should not be routable")
	}
	if !tracker.Routable("provider", "native-b") {
		t.Fatal("sibling native should remain routable")
	}
	if got, ok := tracker.AvgTTFT("provider", "native-b"); !ok || got != 240 {
		t.Fatalf("native-b TTFT = %d, ok=%v", got, ok)
	}
}

func TestRestore_preserves_healthy(t *testing.T) {
	// Given
	tracker := NewPersistent([]PersistedState{{Provider: "provider", Status: Healthy}}, nil)

	// When
	got := tracker.Status("provider", "")

	// Then
	if got != Healthy {
		t.Fatalf("status = %q, want %q", got, Healthy)
	}
}

func TestRestore_changes_expired_unhealthy_to_probing(t *testing.T) {
	// Given
	until := time.Now().Add(-time.Minute)
	tracker := NewPersistent([]PersistedState{{
		Provider: "provider", Status: Unhealthy, UnhealthyUntil: &until, BackoffCount: 2,
	}}, nil)

	// When
	got := tracker.Status("provider", "")

	// Then
	if got != Probing {
		t.Fatalf("status = %q, want %q", got, Probing)
	}
}

func TestRestore_preserves_unexpired_unhealthy(t *testing.T) {
	// Given
	until := time.Now().Add(time.Minute)
	tracker := NewPersistent([]PersistedState{{
		Provider: "provider", Status: Unhealthy, UnhealthyUntil: &until, BackoffCount: 2,
	}}, nil)

	// When
	got := tracker.Status("provider", "")

	// Then
	if got != Unhealthy {
		t.Fatalf("status = %q, want %q", got, Unhealthy)
	}
}

func TestNewProvider_defaults_to_probing(t *testing.T) {
	// Given
	tracker := New()

	// When
	got := tracker.Status("new-provider", "")

	// Then
	if got != Probing {
		t.Fatalf("status = %q, want %q", got, Probing)
	}
}
