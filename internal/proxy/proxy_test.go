package proxy_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/SkyAerope/clowder/internal/app"
	"github.com/SkyAerope/clowder/internal/config"
	"github.com/SkyAerope/clowder/internal/proxy"
	"github.com/SkyAerope/clowder/internal/server"
)

// setup 构建一个指向 mock 上游的完整 clowder 运行态。
func setup(t *testing.T, upstreamURL string, keys []string, extraRules string) (*app.App, http.Handler) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("CLOWDER_HOME", home)

	cfgYAML := fmt.Sprintf(`
bind_addr: "127.0.0.1"
port: 12100
api_key: "test-key"
providers_dir: "providers/"
routing: { max_retries: 10, max_parallel_attempts: 2, upstream_streaming: true }
backoff: { base_seconds: 1, max_seconds: 10 }
capture: { max_records: 100, response_buffer_kb: 64 }
pool: { min_active_keys: 1, max_total_keys: 5, auto_register: false, register_interval: 300 }
dashboard: { public: true, admin: false, admin_key: "x" }
`)
	os.WriteFile(filepath.Join(home, "config.yaml"), []byte(cfgYAML), 0o600)
	os.WriteFile(filepath.Join(home, "models.yaml"), []byte(`
test-model:
  - mock/test-model-v1
  - test-model-alias
`), 0o644)

	pdir := filepath.Join(home, "providers", "mock")
	os.MkdirAll(pdir, 0o755)
	manifest := fmt.Sprintf(`
name: mock
display_name: Mock
base_url: %s
enabled: true
key_strategy: sticky
models:
  - mock/test-model-v1
  - mock/unmapped-model
`, upstreamURL)
	os.WriteFile(filepath.Join(pdir, "manifest.yaml"), []byte(manifest), 0o644)

	if extraRules != "" {
		os.WriteFile(filepath.Join(home, "rules.yaml"), []byte(extraRules), 0o644)
	}

	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	a, err := app.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := a.ImportKeys("mock", keys); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(a.Close)

	mux := http.NewServeMux()
	proxy.New(a).Routes(mux)
	return a, mux
}

func chatReq(t *testing.T, h http.Handler, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-key")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

const sseBody = "data: {\"id\":\"c1\",\"model\":\"mock/test-model-v1\",\"choices\":[{\"delta\":{\"role\":\"assistant\",\"content\":\"hello\"},\"finish_reason\":null}]}\n\n" +
	"data: {\"id\":\"c1\",\"choices\":[{\"delta\":{\"content\":\" world\"},\"finish_reason\":\"stop\"}]}\n\n" +
	"data: [DONE]\n\n"

func sseUpstream(handler func(w http.ResponseWriter, r *http.Request) bool) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/models" {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"data":[{"id":"mock/test-model-v1"}]}`)
			return
		}
		if handler != nil && handler(w, r) {
			return // handler 已处理（错误注入）
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		io.WriteString(w, sseBody)
	}))
}

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
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Choices[0].Message.Content != "hello world" {
		t.Fatalf("aggregated content = %q", resp.Choices[0].Message.Content)
	}
	if resp.Model != "test-model" {
		t.Fatalf("model should be canonical name, got %q", resp.Model)
	}
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

