package proxy_test

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAuthRequired(t *testing.T) {
	up := sseUpstream(nil)
	defer up.Close()
	_, h := setup(t, up.URL, []string{"k1"}, "")

	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{}`))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 401 {
		t.Fatalf("status %d, want 401 without api key", w.Code)
	}
}
