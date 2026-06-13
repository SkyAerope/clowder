package proxy

import (
	"context"
	"net/http"
	"time"

	"github.com/SkyAerope/clowder/internal/app"
	"github.com/SkyAerope/clowder/internal/rules"
	"github.com/SkyAerope/clowder/internal/store"
)

// branchSpec 描述一路并发 attempt（竞速 race / 双发 hedge 共用）。
type branchSpec struct {
	cand   app.Candidate
	keyID  int64
	apiKey string
	delay  time.Duration // 拨号前延迟（hedge 的双发路 = TTFT 阈值）
	hedge  bool          // dial 不安装 TTFT 自动取消计时器
}

// branchResult 一路并发 attempt 的结果。
type branchResult struct {
	spec branchSpec
	la   *liveAttempt
}

// runParallel 并发拨号所有分支（按各自 delay），首字节先到者赢并交付，其余取消。
// 返回是否已交付；未交付时返回最后一路真实失败的结果供主线决定后续路由。
// 语义（DESIGN.md「race 双发」）：每路各算一次 attempt；某路失败照常进引擎跑 do，
// 但 route 挂起（不在此扩散重试），全部失败后由调用方按需继续。
func (s *Server) runParallel(w http.ResponseWriter, ctx context.Context, chain *chainState,
	specs []branchSpec, payload map[string]any, clientStream bool, reqModel string,
) (bool, *branchResult) {

	groupCtx, cancelGroup := context.WithCancel(ctx)
	defer cancelGroup()

	results := make(chan branchResult, len(specs))
	for _, sp := range specs {
		sp := sp
		go func() {
			if sp.delay > 0 {
				select {
				case <-time.After(sp.delay):
				case <-groupCtx.Done():
					results <- branchResult{spec: sp} // 赢家已出，延迟路未发起
					return
				}
			}
			la := s.dial(groupCtx, sp.cand, sp.apiKey, payload, clientStream, reqModel, sp.hedge)
			results <- branchResult{spec: sp, la: la}
		}()
	}

	var winner *branchResult
	var fails []branchResult
	for i := 0; i < len(specs); i++ {
		res := <-results
		if res.la == nil {
			continue // 延迟路被取消，未发起
		}
		if winner == nil && res.la.deliverable() {
			r := res
			winner = &r
			cancelGroup() // 首字节先到者赢，取消其余路
			continue
		}
		if winner != nil {
			res.la.discard()
			continue
		}
		fails = append(fails, res)
	}

	base := chain.attemptNo
	// 失败分支照常进引擎跑 do（route 挂起）；因赢家取消而中断的不算真实失败
	idx := 0
	for _, f := range fails {
		if f.la.o.canceled {
			continue // 客户端断开/赢家取消：不记错不进引擎
		}
		chain.attemptNo = base + idx
		s.finalizeAttempt(chain, f.spec.cand, f.spec.keyID, f.la.o)
		idx++
	}
	chain.attemptNo = base + len(specs) - 1 // 每路各算一次 attempt

	if winner != nil {
		o := s.deliver(w, winner.la, reqModel)
		if o.canceled {
			return true, nil // 交付途中客户端断开，静默终结
		}
		s.finalizeAttempt(chain, winner.spec.cand, winner.spec.keyID, o)
		if o.delivered && !o.event.IsError {
			s.App.Pool.OnSuccess(winner.spec.keyID)
			s.App.Health.MarkSuccess(winner.spec.cand.Provider, winner.spec.cand.Canonical, o.ttftMs)
		}
		return true, nil
	}

	if len(fails) > 0 {
		lf := fails[len(fails)-1]
		return false, &lf
	}
	return false, nil
}

// buildRaceBranches 把 race 路由动作解析为并发分支（上限 max_parallel_attempts）。
func (s *Server) buildRaceBranches(actions []string, cand app.Candidate, key *store.Key,
	candidates []app.Candidate, provIdx int, usedKeys map[int64]bool) []branchSpec {

	maxParallel := s.App.Cfg.Routing.MaxParallelAttempts
	localUsed := map[int64]bool{}
	for id := range usedKeys {
		localUsed[id] = true
	}
	nextProv := provIdx
	var specs []branchSpec
	for _, act := range actions {
		if len(specs) >= maxParallel {
			break // 超出上限部分顺序降级（不并发）
		}
		switch act {
		case rules.RouteRetrySame:
			specs = append(specs, branchSpec{cand: cand, keyID: key.ID, apiKey: key.APIKey})
		case rules.RouteNextKey:
			localUsed[key.ID] = true
			nk, err := s.App.Pool.Pick(cand.Provider, localUsed)
			if err != nil {
				continue
			}
			localUsed[nk.ID] = true
			specs = append(specs, branchSpec{cand: cand, keyID: nk.ID, apiKey: nk.APIKey})
		case rules.RouteNextProvider:
			nextProv++
			if nextProv >= len(candidates) {
				continue
			}
			nc := candidates[nextProv]
			nkey, err := s.App.Pool.Pick(nc.Provider, map[int64]bool{})
			if err != nil {
				continue
			}
			specs = append(specs, branchSpec{cand: nc, keyID: nkey.ID, apiKey: nkey.APIKey})
		}
	}
	return specs
}

// hedgeEligible 判断当前候选是否满足 hedge 双发前置条件：
// 配置了 TTFT 阈值，且源 provider 与下一候选 provider 都声明 hedge:true。
func (s *Server) hedgeEligible(cand app.Candidate, candidates []app.Candidate, provIdx int) bool {
	return s.App.Engine.MinTTFTThreshold() > 0 &&
		cand.Hedge && provIdx+1 < len(candidates) && candidates[provIdx+1].Hedge
}

// runHedge TTFT 超时双发：源请求保持不取消，TTFT 阈值后并发拉起下一 provider，首字节先到者赢。
// 返回 (是否已交付, 末路失败 outcome, 末路 cand, 是否真的发起了双发)。
func (s *Server) runHedge(w http.ResponseWriter, r *http.Request, chain *chainState,
	candidates []app.Candidate, provIdx int, key *store.Key, payload map[string]any,
	clientStream bool, reqModel string,
) (bool, *attemptOutcome, app.Candidate, bool) {

	cand := candidates[provIdx]
	nc := candidates[provIdx+1]
	nkey, err := s.App.Pool.Pick(nc.Provider, map[int64]bool{})
	if err != nil {
		return false, nil, app.Candidate{}, false // 无法发起双发，主线走常规单发
	}
	threshold := time.Duration(s.App.Engine.MinTTFTThreshold()) * time.Millisecond
	specs := []branchSpec{
		{cand: cand, keyID: key.ID, apiKey: key.APIKey, hedge: true},
		{cand: nc, keyID: nkey.ID, apiKey: nkey.APIKey, delay: threshold, hedge: true},
	}
	done, lf := s.runParallel(w, r.Context(), chain, specs, payload, clientStream, reqModel)
	if done {
		return true, nil, app.Candidate{}, true
	}
	if lf != nil {
		return false, lf.la.o, lf.spec.cand, true
	}
	return false, nil, app.Candidate{}, true
}
