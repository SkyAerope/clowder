package models

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestCanonicalsOfManyToMany(t *testing.T) {
	path := filepath.Join(t.TempDir(), "models.yaml")
	err := os.WriteFile(path, []byte(`
model-a:
  - shared-native
  - native-a
model-b:
  - shared-native
patterns:
  - match: "^shared-"
    canonical: pattern-model
  - match: "native$"
    canonical: suffix-model
`), 0o644)
	if err != nil {
		t.Fatal(err)
	}
	mapping, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}

	if got, want := mapping.CanonicalsOf("shared-native"), []string{"model-a", "model-b"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("exact canonicals = %v, want %v", got, want)
	}
	if got, want := mapping.CanonicalsOf("shared-other-native"), []string{"pattern-model", "suffix-model"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("pattern canonicals = %v, want %v", got, want)
	}
}

func TestMappingSlicesAreCopies(t *testing.T) {
	path := filepath.Join(t.TempDir(), "models.yaml")
	if err := os.WriteFile(path, []byte("model-a:\n  - native-a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mapping, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	canonicals := mapping.CanonicalsOf("native-a")
	aliases := mapping.Aliases("model-a")
	canonicals[0] = "changed"
	aliases[0] = "changed"
	if mapping.CanonicalsOf("native-a")[0] != "model-a" || mapping.Aliases("model-a")[0] != "native-a" {
		t.Fatal("mapping accessors must return copies")
	}
}
