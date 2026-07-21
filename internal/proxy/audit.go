package proxy

import (
	"github.com/SkyAerope/clowder/internal/app"
	"github.com/SkyAerope/clowder/internal/rules"
	"github.com/SkyAerope/clowder/internal/store"
)

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
