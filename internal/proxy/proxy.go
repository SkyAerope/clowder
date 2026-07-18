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

// handleModels 返回 canonical、配置别名、provider 原生模型名及 auto 的展平列表。
func (s *Server) handleModels(w http.ResponseWriter, r *http.Request) {
	type model struct {
		ID      string `json:"id"`
		Object  string `json:"object"`
		OwnedBy string `json:"owned_by"`
	}
	list := []model{{ID: "auto", Object: "model", OwnedBy: "clowder"}}
	for _, name := range s.App.ModelNames() {
		list = append(list, model{ID: name, Object: "model", OwnedBy: "clowder"})
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
		var found bool
		candidates, found = s.resolveCandidates(reqModel)
		if !found {
			writeJSONError(w, http.StatusNotFound, fmt.Sprintf("model %q not found", reqModel), "invalid_request_error")
			return
		}
	}
	if len(candidates) == 0 {
		writeJSONError(w, http.StatusServiceUnavailable, "no healthy provider for model "+reqModel, "service_unavailable")
		return
	}

	chain := &chainState{traceID: uuid.NewString()}
	s.runChain(w, r, chain, candidates, payload, clientStream, reqModel)
}

func (s *Server) resolveCandidates(reqModel string) ([]app.Candidate, bool) {
	mapping := s.App.MappingSnapshot()
	if mapping.IsCanonical(reqModel) {
		return s.App.CandidatesFor(reqModel), true
	}
	model, providerName, qualified := s.qualifiedModel(reqModel)
	if qualified {
		if mapping.IsCanonical(model) {
			return s.App.CandidatesForProvider(model, providerName)
		}
		return s.App.CandidatesForNativeProvider(model, providerName)
	}
	return s.App.CandidatesForNative(reqModel)
}

func (s *Server) qualifiedModel(reqModel string) (model, providerName string, ok bool) {
	for name := range s.App.Providers() {
		suffix := ":" + name
		if len(name) > len(providerName) && strings.HasSuffix(reqModel, suffix) {
			model = strings.TrimSuffix(reqModel, suffix)
			providerName = name
		}
	}
	return model, providerName, model != "" && providerName != ""
}

// runChain 重试链主循环。
func (s *Server) runChain(w http.ResponseWriter, r *http.Request, chain *chainState,
	candidates []app.Candidate, payload map[string]any, clientStream bool, reqModel string) {

	maxRetries := s.App.Cfg.Routing.MaxRetries
	auto := reqModel == "auto"
	attemptedProviders := map[string]bool{}
	provIdx := 0
	usedKeys := map[int64]bool{}
	var lastOutcome *attemptOutcome
	var lastCand app.Candidate

	for chain.attemptNo = 1; chain.attemptNo <= maxRetries; chain.attemptNo++ {
		if r.Context().Err() != nil {
			return // 客户端断开：停止重试
		}
		if provIdx >= len(candidates) {
			break // 候选耗尽
		}
		cand := candidates[provIdx]
		attemptedProviders[cand.Provider] = true
		key, err := s.App.Pool.Pick(cand.Provider, usedKeys)
		if err != nil {
			// 全 key 失败 / 无可用 key → 当前 native 不可路由，推进候选。
			s.App.Health.MarkModelDown(cand.Provider, cand.Native)
			s.App.Log.Warn("no usable key, marking model down", "provider", cand.Provider, "model", cand.Native)
			provIdx = nextCandidateIndex(candidates, provIdx, auto, attemptedProviders)
			usedKeys = map[int64]bool{}
			chain.provSwitches++
			chain.attemptNo-- // 没有真正发请求，不消耗 attempt
			continue
		}

		// hedge 双发：源/目标 provider 都声明 hedge 且配置了 TTFT 阈值 →
		// 先发源请求，TTFT 阈值内无首字节则并发拉起下一 provider，首字节先到者赢。
		if hedgeIdx, eligible := s.hedgeTarget(cand, candidates, provIdx); eligible {
			if done, lo, lc, advanced := s.runHedge(w, r, chain, candidates, provIdx, hedgeIdx, key, payload, clientStream, reqModel); advanced {
				if done {
					return
				}
				lastOutcome, lastCand = lo, lc
				provIdx = nextCandidateIndex(candidates, hedgeIdx, auto, attemptedProviders)
				usedKeys = map[int64]bool{}
				chain.provSwitches++
				continue
			}
		}

		la := s.dial(r.Context(), cand, key.APIKey, payload, clientStream, reqModel, s.willWaitOnTTFT(chain, cand))
		outcome := la.o
		if outcome.canceled {
			return // 客户端断开：静默终结，不记错不重试
		}
		if la.deliverable() {
			outcome = s.deliver(w, la, reqModel)
		}
		if outcome.canceled {
			return // 交付途中客户端断开
		}
		lastOutcome = outcome
		lastCand = cand

		matched := s.finalizeAttempt(chain, cand, key.ID, outcome)

		if outcome.delivered {
			if !outcome.event.IsError {
				s.App.Pool.OnSuccess(key.ID)
				s.App.Health.MarkSuccess(cand.Provider, cand.Native, outcome.ttftMs)
			}
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
			// 竞速双发：解析为多路并发分支，首字节先到者赢
			specs := s.buildRaceBranches(route.Race, cand, key, candidates, provIdx, usedKeys, auto)
			if len(specs) >= 2 {
				done, lf := s.runParallel(w, r.Context(), chain, specs, payload, clientStream, reqModel)
				if done {
					return
				}
				if lf != nil {
					lastOutcome, lastCand = lf.la.o, lf.spec.cand
				}
				// 全部失败：保守推进到下一个未试 provider（避免重试指数扩散）
				provIdx = nextCandidateIndex(candidates, provIdx, auto, attemptedProviders)
				usedKeys = map[int64]bool{}
				chain.provSwitches++
				continue
			}
			// 凑不齐 2 路 → 降级为第一路单发
			single = route.Race[0]
		}
		switch single {
		case rules.RouteNextKey:
			usedKeys[key.ID] = true
			chain.keySwitches++
		case rules.RouteNextProvider:
			provIdx = nextCandidateIndex(candidates, provIdx, auto, attemptedProviders)
			usedKeys = map[int64]bool{}
			chain.provSwitches++
		case rules.RouteRetrySame:
			// 原 key 重试（配合 backoff 类 do）
		case rules.RoutePassthrough:
			s.writeErrorToClient(w, outcome, cand)
			return
		case rules.RouteWait:
			// 继续等待上游：仅在 TTFT 超时场景有意义，且已由 willWaitOnTTFT 预判走「不取消」路径。
			// 能进到这里说明上游已真正结束（无可再等），降级为透传当前结果。
			s.writeErrorToClient(w, outcome, cand)
			return
		}
	}

	// 重试耗尽 / 候选耗尽
	if lastOutcome != nil {
		s.writeErrorToClient(w, lastOutcome, lastCand)
		return
	}
	writeJSONError(w, http.StatusServiceUnavailable, "all providers exhausted", "service_unavailable")
}

