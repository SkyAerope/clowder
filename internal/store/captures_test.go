package store_test

import (
	"testing"

	"github.com/SkyAerope/clowder/internal/store"
)

func TestCaptures_insert_and_lookup_by_request(t *testing.T) {
	// Given
	st := newTestStore(t)
	fixtures := []*store.Capture{
		{RequestID: 1, MatchedSlug: "retry", Fingerprint: "fp-a", Provider: "alpha", Model: "m1", RequestHeaders: `{"x":"1"}`, RequestBody: "one", ResponseHeaders: `{"y":"2"}`, ResponseBody: "failed", Redacted: true},
		{RequestID: 2, MatchedSlug: "fallback", Fingerprint: "fp-b", Provider: "beta", Model: "m2", RequestBody: "two"},
		{RequestID: 3, MatchedSlug: "retry", Fingerprint: "fp-a", Provider: "alpha", Model: "m3", RequestBody: "three"},
	}
	for _, fixture := range fixtures {
		if _, err := st.InsertCapture(fixture, 0); err != nil {
			t.Fatal(err)
		}
	}

	// When
	byRequest, err := st.CapturesByRequest([]int64{1, 2, 3})

	// Then
	if err != nil {
		t.Fatal(err)
	}
	if len(byRequest) != 3 {
		t.Fatalf("captures = %#v, want three request entries", byRequest)
	}
	oldest := byRequest[1]
	if oldest.RequestHeaders != `{"x":"1"}` || oldest.RequestBody != "one" || oldest.ResponseHeaders != `{"y":"2"}` ||
		oldest.ResponseBody != "failed" || !oldest.Redacted || oldest.CreatedAt == "" {
		t.Fatalf("oldest capture = %#v", oldest)
	}
}

func TestCaptures_retention_keeps_latest_capture_for_each_fingerprint(t *testing.T) {
	// Given
	st := newTestStore(t)
	fixtures := []*store.Capture{
		{RequestID: 1, MatchedSlug: "retry", Fingerprint: "fp-a", Provider: "alpha", RequestBody: "old-a"},
		{RequestID: 2, MatchedSlug: "retry", Provider: "alpha", RequestBody: "unfingerprinted"},
		{RequestID: 3, MatchedSlug: "retry", Fingerprint: "fp-b", Provider: "alpha", RequestBody: "latest-b"},
		{RequestID: 4, MatchedSlug: "retry", Fingerprint: "fp-a", Provider: "alpha", RequestBody: "latest-a"},
	}
	for _, fixture := range fixtures {
		if _, err := st.InsertCapture(fixture, 2); err != nil {
			t.Fatal(err)
		}
	}

	// When
	got, err := st.CapturesByRequest([]int64{1, 2, 3, 4})

	// Then
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[4] == nil || got[4].Fingerprint != "fp-a" || got[4].RequestBody != "latest-a" ||
		got[3] == nil || got[3].Fingerprint != "fp-b" || got[3].RequestBody != "latest-b" {
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

func TestCaptures_by_request_returns_latest_full_capture_per_request(t *testing.T) {
	// Given
	st := newTestStore(t)
	captures := []*store.Capture{
		{RequestID: 10, MatchedSlug: "retry", Provider: "alpha", RequestHeaders: `{"a":"1"}`, ResponseBody: "old"},
		{RequestID: 10, MatchedSlug: "retry", Provider: "alpha", RequestBody: "new-body", ResponseHeaders: `{"h":"2"}`, ResponseBody: "new"},
		{RequestID: 11, MatchedSlug: "retry", Provider: "alpha", RequestBody: "only-body"},
	}
	for _, capture := range captures {
		if _, err := st.InsertCapture(capture, 0); err != nil {
			t.Fatal(err)
		}
	}

	// When
	byRequest, err := st.CapturesByRequest([]int64{10, 11, 12})

	// Then
	if err != nil {
		t.Fatal(err)
	}
	if len(byRequest) != 2 {
		t.Fatalf("captures = %#v, want only requests 10 and 11", byRequest)
	}
	latest := byRequest[10]
	if latest == nil || latest.RequestBody != "new-body" || latest.RequestHeaders != "" || latest.ResponseBody != "new" {
		t.Fatalf("latest capture for 10 = %#v", latest)
	}
	if only := byRequest[11]; only == nil || only.RequestBody != "only-body" {
		t.Fatalf("capture for 11 = %#v", only)
	}
	if _, ok := byRequest[12]; ok {
		t.Fatalf("request 12 has no capture, want absent, got %#v", byRequest[12])
	}
}
