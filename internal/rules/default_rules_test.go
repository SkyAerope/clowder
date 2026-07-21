package rules

import "testing"

func TestDefaultRules_route_and_capture_representative_upstream_failures(t *testing.T) {
	rules, err := ParseRules([]byte(DefaultRulesYAML), SourceUserFile)
	if err != nil {
		t.Fatalf("ParseRules(DefaultRulesYAML) error = %v", err)
	}
	engine := NewEngine(rules)

	tests := []struct {
		name      string
		event     Event
		wantRoute string
	}{
		{
			name:      "client rejection is returned to the caller",
			event:     Event{Type: "upstream", StatusCode: 418, IsError: true},
			wantRoute: RoutePassthrough,
		},
		{
			name:      "server failure tries another provider",
			event:     Event{Type: "upstream", StatusCode: 503, IsError: true},
			wantRoute: RouteNextProvider,
		},
		{
			name:      "stream interruption tries another provider",
			event:     Event{Type: "upstream", Transport: TransportStreamCut, IsError: true},
			wantRoute: RouteNextProvider,
		},
		{
			name:      "long first-token timeout tries another provider",
			event:     Event{Type: "upstream", TTFTTimeout: true, TTFTMs: 8_000, IsError: true},
			wantRoute: RouteNextProvider,
		},
		{
			name:      "unclassified failure is returned to the caller",
			event:     Event{Type: "upstream", IsError: true},
			wantRoute: RoutePassthrough,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			matched := engine.Match(&tt.event)

			if matched == nil {
				t.Fatal("Match() = nil")
			}
			if matched.Route.Single != tt.wantRoute {
				t.Fatalf("Match().Route = %q, want %q", matched.Route.Single, tt.wantRoute)
			}
			if len(matched.Do) != 1 || matched.Do[0].Kind != "capture" {
				t.Fatalf("Match().Do = %#v, want one capture action", matched.Do)
			}
			if !matched.CapturesField("response_body") {
				t.Fatal("matched rule does not capture the upstream response body")
			}
		})
	}
}

func TestDefaultRules_do_not_route_successful_upstream_responses(t *testing.T) {
	rules, err := ParseRules([]byte(DefaultRulesYAML), SourceUserFile)
	if err != nil {
		t.Fatalf("ParseRules(DefaultRulesYAML) error = %v", err)
	}

	matched := NewEngine(rules).Match(&Event{Type: "upstream", StatusCode: 204})

	if matched != nil {
		t.Fatalf("Match() = %q, want no route", matched.Name)
	}
}
