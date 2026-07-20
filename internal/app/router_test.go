package app

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/SkyAerope/clowder/internal/health"
	"github.com/SkyAerope/clowder/internal/models"
	"github.com/SkyAerope/clowder/internal/pool"
	"github.com/SkyAerope/clowder/internal/provider"
	"github.com/SkyAerope/clowder/internal/store"
)

func newRoutingTestApp(t *testing.T) *App {
	t.Helper()
	path := filepath.Join(t.TempDir(), "models.yaml")
	if err := os.WriteFile(path, []byte(`
model-a:
  - native-a
  - shared-native
model-b:
  - shared-native
same-name: []
`), 0o644); err != nil {
		t.Fatal(err)
	}
	mapping, err := models.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	p, err := pool.New(st, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, providerName := range []string{"one", "two"} {
		if err := p.AddKey(&store.Key{Provider: providerName, APIKey: providerName + "-key", Status: store.KeyIdle}); err != nil {
			t.Fatal(err)
		}
	}
	a := &App{
		Mapping: mapping,
		Health:  health.New(),
		Pool:    p,
		providers: map[string]*ProviderState{
			"one": {
				Manifest: &provider.Manifest{Name: "one", DisplayName: "One", Routing: provider.RoutingPref{Priority: 10, Weight: 1}},
				Models:   []string{"native-a", "shared-native"},
			},
			"two": {
				Manifest: &provider.Manifest{Name: "two", DisplayName: "Two", Routing: provider.RoutingPref{Priority: 20, Weight: 1}},
				Models:   []string{"shared-native", "same-name"},
			},
		},
	}
	for _, ps := range a.providers {
		a.rebuildCanonicalLocked(ps)
	}
	return a
}

func TestRebuildCanonicalManyToMany(t *testing.T) {
	a := newRoutingTestApp(t)
	if got, want := a.providers["one"].NativesByCanonical["model-a"], []string{"native-a", "shared-native"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("model-a natives = %v, want %v", got, want)
	}
	if got, want := a.providers["one"].NativesByCanonical["model-b"], []string{"shared-native"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("model-b natives = %v, want %v", got, want)
	}
}

func TestCandidatesForRoutesSameNameCanonicalToNative(t *testing.T) {
	a := newRoutingTestApp(t)

	candidates := a.CandidatesFor("same-name")

	if got, want := len(candidates), 1; got != want {
		t.Fatalf("candidate count = %d, want %d", got, want)
	}
	if got := candidates[0]; got.Provider != "two" || got.Native != "same-name" || got.Canonical != "same-name" {
		t.Fatalf("candidate = %+v", got)
	}
}

func TestCandidatesUseNativeHealth(t *testing.T) {
	a := newRoutingTestApp(t)
	a.Health.MarkModelDown("one", "native-a")

	candidates := a.CandidatesFor("model-a")
	if len(candidates) != 2 {
		t.Fatalf("candidates = %v", candidates)
	}
	for _, candidate := range candidates {
		if candidate.Provider == "one" && candidate.Native == "native-a" {
			t.Fatal("unhealthy native must not suppress or join sibling candidates")
		}
	}
	if candidates[0].Native != "shared-native" {
		t.Fatalf("first candidate = %+v", candidates[0])
	}
}

func TestExactNativeCandidatesAcrossProviders(t *testing.T) {
	a := newRoutingTestApp(t)
	candidates, found := a.CandidatesForNative("shared-native")
	if !found || len(candidates) != 2 {
		t.Fatalf("candidates = %v, found=%v", candidates, found)
	}
	for _, candidate := range candidates {
		if candidate.Native != "shared-native" || candidate.Canonical != "model-a" {
			t.Fatalf("candidate = %+v", candidate)
		}
	}
}
