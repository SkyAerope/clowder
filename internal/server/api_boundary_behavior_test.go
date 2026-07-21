package server

import (
	"net/http"
	"testing"
)

func TestAPI_session_returns_public_and_admin_state_without_credentials(t *testing.T) {
	// Given
	fixture := newAPIBehaviorFixture(t, false)

	// When
	recorder := fixture.request(t, apiRequest{method: http.MethodGet, path: "/api/session"})

	// Then
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	var body map[string]any
	decodeJSON(t, recorder, &body)
	requireObjectKeys(t, body, "admin", "public")
	if body["admin"] != false || body["public"] != false {
		t.Fatalf("session = %#v, want unauthenticated private session", body)
	}
}

func TestAPI_public_route_requires_session_when_dashboard_is_private(t *testing.T) {
	// Given
	fixture := newAPIBehaviorFixture(t, false)

	// When
	recorder := fixture.request(t, apiRequest{method: http.MethodGet, path: "/api/providers"})

	// Then
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
	var body map[string]any
	decodeJSON(t, recorder, &body)
	if body["error"] != "login required" {
		t.Fatalf("error = %v, want login required", body["error"])
	}
}

func TestAPI_public_route_is_available_without_session_when_enabled(t *testing.T) {
	// Given
	fixture := newAPIBehaviorFixture(t, true)

	// When
	recorder := fixture.request(t, apiRequest{method: http.MethodGet, path: "/api/providers"})

	// Then
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	var body []map[string]any
	decodeJSON(t, recorder, &body)
	if len(body) != 1 || body[0]["name"] != "fixture" {
		t.Fatalf("providers = %#v, want fixture provider", body)
	}
}

func TestAPI_admin_route_rejects_request_without_session(t *testing.T) {
	// Given
	fixture := newAPIBehaviorFixture(t, true)

	// When
	recorder := fixture.request(t, apiRequest{method: http.MethodGet, path: "/api/admin/keys"})

	// Then
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
	var body map[string]any
	decodeJSON(t, recorder, &body)
	if body["error"] != "login required" {
		t.Fatalf("error = %v, want login required", body["error"])
	}
}

func TestAPI_login_sets_HTTP_only_session_cookie(t *testing.T) {
	// Given
	fixture := newAPIBehaviorFixture(t, false)

	// When
	recorder := fixture.request(t, apiRequest{
		method: http.MethodPost,
		path:   "/api/login",
		body:   `{"admin_key":"test-admin-key"}`,
	})

	// Then
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	var body map[string]any
	decodeJSON(t, recorder, &body)
	if body["ok"] != true {
		t.Fatalf("login response = %#v, want ok", body)
	}
	result := recorder.Result()
	t.Cleanup(func() { _ = result.Body.Close() })
	cookies := result.Cookies()
	if len(cookies) != 1 || cookies[0].Name != sessionCookie || !cookies[0].HttpOnly || cookies[0].Path != "/" {
		t.Fatalf("cookies = %#v, want HTTP-only session cookie", cookies)
	}
}

func TestAPI_login_rejects_invalid_admin_key(t *testing.T) {
	// Given
	fixture := newAPIBehaviorFixture(t, false)

	// When
	recorder := fixture.request(t, apiRequest{
		method: http.MethodPost,
		path:   "/api/login",
		body:   `{"admin_key":"wrong"}`,
	})

	// Then
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
	var body map[string]any
	decodeJSON(t, recorder, &body)
	if body["error"] != "invalid admin key" {
		t.Fatalf("error = %v, want invalid admin key", body["error"])
	}
}
