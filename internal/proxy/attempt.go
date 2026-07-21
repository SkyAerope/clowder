package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/SkyAerope/clowder/internal/app"
	"github.com/SkyAerope/clowder/internal/rules"
)

// attemptOutcome 单次上游 attempt 的结果。
type attemptOutcome struct {
	delivered     bool // 已成功交付客户端
	firstByteSent bool // 首字节已写出（不可重试）
	event         *rules.Event
	ttftMs        int64
	latencyMs     int64
	respBody      []byte // 错误响应体（截断）
	respHeaders   string // JSON（capture 用）
	respHeaderMap http.Header
	reqHeaders    string
	reqBody       string
	transportErr  error
	canceled      bool // 客户端断开/竞速取消：静默终结，不记错不重试不计成功
}

func (o *attemptOutcome) errorMsg() string {
	if o.transportErr != nil {
		return o.transportErr.Error()
	}
	if len(o.respBody) > 0 {
		s := string(o.respBody)
		if len(s) > 500 {
			s = s[:500]
		}
		return s
	}
	if o.event.TTFTTimeout {
		return fmt.Sprintf("ttft timeout after %dms", o.event.TTFTMs)
	}
	return ""
}

// liveAttempt 一次"已拨号"的 attempt：dial 阶段推进到"可交付首字节"，deliver 阶段写客户端。
type liveAttempt struct {
	o            *attemptOutcome
	cand         app.Candidate
	resp         *http.Response
	reader       *sseReader
	first        *sseChunk
	whole        []byte // 非 SSE：全量响应体
	hasWhole     bool
	clientStream bool
	aggregate    bool
	start        time.Time
	cancel       context.CancelFunc
	clientCtx    context.Context
}

// deliverable 报告 dial 是否拿到可交付的 2xx 响应（首字节已就绪）。
func (la *liveAttempt) deliverable() bool {
	return la != nil && la.o != nil && !la.o.event.IsError && !la.o.delivered && !la.o.canceled
}

// discard 取消并释放该 attempt 的上游连接（竞速败者 / 提前放弃）。
func (la *liveAttempt) discard() {
	if la == nil {
		return
	}
	if la.cancel != nil {
		la.cancel()
	}
	if la.resp != nil {
		la.resp.Body.Close()
	}
}

// dial 发起上游请求并推进到"可交付首字节"或失败；不写客户端。
// hedge=true 时不安装 TTFT 取消计时器（双发场景由协调器决定何时取消慢路）。
// willWaitOnTTFT 预判：若本次 attempt 发生 TTFT 超时，是否会命中一条 route=wait 的规则。
// 命中则 dial 不安装 TTFT 自动取消计时器，继续等待上游首字节（「继续等待」语义），
// 复用 hedge 的「慢路不自动取消」机制。Match 为纯查询无副作用。
func (s *Server) willWaitOnTTFT(chain *chainState, cand app.Candidate) bool {
	threshold := s.App.Engine.MinTTFTThreshold()
	if threshold <= 0 {
		return false
	}
	ev := &rules.Event{
		Type:             "upstream",
		Provider:         cand.Provider,
		Model:            cand.Canonical,
		TTFTTimeout:      true,
		TTFTMs:           threshold,
		IsError:          true,
		AttemptNo:        chain.attemptNo,
		KeySwitches:      chain.keySwitches,
		ProviderSwitches: chain.provSwitches,
	}
	m := s.App.Engine.Match(ev)
	return m != nil && m.Route.Single == rules.RouteWait
}

