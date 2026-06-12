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
	"github.com/SkyAerope/clowder/internal/store"
	"github.com/google/uuid"
)

// Server 是代理 HTTP 层。
type Server struct {
	App *app.App
}

// New 创建代理服务。
func New(a *app.App) *Server { return &Server{App: a} }

// Routes 注册 /v1/* 端点。
func (s *Server) Routes(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/chat/completions", s.auth(s.handleChat))
	mux.HandleFunc("GET /v1/models", s.auth(s.handleModels))
}

// auth /v1/* 始终需要 api_key 认证。
func (s *Server) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		key := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if key == "" || key != s.App.Cfg.APIKey {
			writeJSONError(w, http.StatusUnauthorized, "invalid api key", "authentication_error")
			return
		}
		next(w, r)
	}
}

// handleModels 返回 canonical 聚合列表 + auto。
func (s *Server) handleModels(w http.ResponseWriter, r *http.Request) {
	type model struct {
		ID      string `json:"id"`
		Object  string `json:"object"`
		OwnedBy string `json:"owned_by"`
	}
	list := []model{{ID: "auto", Object: "model", OwnedBy: "clowder"}}
	for _, c := range s.App.CanonicalModels() {
		list = append(list, model{ID: c, Object: "model", OwnedBy: "clowder"})
	}
	writeJSON(w, http.StatusOK, map[string]any{"object": "list", "data": list})
}

// chainState 一条重试链的上下文。
type chainState struct {
	traceID      string
	attemptNo    int
	keySwitches  int
	provSwitches int
}

// handleChat 核心代理入口。
func (s *Server) handleChat(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 32<<20))
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "read request body: "+err.Error(), "invalid_request_error")
		return
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid JSON body", "invalid_request_error")
		return
	}
	reqModel, _ := payload["model"].(string)
	if reqModel == "" {
		writeJSONError(w, http.StatusBadRequest, "missing model", "invalid_request_error")
		return
	}
	clientStream, _ := payload["stream"].(bool)

	// 解析 canonical & 候选
	var candidates []app.Candidate
	if reqModel == "auto" {
		candidates = s.App.CandidatesAuto()
	} else {
		canonical, ok, suggestion := s.App.Mapping.ResolveRequest(reqModel)
		if !ok {
			msg := fmt.Sprintf("model %q not found", reqModel)
			if suggestion != "" {
				msg += fmt.Sprintf(" (did you mean %q?)", suggestion)
			}
			writeJSONError(w, http.StatusNotFound, msg, "invalid_request_error")
			return
		}
		candidates = s.App.CandidatesFor(canonical)
	}
	if len(candidates) == 0 {
		writeJSONError(w, http.StatusServiceUnavailable, "no healthy provider for model "+reqModel, "service_unavailable")
		return
	}

	chain := &chainState{traceID: uuid.NewString()}
	s.runChain(w, r, chain, candidates, payload, clientStream, reqModel)
}

