// Package rules 实现 IFTTT 风格规则引擎：when（触发）+ if（条件）+ do（副作用）+ route（路由决策）。
package rules

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

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
	//             refresh_models | check_key | check_keys | capture | backoff_key
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

// ---------- YAML 解析 ----------

// rawRule 是 rules.yaml / manifest rules 的原始结构。
type rawRule struct {
	Name        string           `yaml:"name"`
	Description string           `yaml:"description"`
	Priority    *int             `yaml:"priority"`
	Enabled     *bool            `yaml:"enabled"`
	When        map[string]any   `yaml:"when"`
	If          map[string]any   `yaml:"if"`
	Do          []map[string]any `yaml:"do"`
	Route       any              `yaml:"route"`
}

// ParseRules 从 YAML 字节解析规则列表（rules.yaml 顶层为 {rules: [...]}）。
func ParseRules(data []byte, source int) ([]*Rule, error) {
	var doc struct {
		Rules []rawRule `yaml:"rules"`
	}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parse rules: %w", err)
	}
	return buildRules(doc.Rules, source, "")
}

// ParseManifestRules 解析 manifest 内嵌规则（自动附加 provider 作用域）。
func ParseManifestRules(raw []map[string]any, providerName string) ([]*Rule, error) {
	rrs := make([]rawRule, 0, len(raw))
	for _, m := range raw {
		b, err := yaml.Marshal(m)
		if err != nil {
			return nil, err
		}
		var rr rawRule
		if err := yaml.Unmarshal(b, &rr); err != nil {
			return nil, err
		}
		rrs = append(rrs, rr)
	}
	return buildRules(rrs, SourceManifest, providerName)
}

func buildRules(raws []rawRule, source int, forceProvider string) ([]*Rule, error) {
	var out []*Rule
	for i, rr := range raws {
		r, err := buildRule(rr, source)
		if err != nil {
			name := rr.Name
			if name == "" {
				name = fmt.Sprintf("#%d", i)
			}
			return nil, fmt.Errorf("rule %s: %w", name, err)
		}
		if forceProvider != "" && r.When.Type() == "upstream" {
			r.When.Provider = forceProvider // manifest 规则强制限定本 provider
		}
		out = append(out, r)
	}
	return out, nil
}

func buildRule(rr rawRule, source int) (*Rule, error) {
	if rr.Name == "" {
		return nil, fmt.Errorf("missing name")
	}
	r := &Rule{
		Name:        rr.Name,
		Description: rr.Description,
		Priority:    100,
		Source:      source,
		Enabled:     rr.Enabled == nil || *rr.Enabled,
	}
	if rr.Priority != nil {
		r.Priority = *rr.Priority
	}
	if err := parseWhen(rr.When, &r.When); err != nil {
		return nil, err
	}
	conds, err := parseIf(rr.If)
	if err != nil {
		return nil, err
	}
	r.If = conds
	for _, dm := range rr.Do {
		a, err := parseAction(dm)
		if err != nil {
			return nil, err
		}
		r.Do = append(r.Do, a)
	}
	rt, err := parseRoute(rr.Route)
	if err != nil {
		return nil, err
	}
	r.Route = rt
	if r.Route.IsEmpty() && r.When.Type() == "upstream" {
		r.Route = RouteSpec{Single: RoutePassthrough} // 新建规则默认透传
	}
	return r, nil
}

