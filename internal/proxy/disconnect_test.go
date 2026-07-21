package proxy_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

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
			len(logs), logs[0].MatchedSlug, logs[0].ErrorMsg)
	}
}
