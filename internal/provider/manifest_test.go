package provider

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestLoadManifest_applies_defaults_and_deduplicates_models(t *testing.T) {
	// Given
	dir := t.TempDir()
	manifest := "base_url: https://example.test/v1\nmodels: [alpha, '', beta, alpha, beta]\n"
	if err := os.WriteFile(filepath.Join(dir, "manifest.yaml"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}

	// When
	got, err := LoadManifest(dir)

	// Then
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != filepath.Base(dir) || !got.Enabled || got.Format != "openai-compatible" || got.KeyStrategy != StrategySticky {
		t.Fatalf("manifest identity defaults = name %q, enabled %t, format %q, strategy %q", got.Name, got.Enabled, got.Format, got.KeyStrategy)
	}
	if got.Scripts.RegisterTimeout != 300 || got.Scripts.CheckTimeout != 60 {
		t.Fatalf("script timeouts = register %d, check %d", got.Scripts.RegisterTimeout, got.Scripts.CheckTimeout)
	}
	if got.Routing.Priority != 100 || got.Routing.Weight != 1 {
		t.Fatalf("routing defaults = priority %d, weight %d", got.Routing.Priority, got.Routing.Weight)
	}
	if want := []string{"alpha", "beta"}; !reflect.DeepEqual(got.Models, want) {
		t.Fatalf("models = %v, want %v", got.Models, want)
	}
}

func TestScanDir_isolates_bad_manifest_from_valid_providers(t *testing.T) {
	// Given
	root := t.TempDir()
	validDir := filepath.Join(root, "valid")
	invalidDir := filepath.Join(root, "invalid")
	for _, dir := range []string{validDir, invalidDir} {
		if err := os.Mkdir(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(validDir, "manifest.yaml"), []byte("base_url: https://valid.example/v1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(invalidDir, "manifest.yaml"), []byte("name: broken\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	// When
	providers, errs := ScanDir(root)

	// Then
	if len(providers) != 1 || providers[0].Name != "valid" {
		t.Fatalf("providers = %#v, want only valid provider", providers)
	}
	if len(errs) != 1 {
		t.Fatalf("errors = %v, want one isolated manifest error", errs)
	}
}
