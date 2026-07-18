package proxy

import (
	"testing"

	"github.com/SkyAerope/clowder/internal/app"
)

func TestNextCandidateIndex(t *testing.T) {
	candidates := []app.Candidate{
		{Provider: "one", Native: "native-a"},
		{Provider: "one", Native: "native-b"},
		{Provider: "two", Native: "native-c"},
	}
	if got := nextCandidateIndex(candidates, 0, false, map[string]bool{"one": true}); got != 2 {
		t.Fatalf("next provider index = %d, want 2", got)
	}
	if got := nextCandidateIndex(candidates, 0, true, map[string]bool{"one": true}); got != 1 {
		t.Fatalf("next auto edge index = %d, want 1", got)
	}
	if got := nextCandidateIndex(candidates[:2], 0, false, map[string]bool{"one": true}); got != 2 {
		t.Fatalf("pinned provider exhaustion index = %d, want 2", got)
	}
}
