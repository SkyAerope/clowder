package server

import (
	"testing"

	"github.com/SkyAerope/clowder/internal/store"
)

func TestKeyView_includes_metadata(t *testing.T) {
	// Given
	key := &store.Key{ID: 1, Provider: "test", Metadata: `{"balance":100}`}

	// When
	view := keyView(key)

	// Then
	if got := view["metadata"]; got != key.Metadata {
		t.Fatalf("metadata = %v, want %q", got, key.Metadata)
	}
}
