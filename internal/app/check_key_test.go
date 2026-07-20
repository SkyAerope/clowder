package app

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/SkyAerope/clowder/internal/pool"
	"github.com/SkyAerope/clowder/internal/provider"
	"github.com/SkyAerope/clowder/internal/script"
	"github.com/SkyAerope/clowder/internal/store"
)

func TestCheckKeyWithProbe_checks_key_for_disabled_provider(t *testing.T) {
	// Given
	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "check.sh")
	if err := os.WriteFile(scriptPath, []byte("#!/bin/sh\nprintf '%s\\n' '{\"status\":\"active\",\"metadata\":{\"source\":\"disabled\"}}'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	p, err := pool.New(st, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	key := &store.Key{Provider: "disabled", APIKey: "key", Status: store.KeyIdle}
	if err := p.AddKey(key); err != nil {
		t.Fatal(err)
	}
	manifest := &provider.Manifest{
		Name:    "disabled",
		Enabled: false,
		Dir:     dir,
		Scripts: provider.ScriptsConfig{Check: "./check.sh", CheckTimeout: 1},
	}
	a := &App{
		Pool:                p,
		Runner:              script.NewRunner(0, 0),
		configuredProviders: map[string]*ProviderState{"disabled": {Manifest: manifest}},
		providers:           map[string]*ProviderState{},
		Log:                 slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	// When
	result, err := a.CheckKeyWithProbe(context.Background(), key.ID, false)

	// Then
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "active" {
		t.Fatalf("check status = %q, want active", result.Status)
	}
}
