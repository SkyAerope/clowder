package proxy_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/SkyAerope/clowder/internal/store"
)

func TestStreamingCaptureStoresReceivedBodyWhenMatchingRuleRequestsIt(t *testing.T) {
	// Given
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Content-Length", "4096")
		w.WriteHeader(http.StatusOK)
		io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"partial\"}}]}\n\n")
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
	}))
	defer up.Close()
	rulesYAML := `
rules:
  - capture-stream-cut:
      when: { transport: stream_cut }
      do:
        - capture: { fields: [response_body] }
      route: passthrough
`
	a, h := setup(t, up.URL, []string{"k1"}, rulesYAML)
	home := os.Getenv("CLOWDER_HOME")

	// When
	w := chatReq(t, h, `{"model":"test-model","stream":true,"messages":[]}`)
	a.Close()
	captures := capturesForRequests(t, home)

	// Then
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	if len(captures) != 1 || !strings.Contains(captures[0].ResponseBody, "partial") {
		t.Fatalf("stream capture body missing: %#v", captures)
	}
}

func TestSuccessfulStreamingCaptureStoresBodyWhenMatchingRuleRequestsIt(t *testing.T) {
	// Given
	up := sseUpstream(nil)
	defer up.Close()
	rulesYAML := `
rules:
  - capture-stream-success:
      when: { status_code: 200 }
      do:
        - capture: { fields: [response_body] }
      route: passthrough
`
	a, h := setup(t, up.URL, []string{"k1"}, rulesYAML)
	home := os.Getenv("CLOWDER_HOME")

	// When
	w := chatReq(t, h, `{"model":"test-model","stream":true,"messages":[]}`)
	a.Close()
	captures := capturesForRequests(t, home)

	// Then
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	if len(captures) != 1 || !strings.Contains(captures[0].ResponseBody, "hello") || !strings.Contains(captures[0].ResponseBody, "world") {
		t.Fatalf("successful stream capture body missing: %#v", captures)
	}
}

func capturesForRequests(t *testing.T, home string) []*store.Capture {
	t.Helper()
	reopened, err := store.Open(home)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	logs, err := reopened.ListRequests(store.RequestFilters{}, 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	requestIDs := make([]int64, len(logs))
	for i, log := range logs {
		requestIDs[i] = log.ID
	}
	byRequest, err := reopened.CapturesByRequest(requestIDs)
	if err != nil {
		t.Fatal(err)
	}
	captures := make([]*store.Capture, 0, len(byRequest))
	for _, log := range logs {
		if capture := byRequest[log.ID]; capture != nil {
			captures = append(captures, capture)
		}
	}
	return captures
}
