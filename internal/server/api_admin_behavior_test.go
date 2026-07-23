package server

import (
	"fmt"
	"net/http"
	"testing"
)

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
