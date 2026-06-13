package rules

import (
	"testing"
)

func TestDefaultRulesParse(t *testing.T) {
	rs, err := ParseRules([]byte(DefaultRulesYAML), SourceUserFile)
	if err != nil {
		t.Fatalf("default rules invalid: %v", err)
	}
	if len(rs) != 5 {
		t.Fatalf("want 5 default rules, got %d", len(rs))
	}
	_ = PresetRules() // panic 即失败
}

func engineWith(t *testing.T, extraYAML string) *Engine {
	t.Helper()
	all := BuiltinRules()
	def, err := ParseRules([]byte(DefaultRulesYAML), SourceUserFile)
	if err != nil {
		t.Fatal(err)
	}
	all = append(all, def...)
	if extraYAML != "" {
		extra, err := ParseRules([]byte(extraYAML), SourceUserFile)
		if err != nil {
			t.Fatal(err)
		}
		all = append(all, extra...)
	}
	return NewEngine(all)
}

func TestMatchPriority(t *testing.T) {
	e := engineWith(t, `
rules:
  - name: ftl-trial-ended
    priority: 100
    when: { provider: featherless, status_code: 403, body_contains: "trial ended" }
    do: [ { mark_key: invalid } ]
    route: next_key
`)
	// 用户规则(priority 100)胜过 default-4xx-passthrough(8000)
	r := e.Match(&Event{Type: "upstream", Provider: "featherless", StatusCode: 403,
		Body: `{"error":"your trial ended"}`, IsError: true})
	if r == nil || r.Name != "ftl-trial-ended" {
		t.Fatalf("want ftl-trial-ended, got %v", name(r))
	}
	// 其他 provider 的 403 落到默认 4xx
	r = e.Match(&Event{Type: "upstream", Provider: "other", StatusCode: 403, IsError: true})
	if r == nil || r.Name != "default-4xx-passthrough" {
		t.Fatalf("want default-4xx-passthrough, got %v", name(r))
	}
}

func TestBuiltinTransportFallback(t *testing.T) {
	// 默认规则存在时，通用 default-transport-retry(8000) 优先于内置(65535)，
	// 所有传输错误（含 dns/conn/tls）都落到它。
	e := engineWith(t, "")
	r := e.Match(&Event{Type: "upstream", Provider: "p", Transport: TransportDNSFailure, IsError: true})
	if r == nil || r.Name != "default-transport-retry" {
		t.Fatalf("want default-transport-retry, got %v", name(r))
	}
	r = e.Match(&Event{Type: "upstream", Provider: "p", Transport: TransportStreamCut, IsError: true})
	if r == nil || r.Name != "default-transport-retry" {
		t.Fatalf("want default-transport-retry, got %v", name(r))
	}

	// 无默认规则时，内置规则作为兜底命中具体传输错误。
	only := NewEngine(BuiltinRules())
	r = only.Match(&Event{Type: "upstream", Provider: "p", Transport: TransportDNSFailure, IsError: true})
	if r == nil || r.Name != "builtin-dns-failure" {
		t.Fatalf("want builtin-dns-failure, got %v", name(r))
	}
}

func TestCatchAll(t *testing.T) {
	e := engineWith(t, "")
	// 不知名错误（无状态码无 transport，比如 TTFT 之外的奇葩）落兜底
	r := e.Match(&Event{Type: "upstream", Provider: "p", IsError: true})
	if r == nil || r.Name != "default-catch-all" {
		t.Fatalf("want default-catch-all, got %v", name(r))
	}
	// 成功响应不命中任何错误规则
	r = e.Match(&Event{Type: "upstream", Provider: "p", StatusCode: 200})
	if r != nil {
		t.Fatalf("success should not match, got %v", name(r))
	}
}

func TestTTFTRule(t *testing.T) {
	e := engineWith(t, "")
	if e.MinTTFTThreshold() != 7000 {
		t.Fatalf("want min ttft 7000, got %d", e.MinTTFTThreshold())
	}
	r := e.Match(&Event{Type: "upstream", Provider: "p", TTFTTimeout: true, TTFTMs: 7100, IsError: true})
	if r == nil || r.Name != "default-ttft-timeout" {
		t.Fatalf("want default-ttft-timeout, got %v", name(r))
	}
}

func TestIfCondition(t *testing.T) {
	e := engineWith(t, `
rules:
  - name: too-many-key-switches
    priority: 50
    when: { status_code: 429 }
    if:
      key_switches: { gte: 2 }
    route: next_provider
  - name: rate-limit
    priority: 100
    when: { status_code: 429 }
    do: [ { mark_key: rate_limited } ]
    route: next_key
`)
	// key_switches=1：too-many 的 if 不满足，只剩 rate-limit
	r := e.Match(&Event{Type: "upstream", StatusCode: 429, IsError: true, KeySwitches: 1})
	if r == nil || r.Name != "rate-limit" {
		t.Fatalf("want rate-limit, got %v", name(r))
	}
	// key_switches=2：两者都命中，too-many priority 50 < 100 胜出
	r = e.Match(&Event{Type: "upstream", StatusCode: 429, IsError: true, KeySwitches: 2})
	if r == nil || r.Name != "too-many-key-switches" {
		t.Fatalf("want too-many-key-switches, got %v", name(r))
	}
}

func TestCheckTrigger(t *testing.T) {
	e := engineWith(t, `
rules:
  - name: on-check-invalid
    when:
      check: { status: invalid }
    do: [ { mark_key: invalid } ]
`)
	r := e.Match(&Event{Type: "check", Provider: "featherless", CheckStatus: "invalid"})
	if r == nil || r.Name != "on-check-invalid" {
		t.Fatalf("want on-check-invalid, got %v", name(r))
	}
	if r.Route.Single != "" {
		t.Fatalf("check rule should have no default route, got %q", r.Route.Single)
	}
}

func TestInvalidRegexRejected(t *testing.T) {
	_, err := ParseRules([]byte(`
rules:
  - name: bad
    when: { body_regex: "([unclosed" }
`), SourceUserFile)
	if err == nil {
		t.Fatal("invalid regex must be rejected at parse time")
	}
}

func TestManifestRulesScoped(t *testing.T) {
	rs, err := ParseManifestRules([]map[string]any{
		{"name": "trial-ended", "when": map[string]any{"status_code": 403}, "route": "next_key"},
	}, "featherless")
	if err != nil {
		t.Fatal(err)
	}
	if rs[0].When.Provider != "featherless" {
		t.Fatal("manifest rule must be scoped to its provider")
	}
}

func TestFingerprintStability(t *testing.T) {
	a := Fingerprint("p", 429, "", `{"error":"rate limited","request_id":"550e8400-e29b-41d4-a716-446655440000","ts":"2026-06-12T10:00:00Z"}`)
	b := Fingerprint("p", 429, "", `{"error":"rate limited","request_id":"650e8400-e29b-41d4-a716-446655440111","ts":"2026-06-13T11:30:00Z"}`)
	if a != b {
		t.Fatal("fingerprint must normalize UUIDs/timestamps/numbers")
	}
	c := Fingerprint("p", 429, "", `{"error":"trial ended"}`)
	if a == c {
		t.Fatal("different errors must have different fingerprints")
	}
}

func name(r *Rule) string {
	if r == nil {
		return "<nil>"
	}
	return r.Name
}
