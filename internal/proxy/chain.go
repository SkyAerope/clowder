package proxy

import (
	"net/http"

	"github.com/SkyAerope/clowder/internal/app"
	"github.com/SkyAerope/clowder/internal/rules"
)

// chainState 一条重试链的上下文。
type chainState struct {
	traceID      string
	attemptNo    int
	keySwitches  int
	provSwitches int
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
			s.App.Health.MarkModelDown(cand.Provider, cand.Native, "no-usable-key")
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

		route := rules.RouteSpec{Single: rules.RoutePassthrough}
		if matched != nil && !matched.Route.IsEmpty() {
			route = matched.Route
		}
		single := route.Single
		if route.IsRace() {
			specs := s.buildRaceBranches(route.Race, cand, key, candidates, provIdx, usedKeys, auto)
			if len(specs) >= 2 {
				done, lf := s.runParallel(w, r.Context(), chain, specs, payload, clientStream, reqModel)
				if done {
					return
				}
				if lf != nil {
					lastOutcome, lastCand = lf.la.o, lf.spec.cand
				}
				provIdx = nextCandidateIndex(candidates, provIdx, auto, attemptedProviders)
				usedKeys = map[int64]bool{}
				chain.provSwitches++
				continue
			}
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
