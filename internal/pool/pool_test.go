package pool

import (
	"sync"
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

func TestPickChoosesLowestIdleID(t *testing.T) {
	p, cleanup := newTestPool(t, store.KeyIdle, store.KeyIdle, store.KeyIdle)
	defer cleanup()

	keys := p.Keys("test")
	selected, err := p.Pick("test", nil)
	if err != nil {
		t.Fatal(err)
	}
	if selected.ID != keys[0].ID {
		t.Fatalf("selected ID = %d, want lowest ID %d", selected.ID, keys[0].ID)
	}
}

func TestPickRecoversExpiredKeyWhenNoActive(t *testing.T) {
	p, cleanup := newTestPool(t, store.KeyRateLimited, store.KeyRateLimited)
	defer cleanup()

	keys := p.Keys("test")
	if err := p.MarkRateLimited(keys[0].ID, time.Now().Add(-time.Second), 2); err != nil {
		t.Fatal(err)
	}
	if err := p.MarkRateLimited(keys[1].ID, time.Now().Add(time.Minute), 3); err != nil {
		t.Fatal(err)
	}
	selected, err := p.Pick("test", nil)
	if err != nil {
		t.Fatal(err)
	}
	if selected.ID != keys[0].ID || selected.Status != store.KeyActive {
		t.Fatalf("selected = id %d status %q, want expired key %d active", selected.ID, selected.Status, keys[0].ID)
	}

	rows, err := p.st.ListKeys("test")
	if err != nil {
		t.Fatal(err)
	}
	if rows[0].Status != store.KeyActive || rows[0].RateLimitUntil != nil {
		t.Fatalf("persisted expired key = status %q until %v", rows[0].Status, rows[0].RateLimitUntil)
	}
}

func TestPickReturnsActiveBeforeBackgroundRecovery(t *testing.T) {
	p, cleanup := newTestPool(t, store.KeyActive, store.KeyRateLimited)
	defer cleanup()

	keys := p.Keys("test")
	if err := p.MarkRateLimited(keys[1].ID, time.Now().Add(-time.Second), 4); err != nil {
		t.Fatal(err)
	}
	selected, err := p.Pick("test", nil)
	if err != nil {
		t.Fatal(err)
	}
	if selected.ID != keys[0].ID {
		t.Fatalf("selected ID = %d, want existing active %d", selected.ID, keys[0].ID)
	}

	deadline := time.Now().Add(time.Second)
	for {
		rows, err := p.st.ListKeys("test")
		if err != nil {
			t.Fatal(err)
		}
		if rows[1].Status == store.KeyIdle && rows[1].RateLimitUntil == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("expired key was not recovered: status %q until %v", rows[1].Status, rows[1].RateLimitUntil)
		}
		time.Sleep(time.Millisecond)
	}
}

func TestConcurrentPickKeepsSingleActive(t *testing.T) {
	p, cleanup := newTestPool(t, store.KeyIdle, store.KeyIdle, store.KeyIdle)
	defer cleanup()

	var wg sync.WaitGroup
	for range 100 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := p.Pick("test", nil); err != nil {
				t.Errorf("Pick: %v", err)
			}
		}()
	}
	wg.Wait()

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

func TestReadPathsRecoverExpiredKeys(t *testing.T) {
	tests := []struct {
		name string
		read func(*testing.T, *Pool, int64)
	}{
		{name: "Keys", read: func(t *testing.T, p *Pool, id int64) {
			got := p.Keys("test")
			if len(got) != 1 || got[0].ID != id || got[0].Status != store.KeyIdle || got[0].RateLimitUntil != nil || got[0].RateLimitCount != 5 {
				t.Fatalf("Keys result = %#v", got)
			}
		}},
		{name: "UsableCount", read: func(t *testing.T, p *Pool, _ int64) {
			usable, total := p.UsableCount("test")
			if usable != 1 || total != 1 {
				t.Fatalf("UsableCount result = %d/%d, want 1/1", usable, total)
			}
		}},
		{name: "FindByID", read: func(t *testing.T, p *Pool, id int64) {
			got := p.FindByID(id)
			if got == nil || got.ID != id || got.Status != store.KeyIdle || got.RateLimitUntil != nil || got.RateLimitCount != 5 {
				t.Fatalf("FindByID result = %#v", got)
			}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, cleanup := newTestPool(t, store.KeyRateLimited)
			defer cleanup()

			keyID := p.Keys("test")[0].ID
			if err := p.MarkRateLimited(keyID, time.Now().Add(-time.Second), 5); err != nil {
				t.Fatal(err)
			}
			tt.read(t, p, keyID)
			rows, err := p.st.ListKeys("test")
			if err != nil {
				t.Fatal(err)
			}
			if rows[0].Status != store.KeyIdle || rows[0].RateLimitUntil != nil || rows[0].RateLimitCount != 5 {
				t.Fatalf("persisted key = status %q until %v count %d", rows[0].Status, rows[0].RateLimitUntil, rows[0].RateLimitCount)
			}
		})
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
