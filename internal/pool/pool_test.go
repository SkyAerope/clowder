package pool

import (
	"testing"
	"time"

	"github.com/SkyAerope/clowder/internal/store"
)

func newTestPool(t *testing.T, statuses ...string) (*Pool, func()) {
	t.Helper()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	p, err := New(st, time.Second, time.Minute)
	if err != nil {
		_ = st.Close()
		t.Fatal(err)
	}
	for i, status := range statuses {
		if err := p.AddKey(&store.Key{Provider: "test", APIKey: string(rune('a' + i)), Status: status}); err != nil {
			_ = st.Close()
			t.Fatal(err)
		}
	}
	return p, func() { _ = st.Close() }
}

func TestPickKeepsSingleActive(t *testing.T) {
	p, cleanup := newTestPool(t, store.KeyActive, store.KeyActive, store.KeyIdle)
	defer cleanup()
	if _, err := p.Pick("test", nil); err != nil {
		t.Fatal(err)
	}
	active := 0
	for _, k := range p.Keys("test") {
		if k.Status == store.KeyActive {
			active++
		}
	}
	if active != 1 {
		t.Fatalf("active keys = %d, want 1", active)
	}
}

func TestPickPromotesIdleWhenNoActive(t *testing.T) {
	p, cleanup := newTestPool(t, store.KeyIdle, store.KeyIdle)
	defer cleanup()
	selected, err := p.Pick("test", nil)
	if err != nil {
		t.Fatal(err)
	}
	if selected.Status != store.KeyActive {
		t.Fatalf("selected status = %q, want active", selected.Status)
	}
}

func TestPickDoesNotSelectIdleWhileActiveExists(t *testing.T) {
	p, cleanup := newTestPool(t, store.KeyActive, store.KeyIdle)
	defer cleanup()
	selected, err := p.Pick("test", nil)
	if err != nil {
		t.Fatal(err)
	}
	if selected.Status != store.KeyActive {
		t.Fatalf("selected status = %q, want active", selected.Status)
	}
}

func TestUsableCountIncludesActiveAndIdle(t *testing.T) {
	p, cleanup := newTestPool(t, store.KeyActive, store.KeyIdle, store.KeyInvalid, store.KeyRateLimited)
	defer cleanup()

	usable, total := p.UsableCount("test")
	if usable != 2 || total != 4 {
		t.Fatalf("usable/total = %d/%d, want 2/4", usable, total)
	}
}

func TestSetStatusActiveKeepsSingleActive(t *testing.T) {
	p, cleanup := newTestPool(t, store.KeyActive, store.KeyIdle, store.KeyIdle)
	defer cleanup()

	keys := p.Keys("test")
	if err := p.SetStatus(keys[2].ID, store.KeyActive); err != nil {
		t.Fatal(err)
	}
	active := 0
	for _, k := range p.Keys("test") {
		if k.Status == store.KeyActive {
			active++
			if k.ID != keys[2].ID {
				t.Fatalf("unexpected active key %d", k.ID)
			}
		}
	}
	if active != 1 {
		t.Fatalf("active keys = %d, want 1", active)
	}

	rows, err := p.st.ListKeys("test")
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range rows {
		if k.ID == keys[2].ID && k.Status != store.KeyActive {
			t.Fatalf("persisted selected status = %q, want active", k.Status)
		}
	}
}