// runChain 重试链主循环。
func (s *Server) runChain(w http.ResponseWriter, r *http.Request, chain *chainState,
	candidates []app.Candidate, payload map[string]any, clientStream bool, reqModel string) {

	maxRetries := s.App.Cfg.Routing.MaxRetries
	provIdx := 0
	usedKeys := map[int64]bool{}
	var lastOutcome *attemptOutcome

	for chain.attemptNo = 1; chain.attemptNo <= maxRetries; chain.attemptNo++ {
		if provIdx >= len(candidates) {
			break // 候选耗尽
		}
		cand := candidates[provIdx]
		key, err := s.App.Pool.Pick(cand.Provider, usedKeys)
		if err != nil {
			// 全 key 失败 / 无可用 key → model 级 unhealthy，换 provider
			s.App.Health.MarkModelDown(cand.Provider, cand.Canonical)
			s.App.Log.Warn("no usable key, marking model down", "provider", cand.Provider, "model", cand.Canonical)
			provIdx++
			usedKeys = map[int64]bool{}
			chain.provSwitches++
			chain.attemptNo-- // 没有真正发请求，不消耗 attempt
			continue
		}

		outcome := s.doAttempt(r.Context(), w, cand, key.APIKey, payload, clientStream, reqModel)
		lastOutcome = outcome

		// 构建事件
		ev := outcome.event
		ev.Provider = cand.Provider
		ev.Model = cand.Canonical
		ev.AttemptNo = chain.attemptNo
		ev.KeySwitches = chain.keySwitches
		ev.ProviderSwitches = chain.provSwitches

		// 引擎匹配（成功响应也进引擎；route 仅错误时生效）
		matched := s.App.Engine.Match(ev)
		matchedName := ""
		if matched != nil {
			matchedName = matched.Name
		}

		// 写请求日志
		reqID := s.logAttempt(chain, cand, key.ID, outcome, matchedName, ev)

		// 执行 do（异步，不阻塞主线）
		if matched != nil {
			s.App.RunActions(&app.ActionContext{
				Rule: matched, Event: ev, KeyID: key.ID, RequestID: reqID,
				Provider: cand.Provider, Model: cand.Canonical,
				ReqHeaders: outcome.reqHeaders, ReqBody: outcome.reqBody,
				RespHeaders: outcome.respHeaders, RespBody: string(outcome.respBody),
				StatusCode: ev.StatusCode, Fingerprint: ev.FingerprintCache,
			})
		}

		if outcome.delivered {
			s.App.Pool.OnSuccess(key.ID)
			s.App.Health.MarkSuccess(cand.Provider, cand.Canonical, outcome.ttftMs)
			return
		}
		if outcome.firstByteSent {
			return // 首字节已写出，断流只能到此为止（error event 已发）
		}

		// 错误：按 route 决策
		route := rules.RouteSpec{Single: rules.RoutePassthrough}
		if matched != nil && !matched.Route.IsEmpty() {
			route = matched.Route
		}
		single := route.Single
		if route.IsRace() {
			// race 暂顺序降级：取第一路执行（真双发后续补）
			single = route.Race[0]
		}
		switch single {
		case rules.RouteNextKey:
			usedKeys[key.ID] = true
			chain.keySwitches++
		case rules.RouteNextProvider:
			provIdx++
			usedKeys = map[int64]bool{}
			chain.provSwitches++
		case rules.RouteRetrySame:
			// 原 key 重试（配合 backoff 类 do）
		case rules.RoutePassthrough:
			s.writeErrorToClient(w, outcome, cand)
			return
		}
	}

	// 重试耗尽 / 候选耗尽
	if lastOutcome != nil {
		s.writeErrorToClient(w, lastOutcome, app.Candidate{})
		return
	}
	writeJSONError(w, http.StatusServiceUnavailable, "all providers exhausted", "service_unavailable")
}

// logAttempt 写 requests 表。
func (s *Server) logAttempt(chain *chainState, cand app.Candidate, keyID int64,
	o *attemptOutcome, matchedRule string, ev *rules.Event) int64 {
	rl := &store.RequestLog{
		TraceID:     chain.traceID,
		AttemptNo:   chain.attemptNo,
		KeyID:       &keyID,
		Provider:    cand.Provider,
		Model:       cand.Canonical,
		MatchedRule: matchedRule,
	}
	if ev.StatusCode != 0 {
		rl.StatusCode = &ev.StatusCode
	}
	if o.ttftMs > 0 {
		rl.TTFTMs = &o.ttftMs
	}
	if o.latencyMs > 0 {
		rl.LatencyMs = &o.latencyMs
	}
	if ev.IsError {
		rl.ErrorMsg = o.errorMsg()
		ev.FingerprintCache = rules.FingerprintOf(ev)
		rl.ErrorFingerprint = ev.FingerprintCache
	}
	id, err := s.App.Store.InsertRequest(rl)
	if err != nil {
		s.App.Log.Warn("log attempt", "error", err)
	}
	return id
}

// writeErrorToClient passthrough：透传上游错误；超时类无响应可透 → 合成 504。
func (s *Server) writeErrorToClient(w http.ResponseWriter, o *attemptOutcome, cand app.Candidate) {
	if o.event.TTFTTimeout || o.event.Transport == rules.TransportConnectTimeout {
		writeJSONError(w, http.StatusGatewayTimeout,
			fmt.Sprintf("upstream timeout (provider=%s, model=%s)", o.event.Provider, o.event.Model),
			"timeout")
		return
	}
	if o.event.StatusCode != 0 {
		// 透传上游错误响应
		for k, vs := range o.respHeaderMap {
			if skipHopHeader(k) {
				continue
			}
			for _, v := range vs {
				w.Header().Add(k, v)
			}
		}
		w.Header().Set("Content-Length", fmt.Sprint(len(o.respBody)))
		w.WriteHeader(o.event.StatusCode)
		w.Write(o.respBody)
		return
	}
	writeJSONError(w, http.StatusBadGateway,
		fmt.Sprintf("upstream transport error: %s (provider=%s)", o.event.Transport, o.event.Provider),
		"upstream_error")
}

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

