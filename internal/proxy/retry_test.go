package proxy_test

import (
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/SkyAerope/clowder/internal/store"
)

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
  - kill-401:
      when: { status_code: 401 }
      do: [ { mark_key: invalid } ]
      route: next_key
  - catch-all:
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
	logs, err := a.Store.ListRequests(store.RequestFilters{}, 10, 0)
	if err != nil || len(logs) != 2 {
		t.Fatalf("want 2 request logs, got %d (%v)", len(logs), err)
	}
	// 倒序：最新的是成功 attempt 2
	if logs[0].AttemptNo != 2 || logs[1].AttemptNo != 1 {
		t.Fatalf("attempt numbers wrong: %d, %d", logs[0].AttemptNo, logs[1].AttemptNo)
	}
	if logs[1].MatchedSlug != "kill-401" {
		t.Fatalf("attempt 1 matched slug = %q, want kill-401", logs[1].MatchedSlug)
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
  - race-500:
      when: { status_code: 500 }
      route: [retry_same, next_key]
  - catch-all:
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
	logs, err := a.Store.ListRequests(store.RequestFilters{}, 10, 0)
	if err != nil || len(logs) < 2 {
		t.Fatalf("want at least initial and winning attempts, got %d (%v)", len(logs), err)
	}
	for _, l := range logs {
		if l.TraceID != logs[0].TraceID {
			t.Fatal("race attempts must share trace_id")
		}
	}
}
