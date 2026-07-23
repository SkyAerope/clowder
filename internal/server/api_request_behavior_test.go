package server

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/SkyAerope/clowder/internal/store"
)

func TestAPI_requests_applies_combined_filters(t *testing.T) {
	// Given
	fixture := newAPIBehaviorFixture(t, true)
	cookie := fixture.login(t)
	status429, status500 := 429, 500
	requests := []*store.RequestLog{
		{TraceID: "target", AttemptNo: 1, Provider: "alpha", Model: "m1", StatusCode: &status429, MatchedSlug: "retry", ErrorMsg: "limited", ErrorFingerprint: "fp-rate-%_target"},
		{TraceID: "other", AttemptNo: 1, Provider: "alpha", Model: "m1", StatusCode: &status500, MatchedSlug: "retry", ErrorMsg: "down", ErrorFingerprint: "fp-rate-%_other"},
	}
	ids := make([]int64, 0, len(requests))
	for _, request := range requests {
		id, err := fixture.app.Store.InsertRequest(request)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	for _, id := range ids {
		if _, err := fixture.app.Store.DB().Exec(`UPDATE requests SET created_at='2026-07-20 12:00:00' WHERE id=?`, id); err != nil {
			t.Fatal(err)
		}
	}
	query := url.Values{
		"from":        {"2026-07-20T12:00:00Z"},
		"to":          {"2026-07-20T12:00:00Z"},
		"provider":    {"alpha"},
		"model":       {"m1"},
		"status":      {"429"},
		"error":       {"true"},
		"rule":        {"retry"},
		"fingerprint": {"fp-rate-%_"},
		"limit":       {"50"},
		"offset":      {"0"},
	}

	// When
	recorder := fixture.request(t, apiRequest{
		method: http.MethodGet,
		path:   "/api/admin/requests?" + query.Encode(),
		cookie: cookie,
	})

	// Then
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	var body []store.RequestLog
	decodeJSON(t, recorder, &body)
	if len(body) != 1 || body[0].ID != ids[0] {
		t.Fatalf("request IDs = %v, want [%d]", body, ids[0])
	}
}

func TestAPI_requests_fingerprint_search_is_case_insensitive_substring(t *testing.T) {
	// Given
	fixture := newAPIBehaviorFixture(t, true)
	cookie := fixture.login(t)
	status500 := 500
	requests := []*store.RequestLog{
		{TraceID: "a", AttemptNo: 1, Provider: "alpha", Model: "m1", StatusCode: &status500, ErrorMsg: "x", ErrorFingerprint: "abcdef1234567890"},
		{TraceID: "b", AttemptNo: 1, Provider: "alpha", Model: "m1", StatusCode: &status500, ErrorMsg: "y", ErrorFingerprint: "ffffffffffffffff"},
	}
	ids := make([]int64, 0, len(requests))
	for _, request := range requests {
		id, err := fixture.app.Store.InsertRequest(request)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}

	// When
	recorder := fixture.request(t, apiRequest{
		method: http.MethodGet,
		path:   "/api/admin/requests?fingerprint=EF1234",
		cookie: cookie,
	})

	// Then
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	var body []store.RequestLog
	decodeJSON(t, recorder, &body)
	if len(body) != 1 || body[0].ID != ids[0] {
		t.Fatalf("request IDs = %v, want [%d]", body, ids[0])
	}
}

func TestAPI_requests_inlines_trace_and_capture(t *testing.T) {
	// Given
	fixture := newAPIBehaviorFixture(t, true)
	cookie := fixture.login(t)
	status429, status200 := 429, 200
	firstID, err := fixture.app.Store.InsertRequest(&store.RequestLog{
		TraceID: "chain", AttemptNo: 1, Provider: "alpha", Model: "m1", StatusCode: &status429,
		ErrorMsg: "limited", ErrorFingerprint: "fp1234567890abcd",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.app.Store.InsertRequest(&store.RequestLog{
		TraceID: "chain", AttemptNo: 2, Provider: "beta", Model: "m1", StatusCode: &status200,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.app.Store.InsertCapture(&store.Capture{
		RequestID: firstID, MatchedSlug: "retry", Provider: "alpha", Fingerprint: "fp1234567890abcd",
		RequestHeaders: `{"a":"1"}`, ResponseBody: "error body",
	}, 0); err != nil {
		t.Fatal(err)
	}

	// When
	recorder := fixture.request(t, apiRequest{method: http.MethodGet, path: "/api/admin/requests", cookie: cookie})

	// Then
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	var rows []requestLogView
	decodeJSON(t, recorder, &rows)
	if len(rows) != 2 {
		t.Fatalf("rows = %#v, want two entries", rows)
	}
	latest := rows[0]
	if latest.AttemptNo != 2 || latest.Capture != nil || len(latest.Trace) != 2 {
		t.Fatalf("latest row = attempt %d, capture %#v, trace len %d", latest.AttemptNo, latest.Capture, len(latest.Trace))
	}
	captured := rows[1]
	if captured.AttemptNo != 1 || captured.Capture == nil || len(captured.Trace) != 2 {
		t.Fatalf("captured row = attempt %d, capture %#v, trace len %d", captured.AttemptNo, captured.Capture, len(captured.Trace))
	}
	if captured.Capture.RequestHeaders != `{"a":"1"}` || captured.Capture.ResponseBody != "error body" {
		t.Fatalf("capture = %#v", captured.Capture)
	}
}

func TestAPI_requests_rejects_invalid_filters(t *testing.T) {
	// Given
	fixture := newAPIBehaviorFixture(t, true)
	cookie := fixture.login(t)
	tests := map[string]string{
		"invalid from":     "from=not-a-time",
		"invalid to":       "to=not-a-time",
		"inverted range":   "from=2026-07-21T00%3A00%3A00Z&to=2026-07-20T00%3A00%3A00Z",
		"invalid status":   "status=abc",
		"status too low":   "status=99",
		"invalid error":    "error=yes",
		"invalid captured": "captured=yes",
	}

	for name, query := range tests {
		t.Run(name, func(t *testing.T) {
			// When
			recorder := fixture.request(t, apiRequest{
				method: http.MethodGet,
				path:   "/api/admin/requests?" + query,
				cookie: cookie,
			})

			// Then
			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d: %s", recorder.Code, http.StatusBadRequest, recorder.Body.String())
			}
			var body map[string]any
			decodeJSON(t, recorder, &body)
			if _, ok := body["error"].(string); !ok {
				t.Fatalf("error response = %#v, want string error", body)
			}
		})
	}
}
