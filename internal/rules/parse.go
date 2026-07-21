package rules

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

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
