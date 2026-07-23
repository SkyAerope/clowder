package rules

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

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