func nextCandidateIndex(candidates []app.Candidate, current int, auto bool, attempted map[string]bool) int {
	if auto {
		return current + 1
	}
	for i := current + 1; i < len(candidates); i++ {
		if !attempted[candidates[i].Provider] {
			return i
		}
	}
	return len(candidates)
}

// finalizeAttempt 对一次完成的 attempt 跑引擎匹配、写日志、执行 do。返回命中规则。
func (s *Server) finalizeAttempt(chain *chainState, cand app.Candidate, keyID int64, o *attemptOutcome) *rules.Rule {
	ev := o.event
	ev.Provider = cand.Provider
	ev.Model = cand.Canonical
	ev.AttemptNo = chain.attemptNo
	ev.KeySwitches = chain.keySwitches
	ev.ProviderSwitches = chain.provSwitches

	matched := s.App.Engine.Match(ev)
	matchedName := ""
	if matched != nil {
		matchedName = matched.Name
	}
	reqID := s.logAttempt(chain, cand, keyID, o, matchedName, ev)
	if matched != nil {
		s.App.RunActions(&app.ActionContext{
			Rule: matched, Event: ev, KeyID: keyID, RequestID: reqID,
			Provider: cand.Provider, Model: cand.Canonical, Native: cand.Native,
			ReqHeaders: o.reqHeaders, ReqBody: o.reqBody,
			RespHeaders: o.respHeaders, RespBody: string(o.respBody),
			StatusCode: ev.StatusCode, Fingerprint: ev.FingerprintCache,
		})
	}
	return matched
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
	la.aggregate = upstreamStreaming && !clientStream

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
			o.canceled = true // 客户端断开/竞速取消，非上游错误
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

	// 非 2xx：读错误体（限 256KB），不交付，进引擎
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
		// 非流式：dial 阶段就全量读取（读完即"可交付"）
		body, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
		resp.Body.Close()
		if err != nil {
			cancel()
			if ctx.Err() != nil {
				o.canceled = true // 客户端断开/竞速取消，非上游错误
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

	// SSE：读首字节（带 TTFT 计时；hedge 模式不自动取消慢路）
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
			o.event.StatusCode = 0 // 没有可透传的响应
		default:
			if ctx.Err() != nil {
				o.canceled = true // 客户端/竞速组断开：静默结束，不重试不记错
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

// deliver 把已 dial 成功的 attempt 交付客户端（写 w）。
func (s *Server) deliver(w http.ResponseWriter, la *liveAttempt, reqModel string) *attemptOutcome {
	o := la.o
	defer func() { o.latencyMs = time.Since(la.start).Milliseconds() }()

	// 非流式：dial 已读取全量 body，直接写出
	if la.hasWhole {
		body := la.whole
		if la.clientStream {
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

	defer la.resp.Body.Close()
	reader := la.reader
	cand := la.cand
	// clientGone：客户端已断开（区别于上游断流，不应标错误/重试）
	clientGone := func() bool { return la.clientCtx.Err() != nil }

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

	if la.aggregate {
		// 聚合模式：所有 chunk 读完前不写客户端 → 全程可重试
		agg := &aggregator{}
		chunk := la.first
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
				if clientGone() {
					o.canceled = true
					return o // 客户端断开
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
	chunk := la.first
	for {
		if chunk.data != nil {
			teeWrite(chunk.data)
		}
		if !writeChunk(chunk) {
			o.canceled = true
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
			if clientGone() {
				o.canceled = true
				return o // 客户端断开
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
			Provider: cand.Provider, Model: cand.Canonical, Native: cand.Native,
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