func (s *Server) dial(ctx context.Context, cand app.Candidate, apiKey string,
	payload map[string]any, clientStream bool, reqModel string, hedge bool) *liveAttempt {

	o := &attemptOutcome{event: &rules.Event{Type: "upstream"}}
	start := time.Now()
	la := &liveAttempt{o: o, cand: cand, start: start, clientStream: clientStream, clientCtx: ctx}
	fail := func() *liveAttempt { o.latencyMs = time.Since(start).Milliseconds(); return la }

	upstreamStreaming := clientStream
	ps := s.App.Provider(cand.Provider)
	cfgStreaming := s.App.Cfg.Routing.UpstreamStreaming
	if ps != nil && ps.Manifest.UpstreamStreaming != nil {
		cfgStreaming = *ps.Manifest.UpstreamStreaming
	}
	if !clientStream && cfgStreaming {
		upstreamStreaming = true
	}
	la.aggregate = upstreamStreaming && !clientStream

	upPayload := make(map[string]any, len(payload))
	for k, v := range payload {
		upPayload[k] = v
	}
	upPayload["model"] = cand.Native
	upPayload["stream"] = upstreamStreaming
	upBody, err := json.Marshal(upPayload)
	if err != nil {
		o.event.Transport = rules.TransportMalformed
		o.event.IsError = true
		o.transportErr = err
		return fail()
	}
	if ps == nil {
		o.event.Transport = rules.TransportMalformed
		o.event.IsError = true
		o.transportErr = fmt.Errorf("provider %s not loaded", cand.Provider)
		return fail()
	}

	attemptCtx, cancel := context.WithCancel(ctx)
	la.cancel = cancel
	url := app.JoinURL(ps.Manifest.BaseURL, "/v1/chat/completions")
	req, err := http.NewRequestWithContext(attemptCtx, http.MethodPost, url, bytes.NewReader(upBody))
	if err != nil {
		o.event.Transport = rules.TransportMalformed
		o.event.IsError = true
		o.transportErr = err
		cancel()
		return fail()
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)
	o.reqHeaders = headersJSON(req.Header)
	o.reqBody = string(upBody)

	resp, err := upstreamClient.Do(req)
	if err != nil {
		cancel()
		if ctx.Err() != nil {
			o.canceled = true
			return fail()
		}
		o.event.Transport = classifyTransport(err)
		o.event.IsError = true
		o.transportErr = err
		return fail()
	}
	la.resp = resp
	o.respHeaderMap = resp.Header
	o.respHeaders = headersJSON(resp.Header)
	o.event.StatusCode = resp.StatusCode

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 256<<10))
		o.respBody = body
		o.event.Body = string(body)
		o.event.IsError = true
		resp.Body.Close()
		cancel()
		return fail()
	}

	isSSE := strings.Contains(resp.Header.Get("Content-Type"), "text/event-stream")
	if !upstreamStreaming || !isSSE {
		body, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
		resp.Body.Close()
		if err != nil {
			cancel()
			if ctx.Err() != nil {
				o.canceled = true
				return fail()
			}
			o.event.Transport = rules.TransportStreamCut
			o.event.IsError = true
			o.transportErr = err
			return fail()
		}
		o.ttftMs = time.Since(start).Milliseconds()
		o.event.TTFTMs = o.ttftMs
		s.feedSuccessBody(o, string(body))
		la.whole = body
		la.hasWhole = true
		return la
	}

	reader := newSSEReader(resp.Body)
	la.reader = reader
	threshold := s.App.Engine.MinTTFTThreshold()
	var ttftTimer *time.Timer
	timedOut := make(chan struct{})
	if threshold > 0 && !hedge {
		ttftTimer = time.AfterFunc(time.Duration(threshold)*time.Millisecond, func() {
			close(timedOut)
			cancel()
		})
	}
	first, err := reader.next()
	if ttftTimer != nil {
		ttftTimer.Stop()
	}
	elapsed := time.Since(start).Milliseconds()
	if err != nil {
		select {
		case <-timedOut:
			o.event.TTFTTimeout = true
			o.event.TTFTMs = elapsed
			o.event.IsError = true
			o.event.StatusCode = 0
		default:
			if ctx.Err() != nil {
				o.canceled = true
			} else {
				o.event.Transport = rules.TransportStreamCut
				o.event.IsError = true
				o.transportErr = err
				o.event.StatusCode = 0
			}
		}
		resp.Body.Close()
		cancel()
		return fail()
	}
	o.ttftMs = elapsed
	o.event.TTFTMs = elapsed
	la.first = first
	return la
}
