package server

import (
	"encoding/json"
	"testing"
)

func TestProviderDetailModelsEncodeEmptySliceAsArray(t *testing.T) {
	// Given
	models := append([]string{}, []string(nil)...)

	// When
	encoded, err := json.Marshal(map[string]any{"models": models})

	// Then
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(encoded), `{"models":[]}`; got != want {
		t.Fatalf("encoded provider models = %s, want %s", got, want)
	}
}