// doAttempt 发起一次上游请求并处理响应/交付。
func (s *Server) doAttempt(ctx context.Context, w http.ResponseWriter, cand app.Candidate,
	apiKey string, payload map[string]any, clientStream bool, reqModel string) *attemptOutcome {

	o := &attemptOutcome{event: &rules.Event{Type: "upstream"}}
	start := time.Now()
	defer func() { o.latencyMs = time.Since(start).Milliseconds() }()

	// 决定上游是否流式：客户端流式，或 upstream_streaming 开（聚合模式采集 TTFT）
	upstreamStreaming := clientStream
	ps := s.App.Provider(cand.Provider)
	cfgStreaming := s.App.Cfg.Routing.UpstreamStreaming
	if ps != nil && ps.Manifest.UpstreamStreaming != nil {
		cfgStreaming = *ps.Manifest.UpstreamStreaming
	}
	if !clientStream && cfgStreaming {
		upstreamStreaming = true
	}
	aggregate := upstreamStreaming && !clientStream

	// 构造上游请求体（替换 model 为 native 名）
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
		return o
	}

	if ps == nil {
		o.event.Transport = rules.TransportMalformed
		o.event.IsError = true
		o.transportErr = fmt.Errorf("provider %s not loaded", cand.Provider)
		return o
	}
	attemptCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	url := app.JoinURL(ps.Manifest.BaseURL, "/v1/chat/completions")
	req, err := http.NewRequestWithContext(attemptCtx, http.MethodPost, url, bytes.NewReader(upBody))
	if err != nil {
		o.event.Transport = rules.TransportMalformed
		o.event.IsError = true
		o.transportErr = err
		return o
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)
	o.reqHeaders = headersJSON(req.Header)
	o.reqBody = string(upBody)

	resp, err := upstreamClient.Do(req)
	if err != nil {
		o.event.Transport = classifyTransport(err)
		o.event.IsError = true
		o.transportErr = err
		return o
	}
	defer resp.Body.Close()
	o.respHeaderMap = resp.Header
	o.respHeaders = headersJSON(resp.Header)
	o.event.StatusCode = resp.StatusCode

	// 非 2xx：读错误体（限 256KB），不交付，进引擎
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 256<<10))
		o.respBody = body
		o.event.Body = string(body)
		o.event.IsError = true
		return o
	}

	// 2xx：按上游响应形态交付
	isSSE := strings.Contains(resp.Header.Get("Content-Type"), "text/event-stream")
	if !upstreamStreaming || !isSSE {
		return s.deliverWhole(w, resp, o, clientStream, reqModel, start)
	}
	return s.deliverStream(w, resp, o, cancel, aggregate, reqModel, start, cand)
}

// deliverWhole 上游非流式（或不支持 SSE）：全量读取后交付。
func (s *Server) deliverWhole(w http.ResponseWriter, resp *http.Response, o *attemptOutcome,
	clientStream bool, reqModel string, start time.Time) *attemptOutcome {
	body, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		o.event.Transport = rules.TransportStreamCut
		o.event.IsError = true
		o.transportErr = err
		return o // 还没写客户端，可重试
	}
	o.ttftMs = time.Since(start).Milliseconds()
	o.event.TTFTMs = o.ttftMs
	s.feedSuccessBody(o, string(body))
	if clientStream {
		// 客户端要流式但上游给了整包：包装成单 chunk SSE
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(http.StatusOK)
		o.firstByteSent = true
		fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", body)
	} else {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write(rewriteModelField(body, reqModel))
	}
	o.delivered = true
	return o
}

