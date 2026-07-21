package store_test

import (
	"testing"

	"github.com/SkyAerope/clowder/internal/store"
)

func TestCaptures_insert_list_filter_and_limit_newest_first(t *testing.T) {
	// Given
	st := newTestStore(t)
	fixtures := []*store.Capture{
		{RequestID: 1, MatchedRule: "retry", Fingerprint: "fp-a", Provider: "alpha", Model: "m1", RequestHeaders: `{"x":"1"}`, RequestBody: "one", ResponseHeaders: `{"y":"2"}`, ResponseBody: "failed", Redacted: true},
		{RequestID: 2, MatchedRule: "fallback", Fingerprint: "fp-b", Provider: "beta", Model: "m2", RequestBody: "two"},
		{RequestID: 3, MatchedRule: "retry", Fingerprint: "fp-a", Provider: "alpha", Model: "m3", RequestBody: "three"},
	}
	ids := make([]int64, 0, len(fixtures))
	for _, fixture := range fixtures {
		id, err := st.InsertCapture(fixture, 0)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}

	// When
	all, err := st.ListCaptures("", "", 2)
	if err != nil {
		t.Fatal(err)
	}
	filtered, err := st.ListCaptures("retry", "fp-a", 10)

	// Then
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 || all[0].ID != ids[2] || all[1].ID != ids[1] {
		t.Fatalf("capture IDs = %v, want [%d %d]", captureIDs(all), ids[2], ids[1])
	}
	if len(filtered) != 2 || filtered[0].ID != ids[2] || filtered[1].ID != ids[0] {
		t.Fatalf("filtered IDs = %v, want [%d %d]", captureIDs(filtered), ids[2], ids[0])
	}
	oldest := filtered[1]
	if oldest.RequestHeaders != `{"x":"1"}` || oldest.RequestBody != "one" || oldest.ResponseHeaders != `{"y":"2"}` ||
		oldest.ResponseBody != "failed" || !oldest.Redacted || oldest.CreatedAt == "" {
		t.Fatalf("oldest capture = %#v", oldest)
	}
}

func TestCaptures_retention_keeps_latest_capture_for_each_fingerprint(t *testing.T) {
	// Given
	st := newTestStore(t)
	fixtures := []*store.Capture{
		{RequestID: 1, MatchedRule: "retry", Fingerprint: "fp-a", Provider: "alpha", RequestBody: "old-a"},
		{RequestID: 2, MatchedRule: "retry", Provider: "alpha", RequestBody: "unfingerprinted"},
		{RequestID: 3, MatchedRule: "retry", Fingerprint: "fp-b", Provider: "alpha", RequestBody: "latest-b"},
		{RequestID: 4, MatchedRule: "retry", Fingerprint: "fp-a", Provider: "alpha", RequestBody: "latest-a"},
	}
	for _, fixture := range fixtures {
		if _, err := st.InsertCapture(fixture, 2); err != nil {
			t.Fatal(err)
		}
	}

	// When
	got, err := st.ListCaptures("", "", 10)

	// Then
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Fingerprint != "fp-a" || got[0].RequestBody != "latest-a" ||
		got[1].Fingerprint != "fp-b" || got[1].RequestBody != "latest-b" {
		t.Fatalf("retained captures = %#v", got)
	}
}

func TestCaptures_avg_ttft_uses_latest_successful_matching_requests(t *testing.T) {
	// Given
	st := newTestStore(t)
	values := []struct {
		ttft        int64
		fingerprint string
	}{
		{ttft: 100},
		{ttft: 1000, fingerprint: "failed"},
		{ttft: 300},
		{ttft: 500},
	}
	for i, value := range values {
		if _, err := st.InsertRequest(&store.RequestLog{
			TraceID: "trace", AttemptNo: i + 1, Provider: "alpha", Model: "m1",
			TTFTMs: &value.ttft, ErrorFingerprint: value.fingerprint,
		}); err != nil {
			t.Fatal(err)
		}
	}

	// When
	avg, ok := st.AvgTTFT("alpha", "m1", 2)
	emptyAvg, emptyOK := st.AvgTTFT("beta", "m1", 2)

	// Then
	if !ok || avg != 400 {
		t.Fatalf("average = (%d, %v), want (400, true)", avg, ok)
	}
	if emptyOK || emptyAvg != 0 {
		t.Fatalf("empty average = (%d, %v), want (0, false)", emptyAvg, emptyOK)
	}
}

func captureIDs(captures []*store.Capture) []int64 {
	ids := make([]int64, len(captures))
	for i, capture := range captures {
		ids[i] = capture.ID
	}
	return ids
}
