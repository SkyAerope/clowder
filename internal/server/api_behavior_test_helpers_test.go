package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/SkyAerope/clowder/internal/app"
	"github.com/SkyAerope/clowder/internal/config"
	"github.com/SkyAerope/clowder/internal/store"
)

type apiBehaviorFixture struct {
	handler http.Handler
	key     *store.Key
	home    string
}

type apiRequest struct {
	method string
	path   string
	body   string
	cookie *http.Cookie
}

func newAPIBehaviorFixture(t *testing.T, public bool) *apiBehaviorFixture {
	t.Helper()
	home := t.TempDir()
	t.Setenv("CLOWDER_HOME", home)

	cfg := config.Default()
	cfg.Dashboard.Public = public
	cfg.Dashboard.Admin = true
	cfg.Dashboard.AdminKey = "test-admin-key"
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "models.yaml"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	providerDir := filepath.Join(home, "providers", "fixture")
	if err := os.MkdirAll(providerDir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := `name: fixture
display_name: Fixture Provider
base_url: http://127.0.0.1
enabled: true
models:
  - native-model
routing:
  priority: 7
  weight: 2
`
	if err := os.WriteFile(filepath.Join(providerDir, "manifest.yaml"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}

	a, err := app.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(a.Close)
	key := &store.Key{
		Provider: "fixture", APIKey: "fixture-secret", Email: "owner@example.test",
		Metadata: `{"tier":"test"}`, Status: store.KeyIdle,
	}
	if err := a.Pool.AddKey(key); err != nil {
		t.Fatal(err)
	}
	return &apiBehaviorFixture{handler: New(a).Handler(), key: key, home: home}
}

func (f *apiBehaviorFixture) request(t *testing.T, spec apiRequest) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(spec.method, spec.path, strings.NewReader(spec.body))
	if spec.body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if spec.cookie != nil {
		req.AddCookie(spec.cookie)
	}
	recorder := httptest.NewRecorder()
	f.handler.ServeHTTP(recorder, req)
	return recorder
}

func (f *apiBehaviorFixture) login(t *testing.T) *http.Cookie {
	t.Helper()
	recorder := f.request(t, apiRequest{
		method: http.MethodPost,
		path:   "/api/login",
		body:   `{"admin_key":"test-admin-key"}`,
	})
	if recorder.Code != http.StatusOK {
		t.Fatalf("login status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	result := recorder.Result()
	t.Cleanup(func() { _ = result.Body.Close() })
	for _, cookie := range result.Cookies() {
		if cookie.Name == sessionCookie {
			return cookie
		}
	}
	t.Fatal("login response did not set session cookie")
	return nil
}

func decodeJSON(t *testing.T, recorder *httptest.ResponseRecorder, target any) {
	t.Helper()
	decoder := json.NewDecoder(recorder.Body)
	if err := decoder.Decode(target); err != nil {
		t.Fatal(err)
	}
	if _, err := decoder.Token(); err != io.EOF {
		t.Fatalf("response contains trailing JSON: %v", err)
	}
}

func requireObjectKeys(t *testing.T, object map[string]any, expected ...string) {
	t.Helper()
	actual := make([]string, 0, len(object))
	for key := range object {
		actual = append(actual, key)
	}
	sort.Strings(actual)
	sort.Strings(expected)
	if !reflect.DeepEqual(actual, expected) {
		t.Fatalf("object keys = %v, want %v", actual, expected)
	}
}