// deliverStream 上游 SSE：等首字节（带 TTFT 计时）→ 透传或聚合。
func (s *Server) deliverStream(w http.ResponseWriter, resp *http.Response, o *attemptOutcome,
	cancel context.CancelFunc, aggregate bool, reqModel string, start time.Time, cand app.Candidate) *attemptOutcome {

	reader := newSSEReader(resp.Body)

	// TTFT 计时器：到 engine 最小阈值时取消上游请求
	threshold := s.App.Engine.MinTTFTThreshold()
	var ttftTimer *time.Timer
	timedOut := make(chan struct{})
	if threshold > 0 {
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
			o.event.StatusCode = 0 // 没有可透传的响应
		default:
			o.event.Transport = rules.TransportStreamCut
			o.event.IsError = true
			o.transportErr = err
			o.event.StatusCode = 0
		}
		return o // 首字节前失败：可重试
	}
	o.ttftMs = elapsed
	o.event.TTFTMs = elapsed

	// tee 旁路：仅当存在可能命中成功响应 body 的规则
	var tee *bytes.Buffer
	teeLimit := s.App.Cfg.Capture.ResponseBufferKB << 10
	if s.App.Engine.NeedsSuccessBodyTee() {
		tee = &bytes.Buffer{}
	}
	teeWrite := func(b []byte) {
		if tee != nil && tee.Len() < teeLimit {
			n := teeLimit - tee.Len()
			if n > len(b) {
				n = len(b)
			}
			tee.Write(b[:n])
		}
	}

	if aggregate {
		// 聚合模式：所有 chunk 读完前不写客户端 → 全程可重试
		agg := &aggregator{}
		chunk := first
		for {
			if chunk.done {
				break
			}
			if chunk.data != nil {
				agg.feed(chunk.data)
				teeWrite(chunk.data)
			}
			var err error
			chunk, err = reader.next()
			if err != nil {
				if err == io.EOF {
					break // 无 [DONE] 直接 EOF 也算正常结束
				}
				o.event.Transport = rules.TransportStreamCut
				o.event.IsError = true
				o.transportErr = err
				return o // 未写客户端，可重试
			}
		}
		result := agg.result(reqModel)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write(result)
		o.delivered = true
		o.firstByteSent = true
		if tee != nil {
			s.feedSuccessBodyAsync(o.event, cand, tee.String())
		}
		return o
	}

	// 透传模式：写出首字节后进入不可重试区
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher, _ := w.(http.Flusher)
	// 重写 chunk 中的上游原生 model 名为 canonical 名，避免泄漏上游信息
	rewriteModel := newModelRewriter(cand.Native, reqModel)
	writeChunk := func(c *sseChunk) bool {
		rewriteModel(c)
		if _, err := w.Write(c.raw); err != nil {
			return false // 客户端断开
		}
		if flusher != nil {
			flusher.Flush()
		}
		return true
	}
	o.firstByteSent = true
	chunk := first
	for {
		if chunk.data != nil {
			teeWrite(chunk.data)
		}
		if !writeChunk(chunk) {
			return o // 客户端断开，结束（非上游错误）
		}
		if chunk.done {
			o.delivered = true
			break
		}
		var err error
		chunk, err = reader.next()
		if err != nil {
			if err == io.EOF {
				o.delivered = true
				break
			}
			// 首字节已发出，上游断流：发 error event 后断开，不重试
			w.Write(sseErrorEvent(fmt.Sprintf("upstream stream cut (provider=%s): %v", cand.Provider, err)))
			if flusher != nil {
				flusher.Flush()
			}
			o.event.Transport = rules.TransportStreamCut
			o.event.IsError = true
			o.transportErr = err
			return o
		}
	}
	if tee != nil {
		s.feedSuccessBodyAsync(o.event, cand, tee.String())
	}
	return o
}

// feedSuccessBody 同步设置成功事件 body（deliverWhole 已有全量 body 时）。
func (s *Server) feedSuccessBody(o *attemptOutcome, body string) {
	if s.App.Engine.NeedsSuccessBodyTee() {
		limit := s.App.Cfg.Capture.ResponseBufferKB << 10
		if len(body) > limit {
			body = body[:limit]
		}
		o.event.Body = body
	}
}

// feedSuccessBodyAsync 流结束后异步把成功响应 body 喂给引擎（route 忽略，只执行 do）。
func (s *Server) feedSuccessBodyAsync(base *rules.Event, cand app.Candidate, body string) {
	ev := *base
	ev.Body = body
	ev.IsError = false
	go func() {
		r := s.App.Engine.Match(&ev)
		if r == nil {
			return
		}
		s.App.RunActions(&app.ActionContext{
			Rule: r, Event: &ev,
			Provider: cand.Provider, Model: cand.Canonical,
			RespBody: body, StatusCode: ev.StatusCode,
		})
	}()
}

// rewriteModelField 把上游响应里的 model 字段改回客户端请求名（不暴露 native 名）。
func rewriteModelField(body []byte, reqModel string) []byte {
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		return body
	}
	m["model"] = reqModel
	out, err := json.Marshal(m)
	if err != nil {
		return body
	}
	return out
}

func headersJSON(h http.Header) string {
	m := make(map[string]string, len(h))
	for k, vs := range h {
		m[k] = strings.Join(vs, ", ")
	}
	b, _ := json.Marshal(m)
	return string(b)
}

func skipHopHeader(k string) bool {
	switch strings.ToLower(k) {
	case "connection", "keep-alive", "transfer-encoding", "content-length", "te", "trailer", "upgrade":
		return true
	}
	return false
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

func writeJSONError(w http.ResponseWriter, code int, message, typ string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	w.Write(errorJSON(message, typ))
}
