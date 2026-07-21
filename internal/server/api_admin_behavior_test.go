package server

import (
	"fmt"
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

func TestAPI_keys_returns_stable_key_view(t *testing.T) {
	// Given
	fixture := newAPIBehaviorFixture(t, true)
	cookie := fixture.login(t)

	// When
	recorder := fixture.request(t, apiRequest{
		method: http.MethodGet,
		path:   "/api/admin/keys?provider=fixture",
		cookie: cookie,
	})

	// Then
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	var body []map[string]any
	decodeJSON(t, recorder, &body)
	if len(body) != 1 {
		t.Fatalf("keys = %#v, want one key", body)
	}
	requireObjectKeys(t, body[0], "api_key", "email", "id", "metadata", "provider", "rate_limit_count", "status")
	if body[0]["provider"] != "fixture" || body[0]["api_key"] != "fixture-secret" || body[0]["metadata"] != `{"tier":"test"}` {
		t.Fatalf("key view = %#v", body[0])
	}
}

func TestAPI_key_status_rejects_invalid_input(t *testing.T) {
	// Given
	fixture := newAPIBehaviorFixture(t, true)
	cookie := fixture.login(t)

	// When
	recorder := fixture.request(t, apiRequest{
		method: http.MethodPost,
		path:   fmt.Sprintf("/api/admin/keys/%d/status", fixture.key.ID),
		body:   `{"status":"unknown"}`,
		cookie: cookie,
	})

	// Then
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}
	var body map[string]any
	decodeJSON(t, recorder, &body)
	if body["error"] != "invalid key status" {
		t.Fatalf("error = %v, want invalid key status", body["error"])
	}
}

func TestAPI_provider_returns_stable_detail_view(t *testing.T) {
	// Given
	fixture := newAPIBehaviorFixture(t, true)
	cookie := fixture.login(t)

	// When
	recorder := fixture.request(t, apiRequest{
		method: http.MethodGet,
		path:   "/api/admin/providers/fixture",
		cookie: cookie,
	})

	// Then
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	var body map[string]any
	decodeJSON(t, recorder, &body)
	requireObjectKeys(t, body, "keys", "last_register_at", "last_register_duration", "last_register_error", "manifest", "manifest_yaml", "models")
	manifest, ok := body["manifest"].(map[string]any)
	if !ok {
		t.Fatalf("manifest type = %T, want object", body["manifest"])
	}
	requireObjectKeys(t, manifest, "base_url", "default_check_model", "display_name", "enabled", "has_check", "has_register", "hedge", "key_strategy", "name", "priority", "weight")
	models, ok := body["models"].([]any)
	if !ok || len(models) != 1 || models[0] != "native-model" {
		t.Fatalf("models = %#v, want native-model array", body["models"])
	}
}

func TestAPI_provider_manifest_rejects_invalid_input(t *testing.T) {
	// Given
	fixture := newAPIBehaviorFixture(t, true)
	cookie := fixture.login(t)

	// When
	recorder := fixture.request(t, apiRequest{
		method: http.MethodPut,
		path:   "/api/admin/providers/fixture/manifest",
		body:   `{"yaml":"name: fixture"}`,
		cookie: cookie,
	})

	// Then
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}
	var body map[string]any
	decodeJSON(t, recorder, &body)
	if _, ok := body["error"].(string); !ok {
		t.Fatalf("error response = %#v, want string error", body)
	}
}

func TestAPI_rules_returns_stable_view_contract(t *testing.T) {
	// Given
	fixture := newAPIBehaviorFixture(t, true)
	cookie := fixture.login(t)

	// When
	recorder := fixture.request(t, apiRequest{method: http.MethodGet, path: "/api/admin/rules", cookie: cookie})

	// Then
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	var body map[string]any
	decodeJSON(t, recorder, &body)
	requireObjectKeys(t, body, "rules", "warnings", "yaml")
	ruleList, ok := body["rules"].([]any)
	if !ok || len(ruleList) == 0 {
		t.Fatalf("rules = %#v, want non-empty array", body["rules"])
	}
	rule, ok := ruleList[0].(map[string]any)
	if !ok {
		t.Fatalf("rule type = %T, want object", ruleList[0])
	}
	requireObjectKeys(t, rule, "builtin", "detail", "enabled", "name", "priority", "slug", "source", "type")
	detail, ok := rule["detail"].(map[string]any)
	if !ok {
		t.Fatalf("detail type = %T, want object", rule["detail"])
	}
	requireObjectKeys(t, detail, "do", "if", "route", "when")
}

func TestAPI_rules_rejects_invalid_input(t *testing.T) {
	// Given
	fixture := newAPIBehaviorFixture(t, true)
	cookie := fixture.login(t)

	// When
	recorder := fixture.request(t, apiRequest{
		method: http.MethodPut,
		path:   "/api/admin/rules",
		body:   `{"yaml":"rules: ["}`,
		cookie: cookie,
	})

	// Then
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}
	var body map[string]any
	decodeJSON(t, recorder, &body)
	if _, ok := body["error"].(string); !ok {
		t.Fatalf("error response = %#v, want string error", body)
	}
}

func TestAPI_config_returns_stable_view_contract(t *testing.T) {
	// Given
	fixture := newAPIBehaviorFixture(t, true)
	cookie := fixture.login(t)

	// When
	recorder := fixture.request(t, apiRequest{method: http.MethodGet, path: "/api/admin/config", cookie: cookie})

	// Then
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	var body map[string]any
	decodeJSON(t, recorder, &body)
	requireObjectKeys(t, body, "config_yaml", "models_yaml")
	if body["models_yaml"] != "{}\n" {
		t.Fatalf("models_yaml = %#v, want fixture content", body["models_yaml"])
	}
	if _, ok := body["config_yaml"].(string); !ok {
		t.Fatalf("config_yaml type = %T, want string", body["config_yaml"])
	}
}

func TestAPI_config_rejects_invalid_input(t *testing.T) {
	// Given
	fixture := newAPIBehaviorFixture(t, true)
	cookie := fixture.login(t)

	// When
	recorder := fixture.request(t, apiRequest{
		method: http.MethodPut,
		path:   "/api/admin/config",
		body:   `{"config_yaml":"port: ["}`,
		cookie: cookie,
	})

	// Then
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}
	var body map[string]any
	decodeJSON(t, recorder, &body)
	if _, ok := body["error"].(string); !ok {
		t.Fatalf("error response = %#v, want string error", body)
	}
}
