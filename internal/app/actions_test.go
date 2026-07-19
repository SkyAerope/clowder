package app_test

import (
	"io"
	"log/slog"
	"runtime"
	"testing"

	"github.com/SkyAerope/clowder/internal/app"
	"github.com/SkyAerope/clowder/internal/pool"
	"github.com/SkyAerope/clowder/internal/rules"
	"github.com/SkyAerope/clowder/internal/store"
)

func TestRunActions_applies_mark_key_before_returning(t *testing.T) {
	// Given
	previousMaxProcs := runtime.GOMAXPROCS(1)
	t.Cleanup(func() { runtime.GOMAXPROCS(previousMaxProcs) })

	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	p, err := pool.New(st, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	key := &store.Key{Provider: "mock", APIKey: "key", Status: store.KeyActive}
	if err := p.AddKey(key); err != nil {
		t.Fatal(err)
	}
	a := &app.App{Pool: p, Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	ctx := &app.ActionContext{
		Rule:  &rules.Rule{Name: "invalidate", Do: []rules.Action{{Kind: "mark_key", MarkStatus: "invalid"}}},
		Event: &rules.Event{Type: "upstream"},
		KeyID: key.ID,
	}

	// When
	a.RunActions(ctx)

	// Then
	if got := p.FindByID(key.ID).Status; got != store.KeyInvalid {
		t.Fatalf("key status = %q, want %q before RunActions returns", got, store.KeyInvalid)
	}
}
