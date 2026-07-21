package proxy_test

import (
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
