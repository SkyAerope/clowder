package store_test

import (
	"testing"
	"time"

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
	page, err := st.ListRequests(store.RequestFilters{}, 2, 1)
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

func TestRequests_filter_combines_all_fields_with_inclusive_time_bounds(t *testing.T) {
	// Given
	st := newTestStore(t)
	status429, status500 := 429, 500
	target := &store.RequestLog{
		TraceID: "target", AttemptNo: 1, Provider: "alpha", Model: "m1", StatusCode: &status429,
		MatchedSlug: "retry", ErrorMsg: "limited", ErrorFingerprint: "fp-rate-%_target",
	}
	fixtures := []*store.RequestLog{
		target,
		{TraceID: "before", AttemptNo: 1, Provider: "alpha", Model: "m1", StatusCode: &status429, MatchedSlug: "retry", ErrorMsg: "limited", ErrorFingerprint: "fp-rate-%_before"},
		{TraceID: "after", AttemptNo: 1, Provider: "alpha", Model: "m1", StatusCode: &status429, MatchedSlug: "retry", ErrorMsg: "limited", ErrorFingerprint: "fp-rate-%_after"},
		{TraceID: "provider", AttemptNo: 1, Provider: "beta", Model: "m1", StatusCode: &status429, MatchedSlug: "retry", ErrorMsg: "limited", ErrorFingerprint: "fp-rate-%_provider"},
		{TraceID: "model", AttemptNo: 1, Provider: "alpha", Model: "m2", StatusCode: &status429, MatchedSlug: "retry", ErrorMsg: "limited", ErrorFingerprint: "fp-rate-%_model"},
		{TraceID: "status", AttemptNo: 1, Provider: "alpha", Model: "m1", StatusCode: &status500, MatchedSlug: "retry", ErrorMsg: "limited", ErrorFingerprint: "fp-rate-%_status"},
		{TraceID: "rule", AttemptNo: 1, Provider: "alpha", Model: "m1", StatusCode: &status429, MatchedSlug: "fallback", ErrorMsg: "limited", ErrorFingerprint: "fp-rate-%_rule"},
		{TraceID: "fingerprint", AttemptNo: 1, Provider: "alpha", Model: "m1", StatusCode: &status429, MatchedSlug: "retry", ErrorMsg: "limited", ErrorFingerprint: "fp-rate-XX"},
	}
	ids := make([]int64, 0, len(fixtures))
	for _, fixture := range fixtures {
		id, err := st.InsertRequest(fixture)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	createdAt := []string{
		"2026-07-20 12:00:00", "2026-07-20 11:59:59", "2026-07-20 12:00:01",
		"2026-07-20 12:00:00", "2026-07-20 12:00:00", "2026-07-20 12:00:00",
		"2026-07-20 12:00:00", "2026-07-20 12:00:00",
	}
	for i, id := range ids {
		if _, err := st.DB().Exec(`UPDATE requests SET created_at=? WHERE id=?`, createdAt[i], id); err != nil {
			t.Fatal(err)
		}
	}
	bound := time.Date(2026, time.July, 20, 12, 0, 0, 0, time.UTC)
	isError := true

	// When
	logs, err := st.ListRequests(store.RequestFilters{
		From:        &bound,
		To:          &bound,
		Provider:    "alpha",
		Model:       "m1",
		StatusCode:  &status429,
		Error:       &isError,
		MatchedSlug: "retry",
		Fingerprint: "target",
	}, 50, 0)

	// Then
	if err != nil {
		t.Fatal(err)
	}
	if len(logs) != 1 || logs[0].ID != ids[0] {
		t.Fatalf("filtered request IDs = %v, want [%d]", requestIDs(logs), ids[0])
	}
}

func TestRequests_filter_error_state_before_pagination(t *testing.T) {
	// Given
	st := newTestStore(t)
	status200, status503 := 200, 503
	fixtures := []*store.RequestLog{
		{TraceID: "success-old", AttemptNo: 1, Provider: "alpha", Model: "m1", StatusCode: &status200},
		{TraceID: "failed", AttemptNo: 1, Provider: "alpha", Model: "m1", StatusCode: &status503, ErrorMsg: "down", ErrorFingerprint: "fp-down"},
		{TraceID: "success-new", AttemptNo: 1, Provider: "alpha", Model: "m1", StatusCode: &status200},
	}
	ids := make([]int64, 0, len(fixtures))
	for _, fixture := range fixtures {
		id, err := st.InsertRequest(fixture)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	isError := false

	// When
	logs, err := st.ListRequests(store.RequestFilters{Error: &isError}, 1, 1)

	// Then
	if err != nil {
		t.Fatal(err)
	}
	if len(logs) != 1 || logs[0].ID != ids[0] {
		t.Fatalf("filtered request IDs = %v, want [%d]", requestIDs(logs), ids[0])
	}
}

func TestRequests_filter_captured_state_before_pagination(t *testing.T) {
	// Given
	st := newTestStore(t)
	status200 := 200
	fixtures := []*store.RequestLog{
		{TraceID: "plain-old", AttemptNo: 1, Provider: "alpha", Model: "m1", StatusCode: &status200},
		{TraceID: "captured", AttemptNo: 1, Provider: "alpha", Model: "m1", StatusCode: &status200},
		{TraceID: "plain-new", AttemptNo: 1, Provider: "alpha", Model: "m1", StatusCode: &status200},
	}
	ids := make([]int64, 0, len(fixtures))
	for _, fixture := range fixtures {
		id, err := st.InsertRequest(fixture)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	if _, err := st.InsertCapture(&store.Capture{RequestID: ids[1], MatchedSlug: "retry", Provider: "alpha"}, 0); err != nil {
		t.Fatal(err)
	}
	captured := true

	// When
	capturedLogs, err := st.ListRequests(store.RequestFilters{Captured: &captured}, 50, 0)
	if err != nil {
		t.Fatal(err)
	}
	notCaptured := false
	plainLogs, err := st.ListRequests(store.RequestFilters{Captured: &notCaptured}, 1, 1)

	// Then
	if err != nil {
		t.Fatal(err)
	}
	if len(capturedLogs) != 1 || capturedLogs[0].ID != ids[1] {
		t.Fatalf("captured request IDs = %v, want [%d]", requestIDs(capturedLogs), ids[1])
	}
	if len(plainLogs) != 1 || plainLogs[0].ID != ids[0] {
		t.Fatalf("uncaptured request IDs = %v, want [%d]", requestIDs(plainLogs), ids[0])
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