func TestRetryNextKeyOn401(t *testing.T) {
	var calls int32
	up := sseUpstream(func(w http.ResponseWriter, r *http.Request) bool {
		auth := r.Header.Get("Authorization")
		atomic.AddInt32(&calls, 1)
		if auth == "Bearer bad-key" {
			w.WriteHeader(401)
			fmt.Fprint(w, `{"error":"invalid key"}`)
			return true
		}
		return false
	})
	defer up.Close()

	rulesYAML := `
rules:
  - name: kill-401
    when: { status_code: 401 }
    do: [ { mark_key: invalid } ]
    route: next_key
  - name: catch-all
    priority: 10000
    when: { error: true }
    route: passthrough
`
	a, h := setup(t, up.URL, []string{"bad-key", "good-key"}, rulesYAML)

	w := chatReq(t, h, `{"model":"test-model","messages":[]}`)
	if w.Code != 200 {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	if n := atomic.LoadInt32(&calls); n != 2 {
		t.Fatalf("upstream calls = %d, want 2 (bad then good)", n)
	}
	// 验证重试链已落库
	logs, err := a.Store.ListRequests(10, 0)
	if err != nil || len(logs) != 2 {
		t.Fatalf("want 2 request logs, got %d (%v)", len(logs), err)
	}
	// 倒序：最新的是成功 attempt 2
	if logs[0].AttemptNo != 2 || logs[1].AttemptNo != 1 {
		t.Fatalf("attempt numbers wrong: %d, %d", logs[0].AttemptNo, logs[1].AttemptNo)
	}
	if logs[1].MatchedRule != "kill-401" {
		t.Fatalf("attempt 1 matched rule = %q, want kill-401", logs[1].MatchedRule)
	}
	if logs[0].TraceID != logs[1].TraceID {
		t.Fatal("retry chain must share trace_id")
	}
}

func TestPassthrough4xx(t *testing.T) {
	up := sseUpstream(func(w http.ResponseWriter, r *http.Request) bool {
		w.WriteHeader(400)
		fmt.Fprint(w, `{"error":"bad request body"}`)
		return true
	})
	defer up.Close()
	_, h := setup(t, up.URL, []string{"k1"}, "") // 默认规则：4xx passthrough

	w := chatReq(t, h, `{"model":"test-model","messages":[]}`)
	if w.Code != 400 {
		t.Fatalf("status %d, want passthrough 400", w.Code)
	}
	if !strings.Contains(w.Body.String(), "bad request body") {
		t.Fatalf("body not passed through: %s", w.Body.String())
	}
}

// TestRaceFirstByteWins 竞速双发：500 触发 race[retry_same, next_key]，
// k1 恒 500、k2 成功 → 并发取胜者 k2 交付。
func TestRaceFirstByteWins(t *testing.T) {
	up := sseUpstream(func(w http.ResponseWriter, r *http.Request) bool {
		if r.Header.Get("Authorization") == "Bearer k1" {
			w.WriteHeader(500)
			fmt.Fprint(w, `{"error":"boom"}`)
			return true
		}
		return false // k2 走默认 SSE 成功
	})
	defer up.Close()

	rulesYAML := `
rules:
  - name: race-500
    when: { status_code: 500 }
    route: [retry_same, next_key]
  - name: catch-all
    priority: 10000
    when: { error: true }
    route: passthrough
`
	a, h := setup(t, up.URL, []string{"k1", "k2"}, rulesYAML)

	w := chatReq(t, h, `{"model":"test-model","messages":[]}`)
	if w.Code != 200 {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	// 竞速赢家会取消尚未完成的另一分支，因此日志可能是初始 attempt + 赢家
	// 两条，也可能在取消前完成两路而有三条；不把取消时序作为业务契约。
	logs, err := a.Store.ListRequests(10, 0)
	if err != nil || len(logs) < 2 {
		t.Fatalf("want at least initial and winning attempts, got %d (%v)", len(logs), err)
	}
	for _, l := range logs {
		if l.TraceID != logs[0].TraceID {
			t.Fatal("race attempts must share trace_id")
		}
	}
}

func TestModelNotFound(t *testing.T) {
	up := sseUpstream(nil)
	defer up.Close()
	_, h := setup(t, up.URL, []string{"k1"}, "")

	w := chatReq(t, h, `{"model":"nonexistent-model-xyz","messages":[]}`)
	if w.Code != 404 {
		t.Fatalf("status %d, want 404", w.Code)
	}
}

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
	if err := a.Reload(); err != nil {
		t.Fatal(err)
	}
	if a.ConfiguredProvider("disabled") == nil || a.Provider("disabled") != nil {
		t.Fatal("disabled provider must be configured but excluded from runtime routing")
	}
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
	if resp.Object != "list" || len(resp.Data) != 4 {
		t.Fatalf("unexpected models response: %+v", resp)
	}
	if got := resp.Data[0]; got.ID != "auto" || got.Object != "model" || got.OwnedBy != "clowder" {
		t.Fatalf("unexpected auto model: %+v", got)
	}
	wantIDs := []string{"auto", "mock/test-model-v1", "mock/unmapped-model", "test-model"}
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
	unmapped := models[""]
	if len(unmapped) != 1 || unmapped[0].Provider != "mock" || unmapped[0].Native != "mock/unmapped-model" {
		t.Fatalf("unexpected unmapped models: %s", apiW.Body.String())
	}
	if _, exists := models["disabled-model"]; exists {
		t.Fatal("disabled provider model must not be exposed")
	}
	if calls := atomic.LoadInt32(&upstreamModelCalls); calls != 0 {
		t.Fatalf("model list endpoints made %d upstream model requests", calls)
	}

	providersReq := httptest.NewRequest("GET", "/api/providers", nil)
	providersW := httptest.NewRecorder()
	h.ServeHTTP(providersW, providersReq)
	if providersW.Code != http.StatusOK {
		t.Fatalf("GET /api/providers status %d: %s", providersW.Code, providersW.Body.String())
	}
	var providers []struct {
		Name   string `json:"name"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal(providersW.Body.Bytes(), &providers); err != nil {
		t.Fatal(err)
	}
	foundDisabled := false
	for _, provider := range providers {
		if provider.Name == "disabled" {
			foundDisabled = provider.Status == "disabled"
		}
	}
	if !foundDisabled {
		t.Fatalf("disabled provider missing from /api/providers: %s", providersW.Body.String())
	}
}

func setupQualifiedProviders(t *testing.T, mockURL, otherURL string) (*app.App, http.Handler) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("CLOWDER_HOME", home)

	os.WriteFile(filepath.Join(home, "config.yaml"), []byte(`
bind_addr: "127.0.0.1"
port: 12100
api_key: "test-key"
providers_dir: "providers/"
routing: { max_retries: 3, max_parallel_attempts: 2, upstream_streaming: true }
backoff: { base_seconds: 1, max_seconds: 10 }
capture: { max_records: 100, response_buffer_kb: 64 }
pool: { min_active_keys: 1, max_total_keys: 5, auto_register: false, register_interval: 300 }
dashboard: { public: true, admin: false, admin_key: "x" }
`), 0o600)
	os.WriteFile(filepath.Join(home, "models.yaml"), []byte(`
shared-name:
  - mock/canonical-native
  - other/canonical-native
  - configured-only-name
secondary-name:
  - shared-name
`), 0o644)

	for _, p := range []struct {
		name, url, canonicalNative, directNative string
	}{
		{"mock", mockURL, "mock/canonical-native", "mock/direct-native"},
		{"other", otherURL, "other/canonical-native", "other/direct-native"},
	} {
		dir := filepath.Join(home, "providers", p.name)
		os.MkdirAll(dir, 0o755)
		manifest := fmt.Sprintf(`
name: %s
display_name: %s
base_url: %s
enabled: true
key_strategy: sticky
models:
  - %s
  - shared-name
  - %s
  - shared-name
  - common-native
`, p.name, p.name, p.url, p.canonicalNative, p.directNative)
		os.WriteFile(filepath.Join(dir, "manifest.yaml"), []byte(manifest), 0o644)
	}

	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	a, err := app.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	for _, providerName := range []string{"mock", "other"} {
		if _, _, err := a.ImportKeys(providerName, []string{providerName + "-key"}); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(a.Close)
	return a, server.New(a).Handler()
}

func modelUpstream(t *testing.T, calls *int32, models chan<- string, status int) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(calls, 1)
		var payload struct {
			Model string `json:"model"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
		}
		models <- payload.Model
		if status != http.StatusOK {
			w.WriteHeader(status)
			fmt.Fprint(w, `{"error":"upstream failure"}`)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, sseBody)
	}))
}

