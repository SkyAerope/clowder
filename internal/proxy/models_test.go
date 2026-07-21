package proxy_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/SkyAerope/clowder/internal/server"
)

func TestModelNotFound(t *testing.T) {
	up := sseUpstream(nil)
	defer up.Close()
	_, h := setup(t, up.URL, []string{"k1"}, "")

	w := chatReq(t, h, `{"model":"nonexistent-model-xyz","messages":[]}`)
	if w.Code != 404 {
		t.Fatalf("status %d, want 404", w.Code)
	}
}

func TestModelsEndpoint(t *testing.T) {
	var upstreamModelCalls int32
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/models" {
			atomic.AddInt32(&upstreamModelCalls, 1)
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"data":[{"id":"mock/test-model-v1"}]}`)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, sseBody)
	}))
	defer up.Close()
	a, _ := setup(t, up.URL, []string{"k1"}, "")
	modelsPath := filepath.Join(os.Getenv("CLOWDER_HOME"), "models.yaml")
	if err := os.WriteFile(modelsPath, []byte(`
test-model:
  - mock/test-model-v1
patterns:
  - match: ^mock/unmapped-model$
    canonical: pattern-model
`), 0o644); err != nil {
		t.Fatal(err)
	}
	disabledDir := filepath.Join(os.Getenv("CLOWDER_HOME"), "providers", "disabled")
	if err := os.MkdirAll(disabledDir, 0o755); err != nil {
		t.Fatal(err)
	}
	disabledManifest := fmt.Sprintf(`
name: disabled
display_name: Disabled Provider
base_url: %s
enabled: false
models: [disabled-model]
`, up.URL)
	if err := os.WriteFile(filepath.Join(disabledDir, "manifest.yaml"), []byte(disabledManifest), 0o644); err != nil {
		t.Fatal(err)
	}
	emptyDir := filepath.Join(os.Getenv("CLOWDER_HOME"), "providers", "empty")
	if err := os.MkdirAll(emptyDir, 0o755); err != nil {
		t.Fatal(err)
	}
	emptyManifest := fmt.Sprintf(`
