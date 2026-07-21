package proxy_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestNonStreamingAggregation(t *testing.T) {
	up := sseUpstream(nil)
	defer up.Close()
	_, h := setup(t, up.URL, []string{"k1"}, "")

	w := chatReq(t, h, `{"model":"test-model","messages":[{"role":"user","content":"hi"}]}`)
	if w.Code != 200 {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Model   string `json:"model"`
		Choices []struct {
			Message struct{ Content string }
		}
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Choices[0].Message.Content != "hello world" {
		t.Fatalf("aggregated content = %q", resp.Choices[0].Message.Content)
	}
	if resp.Model != "test-model" {
		t.Fatalf("model should be canonical name, got %q", resp.Model)
	}
}

func TestSuccessfulRequest_persists_healthy_provider_state(t *testing.T) {
	// Given
	up := sseUpstream(nil)
	defer up.Close()
	a, h := setup(t, up.URL, []string{"k1"}, "")

	// When
	w := chatReq(t, h, `{"model":"test-model","messages":[]}`)

	// Then
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	snapshots, err := a.Store.ListHealth()
	if err != nil {
		t.Fatal(err)
	}
	for _, snapshot := range snapshots {
		if snapshot.Provider == "mock" && snapshot.Model == "" && snapshot.Status == "healthy" {
			return
		}
	}
	t.Fatalf("provider healthy state not persisted: %#v", snapshots)
}

func TestStreamingPassthrough(t *testing.T) {
	up := sseUpstream(nil)
	defer up.Close()
	_, h := setup(t, up.URL, []string{"k1"}, "")

	w := chatReq(t, h, `{"model":"test-model","stream":true,"messages":[]}`)
	if w.Code != 200 {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "text/event-stream") {
		t.Fatalf("content-type = %q", ct)
	}
	if !strings.Contains(w.Body.String(), "data: [DONE]") {
		t.Fatalf("missing [DONE]: %s", w.Body.String())
	}
}