func TestProviderQualifiedModels(t *testing.T) {
	mockModels := make(chan string, 4)
	otherModels := make(chan string, 4)
	var mockCalls, otherCalls int32
	mock := modelUpstream(t, &mockCalls, mockModels, http.StatusOK)
	defer mock.Close()
	other := modelUpstream(t, &otherCalls, otherModels, http.StatusOK)
	defer other.Close()
	a, h := setupQualifiedProviders(t, mock.URL, other.URL)

	t.Run("canonical wins over same-named native", func(t *testing.T) {
		canonicalCandidates, found := a.CandidatesForProvider("shared-name", "mock")
		if !found || len(canonicalCandidates) != 1 || canonicalCandidates[0].Native != "mock/canonical-native" {
			t.Fatalf("canonical candidates = %v, found=%v", canonicalCandidates, found)
		}
		nativeCandidates, found := a.CandidatesForNativeProvider("shared-name", "mock")
		if !found || len(nativeCandidates) != 1 || nativeCandidates[0].Native != "shared-name" {
			t.Fatalf("native candidates = %v, found=%v", nativeCandidates, found)
		}
		w := chatReq(t, h, `{"model":"shared-name:mock","messages":[]}`)
		if w.Code != http.StatusOK {
			t.Fatalf("status %d: %s", w.Code, w.Body.String())
		}
		if got := <-mockModels; got != "mock/canonical-native" {
			t.Fatalf("upstream model = %q", got)
		}
	})

	t.Run("native selects provider exactly", func(t *testing.T) {
		w := chatReq(t, h, `{"model":"other/direct-native:other","messages":[]}`)
		if w.Code != http.StatusOK {
			t.Fatalf("status %d: %s", w.Code, w.Body.String())
		}
		if got := <-otherModels; got != "other/direct-native" {
			t.Fatalf("upstream model = %q", got)
		}
	})

	t.Run("mapped native remains exact", func(t *testing.T) {
		w := chatReq(t, h, `{"model":"other/canonical-native:other","messages":[]}`)
		if w.Code != http.StatusOK {
			t.Fatalf("status %d: %s", w.Code, w.Body.String())
		}
		if got := <-otherModels; got != "other/canonical-native" {
			t.Fatalf("upstream model = %q", got)
		}
		var resp struct {
			Model string `json:"model"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatal(err)
		}
		if resp.Model != "other/canonical-native:other" {
			t.Fatalf("response model = %q", resp.Model)
		}
	})

	if got := atomic.LoadInt32(&mockCalls); got != 1 {
		t.Fatalf("mock calls = %d", got)
	}
	if got := atomic.LoadInt32(&otherCalls); got != 2 {
		t.Fatalf("other calls = %d", got)
	}

	apiReq := httptest.NewRequest("GET", "/api/models", nil)
	apiW := httptest.NewRecorder()
	h.ServeHTTP(apiW, apiReq)
	if apiW.Code != http.StatusOK {
		t.Fatalf("GET /api/models status %d: %s", apiW.Code, apiW.Body.String())
	}
	var graph map[string][]struct {
		Provider string `json:"provider"`
		Native   string `json:"native"`
		Best     bool   `json:"best"`
	}
	if err := json.Unmarshal(apiW.Body.Bytes(), &graph); err != nil {
		t.Fatal(err)
	}
	if len(graph["shared-name"]) != 2 || len(graph["secondary-name"]) != 2 {
		t.Fatalf("unexpected graph: %s", apiW.Body.String())
	}
	for canonical, edges := range graph {
		if canonical == "" {
			continue
		}
		best := 0
		for _, edge := range edges {
			if edge.Best {
				best++
			}
		}
		if best != 1 {
			t.Fatalf("canonical %q has %d best edges", canonical, best)
		}
	}
}

func TestProviderQualifiedModelNotFound(t *testing.T) {
	mockModels := make(chan string, 4)
	otherModels := make(chan string, 4)
	var mockCalls, otherCalls int32
	mock := modelUpstream(t, &mockCalls, mockModels, http.StatusOK)
	defer mock.Close()
	other := modelUpstream(t, &otherCalls, otherModels, http.StatusOK)
	defer other.Close()
	_, h := setupQualifiedProviders(t, mock.URL, other.URL)

	for _, model := range []string{"shared-name:missing", "other/direct-native:mock", "configured-only-name"} {
		w := chatReq(t, h, fmt.Sprintf(`{"model":%q,"messages":[]}`, model))
		if w.Code != http.StatusNotFound {
			t.Fatalf("model %q status %d: %s", model, w.Code, w.Body.String())
		}
	}
	if got := atomic.LoadInt32(&mockCalls); got != 0 {
		t.Fatalf("mock calls = %d", got)
	}
	if got := atomic.LoadInt32(&otherCalls); got != 0 {
		t.Fatalf("other calls = %d", got)
	}
}

func TestExactNativeRoutesAcrossProviders(t *testing.T) {
	mockModels := make(chan string, 4)
	otherModels := make(chan string, 4)
	var mockCalls, otherCalls int32
	mock := modelUpstream(t, &mockCalls, mockModels, http.StatusOK)
	defer mock.Close()
	other := modelUpstream(t, &otherCalls, otherModels, http.StatusOK)
	defer other.Close()
	_, h := setupQualifiedProviders(t, mock.URL, other.URL)

	w := chatReq(t, h, `{"model":"common-native","messages":[]}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	if calls := atomic.LoadInt32(&mockCalls) + atomic.LoadInt32(&otherCalls); calls != 1 {
		t.Fatalf("upstream calls = %d", calls)
	}
	select {
	case got := <-mockModels:
		if got != "common-native" {
			t.Fatalf("upstream model = %q", got)
		}
	case got := <-otherModels:
		if got != "common-native" {
			t.Fatalf("upstream model = %q", got)
		}
	default:
		t.Fatal("no provider received native model")
	}
}

func TestProviderQualifiedModelDoesNotFailOver(t *testing.T) {
	mockModels := make(chan string, 4)
	otherModels := make(chan string, 4)
	var mockCalls, otherCalls int32
	mock := modelUpstream(t, &mockCalls, mockModels, http.StatusInternalServerError)
	defer mock.Close()
	other := modelUpstream(t, &otherCalls, otherModels, http.StatusOK)
	defer other.Close()
	_, h := setupQualifiedProviders(t, mock.URL, other.URL)

	w := chatReq(t, h, `{"model":"shared-name:mock","messages":[]}`)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	if got := atomic.LoadInt32(&mockCalls); got != 1 {
		t.Fatalf("mock calls = %d", got)
	}
	if got := atomic.LoadInt32(&otherCalls); got != 0 {
		t.Fatalf("other provider must not be called, got %d calls", got)
	}
}

// TestClientDisconnectDuringRead 客户端在非流式读取阶段断开（ctx canceled）：
// 上游回 200 后只发半个 body 并挂起，clowder 读取期间客户端断开 →
// 应静默终结，不记 transport 错误、不重试、不进引擎，requests 表无任何记录。
func TestClientDisconnectDuringRead(t *testing.T) {
	started := make(chan struct{})
	up := sseUpstream(func(w http.ResponseWriter, r *http.Request) bool {
		// 非 SSE 响应 → dial 走 io.ReadAll 全量读取路径
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		io.WriteString(w, `{"id":"c1"`) // 半个 body，永不补全
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		close(started)
		<-r.Context().Done() // 阻塞至 clowder 取消上游请求
		return true
	})
	defer up.Close()

	a, h := setup(t, up.URL, []string{"k1"}, "")

	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest("POST", "/v1/chat/completions",
		strings.NewReader(`{"model":"test-model","messages":[]}`)).WithContext(ctx)
	req.Header.Set("Authorization", "Bearer test-key")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		h.ServeHTTP(w, req)
		close(done)
	}()
	<-started
	cancel() // 模拟客户端断开
	<-done

	logs, err := a.Store.ListRequests(10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(logs) != 0 {
		t.Fatalf("client disconnect must not log any attempt, got %d (rule=%q, err=%q)",
			len(logs), logs[0].MatchedRule, logs[0].ErrorMsg)
	}
}