func parseWhen(m map[string]any, w *When) error {
	if len(m) == 0 {
		return fmt.Errorf("when is required")
	}
	for k, v := range m {
		switch k {
		case "provider":
			w.Provider = toString(v)
		case "model":
			w.Model = toString(v)
		case "status_code":
			s := strings.ToLower(toString(v))
			if !validStatusPattern(s) {
				return fmt.Errorf("invalid status_code %q", s)
			}
			w.StatusCode = s
		case "body_contains":
			w.BodyContains = toString(v)
		case "body_regex":
			src := toString(v)
			re, err := regexp.Compile(src) // 保存时编译（RE2），无效直接拒绝
			if err != nil {
				return fmt.Errorf("invalid body_regex: %w", err)
			}
			w.BodyRegex = re
			w.bodyRegexSrc = src
		case "transport":
			switch tv := v.(type) {
			case bool:
				if tv {
					w.Transport = "true"
				}
			case string:
				if !validTransport(tv) {
					return fmt.Errorf("invalid transport %q", tv)
				}
				w.Transport = tv
			default:
				return fmt.Errorf("invalid transport value %v", v)
			}
		case "ttft_over_ms":
			n, err := toInt(v)
			if err != nil || n <= 0 {
				return fmt.Errorf("invalid ttft_over_ms %v", v)
			}
			w.TTFTOverMs = int64(n)
		case "error":
			b, ok := v.(bool)
			if !ok || !b {
				return fmt.Errorf("error trigger must be `true`")
			}
			w.ErrorAny = true
		case "check":
			cm, ok := v.(map[string]any)
			if !ok {
				return fmt.Errorf("when.check must be a mapping")
			}
			st := toString(cm["status"])
			switch st {
			case "active", "invalid", "rate_limited", "error":
				w.CheckStatus = st
			default:
				return fmt.Errorf("invalid check status %q", st)
			}
		case "schedule":
			sm, ok := v.(map[string]any)
			if !ok {
				return fmt.Errorf("when.schedule must be a mapping")
			}
			w.Cron = toString(sm["cron"])
			if w.Cron == "" {
				return fmt.Errorf("schedule.cron is required")
			}
		case "scope":
			sm, ok := v.(map[string]any)
			if !ok {
				return fmt.Errorf("when.scope must be a mapping")
			}
			w.ScopeProvider = toString(sm["provider"])
		default:
			return fmt.Errorf("unknown when field %q", k)
		}
	}
	return nil
}

func parseIf(m map[string]any) ([]Condition, error) {
	var out []Condition
	for field, v := range m {
		switch field {
		case "attempt_no", "key_switches", "provider_switches":
		default:
			return nil, fmt.Errorf("unknown if field %q", field)
		}
		cm, ok := v.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("if.%s must be a mapping like {gte: 2}", field)
		}
		for op, val := range cm {
			switch op {
			case "eq", "ne", "gt", "gte", "lt", "lte":
			default:
				return nil, fmt.Errorf("if.%s: unknown operator %q", field, op)
			}
			n, err := toInt(val)
			if err != nil {
				return nil, fmt.Errorf("if.%s.%s: %w", field, op, err)
			}
			out = append(out, Condition{Field: field, Op: op, Value: n})
		}
	}
	return out, nil
}

func parseAction(m map[string]any) (Action, error) {
	if len(m) != 1 {
		return Action{}, fmt.Errorf("each do item must have exactly one key, got %v", m)
	}
	for k, v := range m {
		a := Action{Kind: k}
		switch k {
		case "mark_key":
			st := toString(v)
			switch st {
			case "invalid", "rate_limited", "active":
				a.MarkStatus = st
			default:
				return a, fmt.Errorf("mark_key: invalid status %q", st)
			}
		case "mark_model_unhealthy", "mark_provider_unhealthy", "refresh_models", "backoff_key":
			// 无参数（值应为 {} 或 nil）
		case "check_key", "check_keys":
			if cm, ok := v.(map[string]any); ok {
				if b, ok := cm["chain"].(bool); ok {
					a.Chain = b
				}
			}
		case "capture":
			if cm, ok := v.(map[string]any); ok {
				if fs, ok := cm["fields"].([]any); ok {
					for _, f := range fs {
						s := toString(f)
						switch s {
						case "request_headers", "request_body", "response_headers", "response_body", "status_code":
							a.Fields = append(a.Fields, s)
						default:
							return a, fmt.Errorf("capture: invalid field %q", s)
						}
					}
				}
			}
		default:
			return a, fmt.Errorf("unknown action %q", k)
		}
		return a, nil
	}
	return Action{}, fmt.Errorf("empty do item")
}

func parseRoute(v any) (RouteSpec, error) {
	switch rv := v.(type) {
	case nil:
		return RouteSpec{}, nil
	case string:
		if !validRoute(rv) {
			return RouteSpec{}, fmt.Errorf("invalid route %q", rv)
		}
		return RouteSpec{Single: rv}, nil
	case []any:
		// 简写形式：route: [retry_same, next_key] 等价于 {race: [...]}
		return parseRace(rv)
	case map[string]any:
		raceV, ok := rv["race"]
		if !ok || len(rv) != 1 {
			return RouteSpec{}, fmt.Errorf("route mapping must be {race: [...]}")
		}
		items, ok := raceV.([]any)
		if !ok || len(items) == 0 {
			return RouteSpec{}, fmt.Errorf("race must be a non-empty list")
		}
		return parseRace(items)
	default:
		return RouteSpec{}, fmt.Errorf("invalid route value %v", v)
	}
}

