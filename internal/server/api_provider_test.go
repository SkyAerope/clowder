package server

import (
	"net/http"
	"testing"
)

func TestProviderDetailReturnsEmptyModelsAsJSONArray(t *testing.T) {
	// Given
	fixture := newAPIBehaviorFixture(t, true)
	cookie := fixture.login(t)
	updated := fixture.request(t, apiRequest{
		method: http.MethodPut,
		path:   "/api/admin/providers/fixture/manifest",
		body:   `{"yaml":"name: fixture\ndisplay_name: Fixture Provider\nbase_url: http://127.0.0.1\nenabled: true\nmodels: []\n"}`,
		cookie: cookie,
	})
	if updated.Code != http.StatusOK {
		t.Fatalf("manifest update status = %d, want %d: %s", updated.Code, http.StatusOK, updated.Body.String())
	}

	// When
	recorder := fixture.request(t, apiRequest{
		method: http.MethodGet,
		path:   "/api/admin/providers/fixture",
		cookie: cookie,
	})

	// Then
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	var body struct {
		Models []string `json:"models"`
	}
	decodeJSON(t, recorder, &body)
	if body.Models == nil || len(body.Models) != 0 {
		t.Fatalf("provider models = %#v, want non-nil empty array", body.Models)
	}
}