name: empty
display_name: Empty Provider
base_url: %s
enabled: true
models: []
`, up.URL)
	if err := os.WriteFile(filepath.Join(emptyDir, "manifest.yaml"), []byte(emptyManifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := a.Reload(); err != nil {
		t.Fatal(err)
	}
	if _, _, err := a.ImportKeys("empty", []string{"empty-key"}); err != nil {
		t.Fatal(err)
	}
	if a.ConfiguredProvider("disabled") == nil || a.Provider("disabled") != nil {
		t.Fatal("disabled provider must be configured but excluded from runtime routing")
	}
	a.Cfg.Dashboard.Admin = true
	h := server.New(a).Handler()

	req := httptest.NewRequest("GET", "/v1/models", nil)
	req.Header.Set("Authorization", "Bearer test-key")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Object string `json:"object"`
		Data   []struct {
			ID      string `json:"id"`
			Object  string `json:"object"`
			OwnedBy string `json:"owned_by"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Object != "list" || len(resp.Data) != 5 {
		t.Fatalf("unexpected models response: %+v", resp)
	}
	if got := resp.Data[0]; got.ID != "auto" || got.Object != "model" || got.OwnedBy != "clowder" {
		t.Fatalf("unexpected auto model: %+v", got)
	}
	wantIDs := []string{"auto", "mock/test-model-v1", "mock/unmapped-model", "pattern-model", "test-model"}
	for i, got := range resp.Data {
		if got.ID != wantIDs[i] || got.Object != "model" || got.OwnedBy != "clowder" {
			t.Fatalf("model[%d] = %+v, want id %q", i, got, wantIDs[i])
		}
	}
	if calls := atomic.LoadInt32(&upstreamModelCalls); calls != 0 {
		t.Fatalf("GET /v1/models made %d upstream model requests", calls)
	}

	apiReq := httptest.NewRequest("GET", "/api/models", nil)
	apiW := httptest.NewRecorder()
	h.ServeHTTP(apiW, apiReq)
	if apiW.Code != http.StatusOK {
		t.Fatalf("GET /api/models status %d: %s", apiW.Code, apiW.Body.String())
	}
	var models map[string][]struct {
		Provider            string `json:"provider"`
		ProviderDisplayName string `json:"provider_display_name"`
		Native              string `json:"native"`
	}
	if err := json.Unmarshal(apiW.Body.Bytes(), &models); err != nil {
		t.Fatal(err)
	}
	edges := models["test-model"]
	if len(edges) != 1 || edges[0].Provider != "mock" || edges[0].ProviderDisplayName != "Mock" || edges[0].Native != "mock/test-model-v1" {
		t.Fatalf("unexpected /api/models response: %s", apiW.Body.String())
	}
	patternEdges := models["pattern-model"]
	if len(patternEdges) != 1 || patternEdges[0].Provider != "mock" || patternEdges[0].Native != "mock/unmapped-model" {
		t.Fatalf("unexpected pattern-only canonical: %s", apiW.Body.String())
	}
	if len(models[""]) != 0 {
		t.Fatalf("pattern-matched model must not remain unmapped: %s", apiW.Body.String())
	}
	if _, exists := models["disabled-model"]; exists {
		t.Fatal("disabled provider model must not be exposed")
	}
	if calls := atomic.LoadInt32(&upstreamModelCalls); calls != 0 {
		t.Fatalf("model list endpoints made %d upstream model requests", calls)
	}
	for _, key := range a.Pool.Keys("mock") {
		if err := a.Pool.SetStatus(key.ID, "invalid"); err != nil {
			t.Fatal(err)
		}
	}
	if candidates := a.CandidatesFor("test-model"); len(candidates) != 0 {
		t.Fatalf("provider without usable keys returned %d candidates", len(candidates))
	}

	unavailableModelsReq := httptest.NewRequest("GET", "/api/models", nil)
	unavailableModelsW := httptest.NewRecorder()
	h.ServeHTTP(unavailableModelsW, unavailableModelsReq)
	if unavailableModelsW.Code != http.StatusOK {
		t.Fatalf("GET /api/models status %d: %s", unavailableModelsW.Code, unavailableModelsW.Body.String())
	}
	var unavailableModels map[string][]struct {
		Provider string `json:"provider"`
		Healthy  bool   `json:"healthy"`
		Best     bool   `json:"best"`
	}
	if err := json.Unmarshal(unavailableModelsW.Body.Bytes(), &unavailableModels); err != nil {
		t.Fatal(err)
	}
	for _, edge := range unavailableModels["test-model"] {
		if edge.Provider == "mock" && (edge.Healthy || edge.Best) {
			t.Fatalf("provider without usable keys must be unhealthy and not best: %s", unavailableModelsW.Body.String())
		}
	}

	providersReq := httptest.NewRequest("GET", "/api/providers", nil)
	providersW := httptest.NewRecorder()
	h.ServeHTTP(providersW, providersReq)
	if providersW.Code != http.StatusOK {
		t.Fatalf("GET /api/providers status %d: %s", providersW.Code, providersW.Body.String())
	}
	var providers []struct {
		Name         string  `json:"name"`
		Status       string  `json:"status"`
		StatusReason *string `json:"status_reason"`
		ActiveKeys   int     `json:"active_keys"`
		ModelCount   int     `json:"model_count"`
	}
	if err := json.Unmarshal(providersW.Body.Bytes(), &providers); err != nil {
		t.Fatal(err)
	}
	foundDisabled := false
	foundUnavailable := false
	foundEmpty := false
	for _, provider := range providers {
		if provider.StatusReason != nil {
			t.Fatalf("anonymous provider response exposed unhealthy reason: %s", providersW.Body.String())
		}
		if provider.Name == "disabled" {
			foundDisabled = provider.Status == "disabled"
		}
		if provider.Name == "mock" && provider.ActiveKeys == 0 {
			foundUnavailable = provider.Status == "unhealthy"
		}
		if provider.Name == "empty" && provider.ModelCount == 0 {
			foundEmpty = provider.Status == "unhealthy"
		}
	}
	if !foundDisabled {
		t.Fatalf("disabled provider missing from /api/providers: %s", providersW.Body.String())
	}
	if !foundUnavailable {
		t.Fatalf("enabled provider without usable keys must expose its unhealthy reason: %s", providersW.Body.String())
	}
	if !foundEmpty {
		t.Fatalf("enabled provider without models must be unhealthy: %s", providersW.Body.String())
	}

	loginReq := httptest.NewRequest("POST", "/api/login", strings.NewReader(`{"admin_key":"x"}`))
	loginW := httptest.NewRecorder()
	h.ServeHTTP(loginW, loginReq)
	if loginW.Code != http.StatusOK || len(loginW.Result().Cookies()) != 1 {
		t.Fatalf("login failed: status %d, body %s", loginW.Code, loginW.Body.String())
	}
	authenticatedReq := httptest.NewRequest("GET", "/api/providers", nil)
	authenticatedReq.AddCookie(loginW.Result().Cookies()[0])
	authenticatedW := httptest.NewRecorder()
	h.ServeHTTP(authenticatedW, authenticatedReq)
	if authenticatedW.Code != http.StatusOK {
		t.Fatalf("authenticated GET /api/providers status %d: %s", authenticatedW.Code, authenticatedW.Body.String())
	}
	providers = nil
	if err := json.Unmarshal(authenticatedW.Body.Bytes(), &providers); err != nil {
		t.Fatal(err)
	}
	for _, provider := range providers {
		if provider.Name == "mock" && (provider.StatusReason == nil || *provider.StatusReason != "无可用 Key") {
			t.Fatalf("authenticated response missing key reason: %s", authenticatedW.Body.String())
		}
		if provider.Name == "empty" && (provider.StatusReason == nil || *provider.StatusReason != "无可用模型") {
			t.Fatalf("authenticated response missing model reason: %s", authenticatedW.Body.String())
		}
	}
}
