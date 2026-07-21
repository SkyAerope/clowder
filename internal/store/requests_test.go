package store_test

import (
	"testing"

	"github.com/SkyAerope/clowder/internal/store"
)

func TestRequests_list_and_trace_apply_documented_order_and_pagination(t *testing.T) {
	// Given
	st := newTestStore(t)
	status429, status200 := 429, 200
	ttft, latency := int64(45), int64(90)
	keyID := int64(17)
	fixtures := []*store.RequestLog{
		{TraceID: "trace-a", AttemptNo: 2, Provider: "beta", Model: "m2", StatusCode: &status200, TTFTMs: &ttft, LatencyMs: &latency},
		{TraceID: "trace-b", AttemptNo: 1, Provider: "gamma", Model: "m3"},
		{TraceID: "trace-a", AttemptNo: 1, KeyID: &keyID, Provider: "alpha", Model: "m1", StatusCode: &status429, MatchedSlug: "retry", ErrorMsg: "limited", ErrorFingerprint: "fp-rate"},
	}
	ids := make([]int64, 0, len(fixtures))
	for _, fixture := range fixtures {
		id, err := st.InsertRequest(fixture)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}

	// When
	page, err := st.ListRequests(2, 1)
	if err != nil {
		t.Fatal(err)
	}
	trace, err := st.ListTrace("trace-a")

	// Then
	if err != nil {
		t.Fatal(err)
	}
	if len(page) != 2 || page[0].ID != ids[1] || page[1].ID != ids[0] {
		t.Fatalf("page IDs = %v, want [%d %d]", requestIDs(page), ids[1], ids[0])
	}
	if len(trace) != 2 || trace[0].AttemptNo != 1 || trace[1].AttemptNo != 2 {
		t.Fatalf("trace attempts = %v, want [1 2]", requestAttempts(trace))
	}
	first := trace[0]
	if first.KeyID == nil || *first.KeyID != keyID || first.StatusCode == nil || *first.StatusCode != status429 ||
		first.MatchedSlug != "retry" || first.ErrorMsg != "limited" || first.ErrorFingerprint != "fp-rate" {
		t.Fatalf("first attempt = %#v", first)
	}
	second := trace[1]
	if second.TTFTMs == nil || *second.TTFTMs != ttft || second.LatencyMs == nil || *second.LatencyMs != latency {
		t.Fatalf("second attempt = %#v", second)
	}
}

func TestErrors_aggregate_distinct_values_and_filter_by_rule(t *testing.T) {
	// Given
	st := newTestStore(t)
	status429, status503 := 429, 503
	fixtures := []*store.RequestLog{
		{TraceID: "t1", AttemptNo: 1, Provider: "alpha", Model: "m1", StatusCode: &status429, MatchedSlug: "retry", ErrorMsg: "first", ErrorFingerprint: "fp-a"},
		{TraceID: "t2", AttemptNo: 1, Provider: "alpha", Model: "m2", StatusCode: &status429, MatchedSlug: "fallback", ErrorMsg: "second", ErrorFingerprint: "fp-a"},
		{TraceID: "t3", AttemptNo: 1, Provider: "alpha", Model: "m1", StatusCode: &status429, MatchedSlug: "retry", ErrorMsg: "third", ErrorFingerprint: "fp-a"},
		{TraceID: "t4", AttemptNo: 1, Provider: "beta", Model: "m3", StatusCode: &status503, MatchedSlug: "retry", ErrorMsg: "unavailable", ErrorFingerprint: "fp-b"},
		{TraceID: "t5", AttemptNo: 1, Provider: "alpha", Model: "m1", StatusCode: &status503, MatchedSlug: "retry", ErrorMsg: "not grouped"},
	}
	for _, fixture := range fixtures {
		if _, err := st.InsertRequest(fixture); err != nil {
			t.Fatal(err)
		}
	}

	// When
	groups, err := st.AggregateErrors("", 10)
	if err != nil {
		t.Fatal(err)
	}
	retryGroups, err := st.AggregateErrors("retry", 10)

	// Then
	if err != nil {
		t.Fatal(err)
	}
	byFingerprint := indexErrorGroups(groups)
	if len(byFingerprint) != 2 {
		t.Fatalf("groups = %#v, want 2 fingerprints", groups)
	}
	groupA := byFingerprint["fp-a"]
	if groupA == nil || groupA.Provider != "alpha" || groupA.StatusCode == nil || *groupA.StatusCode != status429 ||
		groupA.Count != 3 || groupA.Models != "m1,m2" || groupA.Rules != "retry,fallback" || groupA.SampleError != "third" {
		t.Fatalf("fp-a group = %#v", groupA)
	}
	retryByFingerprint := indexErrorGroups(retryGroups)
	if len(retryByFingerprint) != 2 || retryByFingerprint["fp-a"].Count != 2 || retryByFingerprint["fp-a"].Rules != "retry" {
		t.Fatalf("retry groups = %#v", retryGroups)
	}
}

func requestIDs(requests []*store.RequestLog) []int64 {
	ids := make([]int64, len(requests))
	for i, request := range requests {
		ids[i] = request.ID
	}
	return ids
}

func requestAttempts(requests []*store.RequestLog) []int {
	attempts := make([]int, len(requests))
	for i, request := range requests {
		attempts[i] = request.AttemptNo
	}
	return attempts
}

func indexErrorGroups(groups []*store.ErrorGroup) map[string]*store.ErrorGroup {
	indexed := make(map[string]*store.ErrorGroup, len(groups))
	for _, group := range groups {
		indexed[group.Fingerprint] = group
	}
	return indexed
}
