package rules

import "testing"

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
	rules, err := ParseManifestRules([]map[string]any{
		{"name": "trial-ended", "when": map[string]any{"status_code": 403}, "route": "next_key"},
	}, "featherless")
	if err != nil {
		t.Fatal(err)
	}

	if rules[0].When.Provider != "featherless" {
		t.Fatal("manifest rule must be scoped to its provider")
	}
}
