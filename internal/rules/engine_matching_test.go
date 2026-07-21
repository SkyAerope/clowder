package rules

import "testing"

func engineWith(t *testing.T, extraYAML string) *Engine {
	t.Helper()
	all := BuiltinRules()
	defaults, err := ParseRules([]byte(DefaultRulesYAML), SourceUserFile)
	if err != nil {
		t.Fatal(err)
	}
	all = append(all, defaults...)
	if extraYAML != "" {
		extra, err := ParseRules([]byte(extraYAML), SourceUserFile)
		if err != nil {
			t.Fatal(err)
		}
		all = append(all, extra...)
	}
	return NewEngine(all)
}

func TestRuleWithoutWhenMatchesAnyUpstreamEvent(t *testing.T) {
	rules, err := ParseRules([]byte(`
rules:
  - name: unconditional
    route: passthrough
`), SourceUserFile)
	if err != nil {
		t.Fatalf("rule without when should parse: %v", err)
	}

	got := NewEngine(rules).Match(&Event{Type: "upstream", StatusCode: 200})

	if got == nil || got.Name != "unconditional" {
		t.Fatalf("rule without when should match upstream events, got %v", ruleName(got))
	}
}

func TestMatchPriority(t *testing.T) {
	engine := engineWith(t, `
rules:
  - name: ftl-trial-ended
    priority: 100
    when: { provider: featherless, status_code: 403, body_contains: "trial ended" }
    do: [ { mark_key: invalid } ]
    route: next_key
`)

	matched := engine.Match(&Event{
		Type: "upstream", Provider: "featherless", StatusCode: 403,
		Body: `{"error":"your trial ended"}`, IsError: true,
	})
	if matched == nil || matched.Name != "ftl-trial-ended" {
		t.Fatalf("want ftl-trial-ended, got %v", ruleName(matched))
	}

	matched = engine.Match(&Event{Type: "upstream", Provider: "other", StatusCode: 403, IsError: true})
	if matched == nil || matched.Route.Single != RoutePassthrough {
		t.Fatalf("other provider route = %v, want passthrough", ruleName(matched))
	}
}

func TestBuiltinTransportPriority(t *testing.T) {
	engine := engineWith(t, "")

	matched := engine.Match(&Event{Type: "upstream", Provider: "p", Transport: TransportDNSFailure, IsError: true})
	if matched == nil || matched.Name != "builtin-dns-failure" {
		t.Fatalf("want builtin-dns-failure, got %v", ruleName(matched))
	}
	matched = engine.Match(&Event{Type: "upstream", Provider: "p", Transport: TransportStreamCut, IsError: true})
	if matched == nil || matched.Route.Single != RouteNextProvider {
		t.Fatalf("stream cut route = %v, want next_provider", ruleName(matched))
	}
}

func TestDisabledBuiltinFallsThroughToUserRules(t *testing.T) {
	builtins := BuiltinRules()
	for _, rule := range builtins {
		if rule.Name == "builtin-dns-failure" {
			rule.Enabled = false
		}
	}
	defaults, err := ParseRules([]byte(DefaultRulesYAML), SourceUserFile)
	if err != nil {
		t.Fatal(err)
	}
	engine := NewEngine(append(builtins, defaults...))

	matched := engine.Match(&Event{Type: "upstream", Provider: "p", Transport: TransportDNSFailure, IsError: true})

	if matched == nil || matched.Route.Single != RouteNextProvider || matched.Builtin {
		t.Fatalf("disabled builtin fallback = %#v", matched)
	}
}

func TestIfConditionSelectsEligibleHigherPriorityRule(t *testing.T) {
	engine := engineWith(t, `
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

	matched := engine.Match(&Event{Type: "upstream", StatusCode: 429, IsError: true, KeySwitches: 1})
	if matched == nil || matched.Name != "rate-limit" {
		t.Fatalf("want rate-limit, got %v", ruleName(matched))
	}
	matched = engine.Match(&Event{Type: "upstream", StatusCode: 429, IsError: true, KeySwitches: 2})
	if matched == nil || matched.Name != "too-many-key-switches" {
		t.Fatalf("want too-many-key-switches, got %v", ruleName(matched))
	}
}

func TestCheckTriggerMatchesStatusWithoutDefaultRoute(t *testing.T) {
	engine := engineWith(t, `
rules:
  - name: on-check-invalid
    when:
      check: { status: invalid }
    do: [ { mark_key: invalid } ]
`)

	matched := engine.Match(&Event{Type: "check", Provider: "featherless", CheckStatus: "invalid"})

	if matched == nil || matched.Name != "on-check-invalid" {
		t.Fatalf("want on-check-invalid, got %v", ruleName(matched))
	}
	if matched.Route.Single != "" {
		t.Fatalf("check rule should have no default route, got %q", matched.Route.Single)
	}
}

func ruleName(rule *Rule) string {
	if rule == nil {
		return "<nil>"
	}
	return rule.Name
}
