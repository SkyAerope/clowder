package rules

import "fmt"

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
		case "mark_model_unhealthy", "mark_provider_unhealthy", "backoff_key":
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
						case "request_headers", "request_body", "response_headers", "response_body":
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
