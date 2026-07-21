package rules

import "testing"

func TestEngineNeedsResponseCaptureTeeOnlyForEnabledRulesCapturingBody(t *testing.T) {
	tests := []struct {
		name string
		yaml string
		want bool
	}{
		{
			name: "explicit response body",
			yaml: `rules: [{ name: capture, do: [{ capture: { fields: [response_body] } }] }]`,
			want: true,
		},
		{
			name: "empty fields captures all",
			yaml: `rules: [{ name: capture, do: [{ capture: {} }] }]`,
			want: true,
		},
		{
			name: "response headers only",
			yaml: `rules: [{ name: capture, do: [{ capture: { fields: [response_headers] } }] }]`,
			want: false,
		},
		{
			name: "disabled response body capture",
			yaml: `rules: [{ name: capture, enabled: false, do: [{ capture: { fields: [response_body] } }] }]`,
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rules, err := ParseRules([]byte(tt.yaml), SourceUserFile)
			if err != nil {
				t.Fatal(err)
			}

			got := NewEngine(rules).NeedsResponseCaptureTee()

			if got != tt.want {
				t.Fatalf("NeedsResponseCaptureTee() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestEngineMinTTFTThresholdUsesLowestEnabledRule(t *testing.T) {
	rules, err := ParseRules([]byte(`
rules:
  - name: slower
    when: { ttft_over_ms: 1200 }
  - name: faster
    when: { ttft_over_ms: 900 }
  - name: disabled
    enabled: false
    when: { ttft_over_ms: 100 }
`), SourceUserFile)
	if err != nil {
		t.Fatal(err)
	}

	got := NewEngine(rules).MinTTFTThreshold()

	if got != 900 {
		t.Fatalf("MinTTFTThreshold() = %d, want 900", got)
	}
}
