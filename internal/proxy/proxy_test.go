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
`), 0o644)

	pdir := filepath.Join(home, "providers", "mock")
	os.MkdirAll(pdir, 0o755)
	manifest := fmt.Sprintf(`
name: mock
display_name: Mock
base_url: %s
enabled: true
key_source: static
key_strategy: sequential
model_whitelist:
  - mock/test-model-v1
`, upstreamURL)
	os.WriteFile(filepath.Join(pdir, "manifest.yaml"), []byte(manifest), 0o644)
	os.WriteFile(filepath.Join(pdir, "keys.txt"), []byte(strings.Join(keys, "\n")), 0o600)

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
	// 触发 attempt(1) + race 两路 = 至少 3 条 attempt 落库
	logs, err := a.Store.ListRequests(10, 0)
	if err != nil || len(logs) < 3 {
		t.Fatalf("want >=3 request logs, got %d (%v)", len(logs), err)
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
	up := sseUpstream(nil)
	defer up.Close()
	a, h := setup(t, up.URL, []string{"k1"}, "")
	a.RefreshAllModels(t.Context())

	req := httptest.NewRequest("GET", "/v1/models", nil)
	req.Header.Set("Authorization", "Bearer test-key")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	body := w.Body.String()
	if !strings.Contains(body, `"auto"`) || !strings.Contains(body, `"test-model"`) {
		t.Fatalf("models list: %s", body)
	}
	if strings.Contains(body, "mock/test-model-v1") {
		t.Fatal("must not expose provider native model names")
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

