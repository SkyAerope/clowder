package proxy_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestProviderQualifiedModels(t *testing.T) {
	mockModels := make(chan string, 4)
	otherModels := make(chan string, 4)
	var mockCalls, otherCalls int32
	mock := modelUpstream(t, &mockCalls, mockModels, http.StatusOK)
	defer mock.Close()
	other := modelUpstream(t, &otherCalls, otherModels, http.StatusOK)
	defer other.Close()
	a, h := setupQualifiedProviders(t, mock.URL, other.URL)
	a.Health.MarkSuccess("mock", "mock/canonical-native", 10)
	a.Health.MarkSuccess("mock", "shared-name", 20)
	a.Health.MarkSuccess("other", "other/canonical-native", 30)
	a.Health.MarkSuccess("other", "shared-name", 40)

	t.Run("canonical includes same-named native", func(t *testing.T) {
		canonicalCandidates, found := a.CandidatesForProvider("shared-name", "mock")
		if !found || len(canonicalCandidates) != 2 {
			t.Fatalf("canonical candidates = %v, found=%v", canonicalCandidates, found)
		}
		canonicalNatives := map[string]bool{}
		for _, candidate := range canonicalCandidates {
			canonicalNatives[candidate.Native] = true
		}
		if !canonicalNatives["mock/canonical-native"] || !canonicalNatives["shared-name"] {
			t.Fatalf("canonical natives = %v", canonicalNatives)
		}
		nativeCandidates, found := a.CandidatesForNativeProvider("shared-name", "mock")
		if !found || len(nativeCandidates) != 1 || nativeCandidates[0].Native != "shared-name" {
			t.Fatalf("native candidates = %v, found=%v", nativeCandidates, found)
		}
		w := chatReq(t, h, `{"model":"shared-name:mock","messages":[]}`)
		if w.Code != http.StatusOK {
			t.Fatalf("status %d: %s", w.Code, w.Body.String())
		}
		if got := <-mockModels; !canonicalNatives[got] {
			t.Fatalf("upstream model = %q", got)
		}
	})

	t.Run("native selects provider exactly", func(t *testing.T) {
		w := chatReq(t, h, `{"model":"other/direct-native:other","messages":[]}`)
		if w.Code != http.StatusOK {
			t.Fatalf("status %d: %s", w.Code, w.Body.String())
		}
		if got := <-otherModels; got != "other/direct-native" {
			t.Fatalf("upstream model = %q", got)
		}
	})

	t.Run("mapped native remains exact", func(t *testing.T) {
		w := chatReq(t, h, `{"model":"other/canonical-native:other","messages":[]}`)
		if w.Code != http.StatusOK {
			t.Fatalf("status %d: %s", w.Code, w.Body.String())
		}
		if got := <-otherModels; got != "other/canonical-native" {
			t.Fatalf("upstream model = %q", got)
		}
		var resp struct {
			Model string `json:"model"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatal(err)
		}
		if resp.Model != "other/canonical-native:other" {
			t.Fatalf("response model = %q", resp.Model)
		}
	})

	if got := atomic.LoadInt32(&mockCalls); got != 1 {
		t.Fatalf("mock calls = %d", got)
	}
	if got := atomic.LoadInt32(&otherCalls); got != 2 {
		t.Fatalf("other calls = %d", got)
	}

	apiReq := httptest.NewRequest("GET", "/api/models", nil)
	apiW := httptest.NewRecorder()
	h.ServeHTTP(apiW, apiReq)
	if apiW.Code != http.StatusOK {
		t.Fatalf("GET /api/models status %d: %s", apiW.Code, apiW.Body.String())
	}
	var graph map[string][]struct {
		Provider string `json:"provider"`
		Native   string `json:"native"`
		Best     bool   `json:"best"`
	}
	if err := json.Unmarshal(apiW.Body.Bytes(), &graph); err != nil {
		t.Fatal(err)
	}
	if len(graph["shared-name"]) != 4 || len(graph["secondary-name"]) != 2 {
		t.Fatalf("unexpected graph: %s", apiW.Body.String())
	}
	wantBest := map[string]struct {
		provider string
		native   string
	}{
		"shared-name":    {provider: "mock", native: "mock/canonical-native"},
		"secondary-name": {provider: "mock", native: "shared-name"},
	}
	for canonical, expected := range wantBest {
		best := 0
		for _, edge := range graph[canonical] {
			if !edge.Best {
				continue
			}
			best++
			if edge.Provider != expected.provider || edge.Native != expected.native {
				t.Fatalf("canonical %q best edge = %s/%s, want %s/%s", canonical, edge.Provider, edge.Native, expected.provider, expected.native)
			}
		}
		if best != 1 {
			t.Fatalf("canonical %q has %d best edges, want 1", canonical, best)
		}
	}
}

func TestProviderQualifiedModelNotFound(t *testing.T) {
	mockModels := make(chan string, 4)
	otherModels := make(chan string, 4)
	var mockCalls, otherCalls int32
	mock := modelUpstream(t, &mockCalls, mockModels, http.StatusOK)
	defer mock.Close()
	other := modelUpstream(t, &otherCalls, otherModels, http.StatusOK)
	defer other.Close()
	_, h := setupQualifiedProviders(t, mock.URL, other.URL)

	for _, model := range []string{"shared-name:missing", "other/direct-native:mock", "configured-only-name"} {
		w := chatReq(t, h, fmt.Sprintf(`{"model":%q,"messages":[]}`, model))
		if w.Code != http.StatusNotFound {
			t.Fatalf("model %q status %d: %s", model, w.Code, w.Body.String())
		}
	}
	if got := atomic.LoadInt32(&mockCalls); got != 0 {
		t.Fatalf("mock calls = %d", got)
	}
	if got := atomic.LoadInt32(&otherCalls); got != 0 {
		t.Fatalf("other calls = %d", got)
	}
}

func TestExactNativeRoutesAcrossProviders(t *testing.T) {
	mockModels := make(chan string, 4)
	otherModels := make(chan string, 4)
	var mockCalls, otherCalls int32
	mock := modelUpstream(t, &mockCalls, mockModels, http.StatusOK)
	defer mock.Close()
	other := modelUpstream(t, &otherCalls, otherModels, http.StatusOK)
	defer other.Close()
	_, h := setupQualifiedProviders(t, mock.URL, other.URL)

	w := chatReq(t, h, `{"model":"common-native","messages":[]}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	if calls := atomic.LoadInt32(&mockCalls) + atomic.LoadInt32(&otherCalls); calls != 1 {
		t.Fatalf("upstream calls = %d", calls)
	}
	select {
	case got := <-mockModels:
		if got != "common-native" {
			t.Fatalf("upstream model = %q", got)
		}
	case got := <-otherModels:
		if got != "common-native" {
			t.Fatalf("upstream model = %q", got)
		}
	default:
		t.Fatal("no provider received native model")
	}
}

func TestProviderQualifiedModelDoesNotFailOver(t *testing.T) {
	mockModels := make(chan string, 4)
	otherModels := make(chan string, 4)
	var mockCalls, otherCalls int32
	mock := modelUpstream(t, &mockCalls, mockModels, http.StatusInternalServerError)
	defer mock.Close()
	other := modelUpstream(t, &otherCalls, otherModels, http.StatusOK)
	defer other.Close()
	_, h := setupQualifiedProviders(t, mock.URL, other.URL)

	w := chatReq(t, h, `{"model":"shared-name:mock","messages":[]}`)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	if got := atomic.LoadInt32(&mockCalls); got != 1 {
		t.Fatalf("mock calls = %d", got)
	}
	if got := atomic.LoadInt32(&otherCalls); got != 0 {
		t.Fatalf("other provider must not be called, got %d calls", got)
	}
}