// parseRace 校验并构造 race 多发路由。
func parseRace(items []any) (RouteSpec, error) {
	if len(items) == 0 {
		return RouteSpec{}, fmt.Errorf("race must be a non-empty list")
	}
	var race []string
	for _, it := range items {
		s := toString(it)
		if !validRoute(s) || s == RoutePassthrough || s == RouteWait {
			return RouteSpec{}, fmt.Errorf("invalid race route %q", s)
		}
		race = append(race, s)
	}
	return RouteSpec{Race: race}, nil
}

// TriggerKey 返回规则触发条件（when + if）的归一化签名。
// 两条规则签名相同 = 触发条件完全一致，会对同一批事件竞争匹配。
func (r *Rule) TriggerKey() string {
	w := &r.When
	var b strings.Builder
	fmt.Fprintf(&b, "t=%s;", w.Type())
	fmt.Fprintf(&b, "prov=%s;model=%s;sc=%s;bc=%s;br=%s;tr=%s;ttft=%d;err=%t;",
		w.Provider, w.Model, w.StatusCode, w.BodyContains, w.bodyRegexSrc,
		w.Transport, w.TTFTOverMs, w.ErrorAny)
	fmt.Fprintf(&b, "check=%s;cron=%s;scope=%s;", w.CheckStatus, w.Cron, w.ScopeProvider)
	// if 条件排序后拼接（顺序无关）
	conds := make([]string, 0, len(r.If))
	for _, c := range r.If {
		conds = append(conds, fmt.Sprintf("%s%s%d", c.Field, c.Op, c.Value))
	}
	sort.Strings(conds)
	b.WriteString("if=" + strings.Join(conds, ","))
	return b.String()
}

// CanMatchSuccess 该规则是否可能命中成功响应
// （无 error / transport / ttft_over_ms / 4xx / 5xx / 具体错误码条件 → 可能命中成功响应）。
func (r *Rule) CanMatchSuccess() bool {
	if r.When.Type() != "upstream" {
		return false
	}
	w := &r.When
	if w.ErrorAny || w.Transport != "" || w.TTFTOverMs > 0 {
		return false
	}
	if w.StatusCode != "" && w.StatusCode != "2xx" {
		// 4xx/5xx/具体非 2xx 错误码都不会命中成功响应
		if w.StatusCode == "4xx" || w.StatusCode == "5xx" {
			return false
		}
		if lo, hi, ok := parseStatusRange(w.StatusCode); ok {
			// 区间完全落在 2xx 之外 → 不会命中成功响应
			if lo > 299 || hi < 200 {
				return false
			}
		} else if n, err := strconv.Atoi(w.StatusCode); err == nil && (n < 200 || n >= 300) {
			return false
		}
	}
	return true
}

// NeedsBody 是否需要响应体参与匹配（决定 tee 旁路）。
func (r *Rule) NeedsBody() bool {
	return r.When.BodyContains != "" || r.When.BodyRegex != nil
}

func validStatusPattern(s string) bool {
	if s == "4xx" || s == "5xx" || s == "2xx" || s == "3xx" || s == "1xx" {
		return true
	}
	if lo, hi, ok := parseStatusRange(s); ok {
		return lo >= 100 && hi <= 599
	}
	n, err := strconv.Atoi(s)
	return err == nil && n >= 100 && n <= 599
}

func validTransport(s string) bool {
	switch s {
	case TransportDNSFailure, TransportConnRefused, TransportTLSError,
		TransportConnectTimeout, TransportStreamCut, TransportMalformed:
		return true
	}
	return false
}

func validRoute(s string) bool {
	switch s {
	case RouteNextKey, RouteNextProvider, RouteRetrySame, RoutePassthrough, RouteWait:
		return true
	}
	return false
}

func toString(v any) string {
	switch s := v.(type) {
	case string:
		return s
	case int:
		return strconv.Itoa(s)
	case nil:
		return ""
	default:
		return fmt.Sprintf("%v", v)
	}
}

func toInt(v any) (int, error) {
	switch n := v.(type) {
	case int:
		return n, nil
	case int64:
		return int(n), nil
	case float64:
		return int(n), nil
	case string:
		return strconv.Atoi(n)
	default:
		return 0, fmt.Errorf("not a number: %v", v)
	}
}
