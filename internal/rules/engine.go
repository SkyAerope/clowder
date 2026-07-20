package rules

import (
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

	// FingerprintCache 由调用方填充的错误指纹缓存（避免重复计算）。
	FingerprintCache string
}

// Engine 持有规则全集（来源混合）并执行匹配。
type Engine struct {
	rules []*Rule
	// 预计算：是否存在可能命中成功响应且需要 body 的规则（tee 旁路开关）
	successBodyRules     bool
	responseCaptureRules bool
	minTTFT              int64
}

// NewEngine 组装引擎；规则在加载时已按来源打好 Source。
func NewEngine(all []*Rule) *Engine {
	e := &Engine{rules: all}
	for _, r := range all {
		if !r.Enabled {
			continue
		}
		if r.CanMatchSuccess() && r.NeedsBody() {
			e.successBodyRules = true
		}
		if r.When.Type() == "upstream" && r.CapturesField("response_body") {
			e.responseCaptureRules = true
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

// NeedsResponseCaptureTee 是否有启用的上游规则要求抓取响应体。
func (e *Engine) NeedsResponseCaptureTee() bool { return e.responseCaptureRules }

// MinTTFTThreshold 所有规则中最小的 ttft_over_ms（proxy 用它设定时器）；0 = 无 TTFT 规则。
func (e *Engine) MinTTFTThreshold() int64 { return e.minTTFT }

// Match 返回命中的唯一规则（命中即停）；无命中返回 nil。
// 排序：priority 小者优先 → 来源顺序（manifest → user → builtin）→ 定义顺序。
func (e *Engine) Match(ev *Event) *Rule {
	var best *Rule
	for _, r := range e.rules {
		if !e.matches(r, ev) {
			continue
		}
		// 仅在严格更优时替换：完全平手保留更早定义的规则（稳定）。
		if best == nil || ruleLess(r, best) {
			best = r
		}
	}
	return best
}

// ruleLess 报告 a 是否比 b 优先：priority 小者优先，其次来源顺序。
func ruleLess(a, b *Rule) bool {
	if a.Priority != b.Priority {
		return a.Priority < b.Priority
	}
	return a.Source < b.Source
}

func (e *Engine) matches(r *Rule, ev *Event) bool {
	if !r.Enabled {
		return false
	}
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
	if lo, hi, ok := parseStatusRange(pattern); ok {
		return code >= lo && code <= hi
	}
	n, err := strconv.Atoi(pattern)
	return err == nil && n == code
}

// parseStatusRange 解析 "400-499" 区间，返回 [lo, hi]。
func parseStatusRange(pattern string) (lo, hi int, ok bool) {
	i := strings.IndexByte(pattern, '-')
	if i <= 0 || i >= len(pattern)-1 {
		return 0, 0, false
	}
	a, err1 := strconv.Atoi(pattern[:i])
	b, err2 := strconv.Atoi(pattern[i+1:])
	if err1 != nil || err2 != nil || a > b {
		return 0, 0, false
	}
	return a, b, true
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
