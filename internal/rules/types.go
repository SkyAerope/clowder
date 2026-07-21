// Package rules 实现 IFTTT 风格规则引擎：when（触发）+ if（条件）+ do（副作用）+ route（路由决策）。
package rules

import "regexp"

// 规则来源（匹配平手时的最终排序依据：manifest → rules.yaml → builtin）。
const (
	SourceManifest = 0
	SourceUserFile = 1
	SourceBuiltin  = 2
)

// Transport 错误类别。
const (
	TransportDNSFailure     = "dns_failure"
	TransportConnRefused    = "conn_refused"
	TransportTLSError       = "tls_error"
	TransportConnectTimeout = "connect_timeout"
	TransportStreamCut      = "stream_cut"
	TransportMalformed      = "malformed"
)

// Route 取值。
const (
	RouteNextKey      = "next_key"
	RouteNextProvider = "next_provider"
	RouteRetrySame    = "retry_same"
	RoutePassthrough  = "passthrough"
	RouteWait         = "wait" // 继续等待上游首字节，不切换/不透传（仅对上游未返回场景有意义）
)

// Rule 是一条完整规则。
type Rule struct {
	Name        string
	Description string
	Priority    int // 默认 100，越小越优先
	Source      int // SourceManifest / SourceUserFile / SourceBuiltin
	Builtin     bool
	Enabled     bool // false = 规则停用，引擎跳过匹配

	When  When
	If    []Condition
	Do    []Action
	Route RouteSpec
}

// When 三种触发器之一：上游结果（默认，字段平铺）/ check / schedule。
type When struct {
	// --- 上游结果触发字段 ---
	Provider     string
	Model        string
	StatusCode   string // "403" 精确、"400-499" 区间、"" 未设
	BodyContains string
	BodyRegex    *regexp.Regexp
	bodyRegexSrc string
	Transport    string // "" 未设；"true" 任意；或具体类别
	TTFTOverMs   int64  // 0 未设
	ErrorAny     bool   // error: true

	// --- check 触发 ---
	CheckStatus string // active|invalid|rate_limited|error；非空 = check 触发器

	// --- schedule 触发 ---
	Cron          string // 非空 = schedule 触发器
	ScopeProvider string
}

// Type 返回触发器类型。
func (w *When) Type() string {
	if w.Cron != "" {
		return "schedule"
	}
	if w.CheckStatus != "" {
		return "check"
	}
	return "upstream"
}

// BodyRegexSrc 返回 body_regex 的原始正则文本（供 UI 回显）。
func (w *When) BodyRegexSrc() string { return w.bodyRegexSrc }

// Condition 是 if 里的一个数值比较。
type Condition struct {
	Field string // attempt_no | key_switches | provider_switches
	Op    string // eq|ne|gt|gte|lt|lte
	Value int
}

// Action 是 do 里的一个副作用动作（单键）。
type Action struct {
	Kind string // mark_key | mark_model_unhealthy | mark_provider_unhealthy |
	//             check_key | check_keys | capture | backoff_key
	MarkStatus string   // mark_key 的目标状态
	Chain      bool     // check_key / check_keys
	Fields     []string // capture 字段裁剪；空 = 全抓
}

// RouteSpec 单一路由或 race 多发。
type RouteSpec struct {
	Single string   // 非 race 时
	Race   []string // race: [...] 时
}

func (r RouteSpec) IsRace() bool  { return len(r.Race) > 0 }
func (r RouteSpec) IsEmpty() bool { return r.Single == "" && len(r.Race) == 0 }
