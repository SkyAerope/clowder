package store_test

import (
	"testing"
	"time"

	"github.com/SkyAerope/clowder/internal/store"
)

func TestKeys_insert_and_list_preserve_values_defaults_and_provider_order(t *testing.T) {
	// Given
	st := newTestStore(t)
	firstID, err := st.InsertKey(&store.Key{
		Provider: "alpha", APIKey: "key-alpha-1", Email: "alpha@example.test",
		Password: "secret", Metadata: `{"region":"east"}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	secondID, err := st.InsertKey(&store.Key{
		Provider: "beta", APIKey: "key-beta", Status: store.KeyActive,
	})
	if err != nil {
		t.Fatal(err)
	}
	thirdID, err := st.InsertKey(&store.Key{Provider: "alpha", APIKey: "key-alpha-2"})
	if err != nil {
		t.Fatal(err)
	}

	// When
	all, err := st.ListKeys("")
	if err != nil {
		t.Fatal(err)
	}
	alpha, err := st.ListKeys("alpha")

	// Then
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 3 || all[0].ID != firstID || all[1].ID != secondID || all[2].ID != thirdID {
		t.Fatalf("all keys IDs = %v, want [%d %d %d]", keyIDs(all), firstID, secondID, thirdID)
	}
	if len(alpha) != 2 || alpha[0].ID != firstID || alpha[1].ID != thirdID {
		t.Fatalf("alpha key IDs = %v, want [%d %d]", keyIDs(alpha), firstID, thirdID)
	}
	got := alpha[0]
	if got.Provider != "alpha" || got.APIKey != "key-alpha-1" || got.Email != "alpha@example.test" ||
		got.Password != "secret" || got.Status != store.KeyIdle || got.Metadata != `{"region":"east"}` {
		t.Fatalf("first key = %#v", got)
	}
	if got.CreatedAt.IsZero() || got.RateLimitUntil != nil || got.FirstRequestAt != nil || got.LastUsedAt != nil {
		t.Fatalf("first key timestamps = %#v", got)
	}
}

func TestKeys_status_metadata_and_touch_roundtrip(t *testing.T) {
	// Given
	st := newTestStore(t)
	id, err := st.InsertKey(&store.Key{Provider: "alpha", APIKey: "key-alpha"})
	if err != nil {
		t.Fatal(err)
	}
	until := time.Date(2026, 7, 20, 12, 34, 56, 0, time.UTC)

	// When
	if err := st.UpdateKeyStatus(id, store.KeyRateLimited, &until, 3); err != nil {
		t.Fatal(err)
	}
	if err := st.UpdateKeyMetadata(id, `{"account":"primary"}`); err != nil {
		t.Fatal(err)
	}
	if err := st.TouchKey(id); err != nil {
		t.Fatal(err)
	}
	keys, err := st.ListKeys("alpha")

	// Then
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 1 {
		t.Fatalf("keys = %d, want 1", len(keys))
	}
	got := keys[0]
	if got.Status != store.KeyRateLimited || got.RateLimitCount != 3 || got.RateLimitUntil == nil || !got.RateLimitUntil.Equal(until) {
		t.Fatalf("rate limit state = %#v", got)
	}
	if got.Metadata != `{"account":"primary"}` || got.FirstRequestAt == nil || got.LastUsedAt == nil || !got.FirstRequestAt.Equal(*got.LastUsedAt) {
		t.Fatalf("metadata/use state = %#v", got)
	}
}

func TestKeys_reset_backoff_clears_limit_without_changing_status(t *testing.T) {
	// Given
	st := newTestStore(t)
	id, err := st.InsertKey(&store.Key{Provider: "alpha", APIKey: "key-alpha"})
	if err != nil {
		t.Fatal(err)
	}
	until := time.Date(2026, 7, 20, 12, 34, 56, 0, time.UTC)
	if err := st.UpdateKeyStatus(id, store.KeyRateLimited, &until, 3); err != nil {
		t.Fatal(err)
	}

	// When
	if err := st.ResetKeyBackoff(id); err != nil {
		t.Fatal(err)
	}
	keys, err := st.ListKeys("alpha")

	// Then
	if err != nil {
		t.Fatal(err)
	}
	if keys[0].RateLimitCount != 0 || keys[0].RateLimitUntil != nil || keys[0].Status != store.KeyRateLimited {
		t.Fatalf("reset key = %#v", keys[0])
	}
}

func TestKeys_count_reports_total_and_usable_inventory(t *testing.T) {
	// Given
	st := newTestStore(t)
	for _, key := range []*store.Key{
		{Provider: "alpha", APIKey: "idle", Status: store.KeyIdle},
		{Provider: "alpha", APIKey: "active", Status: store.KeyActive},
		{Provider: "alpha", APIKey: "invalid", Status: store.KeyInvalid},
	} {
		_, err := st.InsertKey(key)
		if err != nil {
			t.Fatal(err)
		}
	}

	// When
	total, usable, err := st.CountKeys("alpha")

	// Then
	if err != nil {
		t.Fatal(err)
	}
	if total != 3 || usable != 2 {
		t.Fatalf("counts = (%d, %d), want (3, 2)", total, usable)
	}
}

func TestKeys_delete_removes_only_selected_key(t *testing.T) {
	// Given
	st := newTestStore(t)
	ids := make([]int64, 0, 3)
	for _, apiKey := range []string{"first", "second", "third"} {
		id, err := st.InsertKey(&store.Key{Provider: "alpha", APIKey: apiKey})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}

	// When
	if err := st.DeleteKey(ids[1]); err != nil {
		t.Fatal(err)
	}
	remaining, err := st.ListKeys("alpha")

	// Then
	if err != nil {
		t.Fatal(err)
	}
	if len(remaining) != 2 || remaining[0].ID != ids[0] || remaining[1].ID != ids[2] {
		t.Fatalf("remaining IDs = %v, want [%d %d]", keyIDs(remaining), ids[0], ids[2])
	}
}

func keyIDs(keys []*store.Key) []int64 {
	ids := make([]int64, len(keys))
	for i, key := range keys {
		ids[i] = key.ID
	}
	return ids
}
