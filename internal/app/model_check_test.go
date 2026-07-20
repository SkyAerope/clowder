package app

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/SkyAerope/clowder/internal/health"
	"github.com/SkyAerope/clowder/internal/models"
	"github.com/SkyAerope/clowder/internal/pool"
	"github.com/SkyAerope/clowder/internal/provider"
	"github.com/SkyAerope/clowder/internal/rules"
	"github.com/SkyAerope/clowder/internal/store"
)

func TestCheckModel_returns_matching_rule_with_actions(t *testing.T) {
	// Given
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"yes\"}}]}\n\ndata: [DONE]\n\n")
	}))
	t.Cleanup(upstream.Close)
	a, key := newModelCheckTestApp(t, upstream.URL, []*rules.Rule{
		{
			Name: "record-success", Enabled: true, Priority: 10,
			When: rules.When{Provider: "test", Model: "native-model", StatusCode: "2xx"},
			Do:   []rules.Action{{Kind: "mark_key", MarkStatus: store.KeyActive}},
		},
	})

	// When
	result, err := a.CheckModel(context.Background(), "test", "native-model")

	// Then
	if err != nil {
		t.Fatal(err)
	}
	if result.KeyID != key.ID || result.Status != "active" || result.ModelResponse != "yes" {
		t.Fatalf("result = %+v", result)
	}
	if result.RuleName != "record-success" {
		t.Fatalf("rule name = %q, want record-success", result.RuleName)
	}
	if got := a.Health.Status("test", "native-model"); got != health.Healthy {
		t.Fatalf("health = %q, want %q", got, health.Healthy)
	}
}

func TestCheckModel_hides_matching_rule_without_actions(t *testing.T) {
	// Given
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	t.Cleanup(upstream.Close)
	a, _ := newModelCheckTestApp(t, upstream.URL, []*rules.Rule{{
		Name: "route-only", Enabled: true, When: rules.When{StatusCode: "2xx"},
		Route: rules.RouteSpec{Single: rules.RouteNextProvider},
	}})

	// When
	result, err := a.CheckModel(context.Background(), "test", "native-model")

	// Then
	if err != nil {
		t.Fatal(err)
	}
	if result.RuleName != "" {
		t.Fatalf("rule name = %q, want empty", result.RuleName)
	}
}

func TestApplyModelCheckRule_runs_rule_actions(t *testing.T) {
	// Given
	a, key := newModelCheckTestApp(t, "http://unused", []*rules.Rule{{
		Name: "invalidate", Enabled: true,
		Do: []rules.Action{{Kind: "mark_key", MarkStatus: store.KeyInvalid}},
	}})

	// When
	err := a.ApplyModelCheckRule(ModelCheckRuleApplication{
		ProviderName: "test", NativeModel: "native-model", KeyID: key.ID,
		RuleName: "invalidate", StatusCode: http.StatusOK, ResponseBody: "yes",
	})

	// Then
	if err != nil {
		t.Fatal(err)
	}
	if got := a.Pool.FindByID(key.ID).Status; got != store.KeyInvalid {
		t.Fatalf("key status = %q, want %q", got, store.KeyInvalid)
	}
}

func TestApplyModelCheckRule_rejects_rule_that_does_not_match_check_result(t *testing.T) {
	// Given
	a, key := newModelCheckTestApp(t, "http://unused", []*rules.Rule{{
		Name: "invalidate-on-error", Enabled: true,
		When: rules.When{StatusCode: "500"},
		Do:   []rules.Action{{Kind: "mark_key", MarkStatus: store.KeyInvalid}},
	}})

	// When
	err := a.ApplyModelCheckRule(ModelCheckRuleApplication{
		ProviderName: "test", NativeModel: "native-model", KeyID: key.ID,
		RuleName: "invalidate-on-error", StatusCode: http.StatusOK, ResponseBody: "yes",
	})

	// Then
	if err == nil {
		t.Fatal("expected unmatched rule to be rejected")
	}
	if got := a.Pool.FindByID(key.ID).Status; got == store.KeyInvalid {
		t.Fatalf("key status = %q, rule action should not run", got)
	}
}

func newModelCheckTestApp(t *testing.T, baseURL string, configuredRules []*rules.Rule) (*App, *store.Key) {
	t.Helper()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	p, err := pool.New(st, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	key := &store.Key{Provider: "test", APIKey: "secret", Status: store.KeyIdle}
	if err := p.AddKey(key); err != nil {
		t.Fatal(err)
	}
	mapping, err := models.Load(t.TempDir() + "/missing.yaml")
	if err != nil {
		t.Fatal(err)
	}
	state := &ProviderState{
		Manifest: &provider.Manifest{Name: "test", BaseURL: baseURL, Scripts: provider.ScriptsConfig{CheckTimeout: 2}},
		Models:   []string{"native-model"},
	}
	a := &App{
		Pool: p, Health: health.New(), Mapping: mapping, Engine: rules.NewEngine(configuredRules),
		configuredProviders: map[string]*ProviderState{"test": state},
		providers:           map[string]*ProviderState{"test": state},
		Log:                 slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	return a, key
}
