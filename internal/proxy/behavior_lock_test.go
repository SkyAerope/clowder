package proxy_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestChatRejectsInvalidJSON(t *testing.T) {
	// Given
	up := sseUpstream(nil)
	defer up.Close()
	_, handler := setup(t, up.URL, []string{"k1"}, "")

	// When
	response := chatReq(t, handler, `{`)

	// Then
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status %d: %s", response.Code, response.Body.String())
	}
}

func TestChatRejectsRequestWithoutModel(t *testing.T) {
	// Given
	up := sseUpstream(nil)
	defer up.Close()
	_, handler := setup(t, up.URL, []string{"k1"}, "")

	// When
	response := chatReq(t, handler, `{"messages":[]}`)

	// Then
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status %d: %s", response.Code, response.Body.String())
	}
}

func TestChatReturnsServiceUnavailableWithoutUsableKeys(t *testing.T) {
	// Given
	var upstreamCalls atomic.Int32
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamCalls.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer up.Close()
	app, handler := setup(t, up.URL, []string{"k1"}, "")
	for _, key := range app.Pool.Keys("mock") {
		if err := app.Pool.SetStatus(key.ID, "invalid"); err != nil {
			t.Fatal(err)
		}
	}

	// When
	response := chatReq(t, handler, `{"model":"test-model","messages":[]}`)

	// Then
	if response.Code != http.StatusServiceUnavailable || upstreamCalls.Load() != 0 {
		t.Fatalf("status %d with %d upstream calls: %s", response.Code, upstreamCalls.Load(), response.Body.String())
	}
}

func TestChatDeliversUpstreamTransportError(t *testing.T) {
	// Given
	up := sseUpstream(nil)
	upstreamURL := up.URL
	up.Close()
	_, handler := setup(t, upstreamURL, []string{"k1"}, "")

	// When
	response := chatReq(t, handler, `{"model":"test-model","messages":[]}`)

	// Then
	var body struct {
		Error struct {
			Type string `json:"type"`
		} `json:"error"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if response.Code != http.StatusBadGateway || body.Error.Type != "upstream_error" {
		t.Fatalf("status %d, error type %q: %s", response.Code, body.Error.Type, response.Body.String())
	}
}

func TestRetrySelectsRuleAfterOneKeySwitch(t *testing.T) {
	// Given
	up := sseUpstream(func(w http.ResponseWriter, r *http.Request) bool {
		w.WriteHeader(http.StatusTooManyRequests)
		fmt.Fprint(w, `{"error":"rate limited"}`)
		return true
	})
	defer up.Close()
	rulesYAML := `
rules:
  - stop-after-key-switch:
      priority: 50
      when: { status_code: 429 }
      if:
        key_switches: { gte: 1 }
      route: passthrough
  - switch-key:
      priority: 100
      when: { status_code: 429 }
      route: next_key
`
	app, handler := setup(t, up.URL, []string{"k1", "k2"}, rulesYAML)

	// When
	chatReq(t, handler, `{"model":"test-model","messages":[]}`)

	// Then
	logs, err := app.Store.ListRequests(10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(logs) != 2 {
		t.Fatalf("request logs = %d, want 2", len(logs))
	}
	if logs[0].MatchedSlug != "stop-after-key-switch" || logs[1].MatchedSlug != "switch-key" {
		t.Fatalf("matched slugs = %q, %q", logs[0].MatchedSlug, logs[1].MatchedSlug)
	}
}
