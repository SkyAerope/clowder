package rules

import (
	"sort"
	"strconv"
	"strings"
)

// Event 是进入引擎的一次事件（上游 attempt 结束 / check 结果 / 定时触发）。
type Event struct {
	Type string // "upstream" | "check"（schedule 由调度器直接执行，不走匹配）

	// 上游结果
	Provider    string
	Model       string // canonical
	StatusCode  int    // 0 = 无（传输错误等）
	Body        string // 响应体（可能截断）
	Transport   string // "" = 无传输错误，否则类别
	TTFTMs      int64
	TTFTTimeout bool // proxy 判定 TTFT 超时
	IsError     bool // 非 2xx / 传输错误 / TTFT 超时

	// check 结果
	CheckStatus string

	// if 上下文（重试链）
	AttemptNo        int
	KeySwitches      int
	ProviderSwitches int
}

// Engine 持有规则全集（来源混合）并执行匹配。
type Engine struct {
	rules []*Rule
	// 预计算：是否存在可能命中成功响应且需要 body 的规则（tee 旁路开关）
	successBodyRules bool
	minTTFT          int64
}

// NewEngine 组装引擎；规则在加载时已按来源打好 Source。
func NewEngine(all []*Rule) *Engine {
	e := &Engine{rules: all}
	for _, r := range all {
		if r.CanMatchSuccess() && r.NeedsBody() {
			e.successBodyRules = true
		}
		if r.When.TTFTOverMs > 0 && (e.minTTFT == 0 || r.When.TTFTOverMs < e.minTTFT) {
			e.minTTFT = r.When.TTFTOverMs
		}
	}
	return e
}

// Rules 返回全部规则（只读用途）。
func (e *Engine) Rules() []*Rule { return e.rules }

// NeedsSuccessBodyTee 是否需要对成功响应做 tee 镜像（无相关规则时零开销）。
func (e *Engine) NeedsSuccessBodyTee() bool { return e.successBodyRules }

// MinTTFTThreshold 所有规则中最小的 ttft_over_ms（proxy 用它设定时器）；0 = 无 TTFT 规则。
func (e *Engine) MinTTFTThreshold() int64 { return e.minTTFT }

// Match 返回命中的唯一规则（命中即停）；无命中返回 nil。
// 排序：特异性大者优先 → priority 小者优先 → 来源顺序（manifest → user → builtin）。
func (e *Engine) Match(ev *Event) *Rule {
	var candidates []*Rule
	for _, r := range e.rules {
		if e.matches(r, ev) {
			candidates = append(candidates, r)
		}
	}
	if len(candidates) == 0 {
		return nil
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		si, sj := candidates[i].Specificity(), candidates[j].Specificity()
		if si != sj {
			return si > sj
		}
		if candidates[i].Priority != candidates[j].Priority {
			return candidates[i].Priority < candidates[j].Priority
		}
		return candidates[i].Source < candidates[j].Source
	})
	return candidates[0]
}

func (e *Engine) matches(r *Rule, ev *Event) bool {
	if r.When.Type() != ev.Type {
		return false
	}
	w := &r.When
	switch ev.Type {
	case "check":
		if w.CheckStatus != ev.CheckStatus {
			return false
		}
		if w.ScopeProvider != "" && w.ScopeProvider != ev.Provider {
			return false
		}
	case "upstream":
		if w.Provider != "" && w.Provider != ev.Provider {
			return false
		}
		if w.Model != "" && w.Model != ev.Model {
			return false
		}
		if w.StatusCode != "" && !matchStatus(w.StatusCode, ev.StatusCode) {
			return false
		}
		if w.BodyContains != "" && !containsBody(ev.Body, w.BodyContains) {
			return false
		}
		if w.BodyRegex != nil && !w.BodyRegex.MatchString(ev.Body) {
			return false
		}
		if w.Transport != "" {
			if ev.Transport == "" {
				return false
			}
			if w.Transport != "true" && w.Transport != ev.Transport {
				return false
			}
		}
		if w.TTFTOverMs > 0 {
			if !ev.TTFTTimeout || ev.TTFTMs < w.TTFTOverMs {
				return false
			}
		}
		if w.ErrorAny && !ev.IsError {
			return false
		}
	default:
		return false
	}
	// if 条件
	for _, c := range r.If {
		if !evalCond(c, ev) {
			return false
		}
	}
	return true
}

func matchStatus(pattern string, code int) bool {
	if code == 0 {
		return false
	}
	switch pattern {
	case "1xx", "2xx", "3xx", "4xx", "5xx":
		return strconv.Itoa(code/100)+"xx" == pattern
	}
	n, err := strconv.Atoi(pattern)
	return err == nil && n == code
}

func containsBody(body, substr string) bool {
	return body != "" && strings.Contains(body, substr)
}

func evalCond(c Condition, ev *Event) bool {
	var v int
	switch c.Field {
	case "attempt_no":
		v = ev.AttemptNo
	case "key_switches":
		v = ev.KeySwitches
	case "provider_switches":
		v = ev.ProviderSwitches
	}
	switch c.Op {
	case "eq":
		return v == c.Value
	case "ne":
		return v != c.Value
	case "gt":
		return v > c.Value
	case "gte":
		return v >= c.Value
	case "lt":
		return v < c.Value
	case "lte":
		return v <= c.Value
	}
	return false
}
