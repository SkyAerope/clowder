package rules

import (
	"strings"
	"testing"
)

func TestInvalidRegexRejected(t *testing.T) {
	_, err := ParseRules([]byte(`
rules:
  - slug: bad
    when: { body_regex: "([unclosed" }
`), SourceUserFile)

	if err == nil {
		t.Fatal("invalid regex must be rejected at parse time")
	}
}

func TestManifestRulesScoped(t *testing.T) {
	rules, err := ParseManifestRules([]map[string]any{
		{"trial-ended": map[string]any{"name": "试用已结束", "when": map[string]any{"status_code": 403}, "route": "next_key"}},
	}, "featherless")
	if err != nil {
		t.Fatal(err)
	}

	if rules[0].When.Provider != "featherless" {
		t.Fatal("manifest rule must be scoped to its provider")
	}
	if rules[0].Slug != "trial-ended" || rules[0].Name != "试用已结束" {
		t.Fatalf("identity = (%q, %q), want slug and display name", rules[0].Slug, rules[0].Name)
	}
}

func TestParseRules_rejects_legacy_name_description_schema(t *testing.T) {
	// Given
	data := []byte(`rules:
  - name: legacy-rule
    description: Legacy rule
    when: { error: true }
`)

	// When
	_, err := ParseRules(data, SourceUserFile)

	// Then
	if err == nil {
		t.Fatal("legacy rule schema must be rejected")
	}
}

func TestParseRules_rejects_inline_slug_field_schema(t *testing.T) {
	// Given
	data := []byte(`rules:
  - slug: legacy-rule
    name: Legacy rule
    when: { error: true }
`)

	// When
	_, err := ParseRules(data, SourceUserFile)

	// Then
	if err == nil {
		t.Fatal("inline slug field schema must be rejected")
	}
}

func TestParseRules_rejects_duplicate_action_kinds(t *testing.T) {
	// Given
	data := []byte(`rules:
  - duplicate-capture:
      do:
        - capture: { fields: [request_body] }
        - capture: { fields: [response_body] }
`)

	// When
	_, err := ParseRules(data, SourceUserFile)

	// Then
	if err == nil || !strings.Contains(err.Error(), `duplicate action "capture"`) {
		t.Fatalf("ParseRules() error = %v, want duplicate capture rejection", err)
	}
}

func TestParseRules_rejects_status_code_capture_field(t *testing.T) {
	// Given
	data := []byte(`rules:
  - capture-status:
      do:
        - capture: { fields: [status_code] }
`)

	// When
	_, err := ParseRules(data, SourceUserFile)

	// Then
	if err == nil || !strings.Contains(err.Error(), `capture: invalid field "status_code"`) {
		t.Fatalf("ParseRules() error = %v, want status_code capture rejection", err)
	}
}
