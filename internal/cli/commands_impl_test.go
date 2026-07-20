package cli

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCmdStatusShowsConfiguredProvidersAndEffectiveStreaming(t *testing.T) {
	// Given
	home := t.TempDir()
	t.Setenv("CLOWDER_HOME", home)
	writeStatusFixture(t, home, "inherited", true, "")
	writeStatusFixture(t, home, "disabled", false, "upstream_streaming: false")
	if err := os.WriteFile(filepath.Join(home, "config.yaml"), []byte(`
bind_addr: 127.0.0.1
port: 12100
providers_dir: providers/
routing: { upstream_streaming: true }
`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "models.yaml"), nil, 0o644); err != nil {
		t.Fatal(err)
	}

	// When
	output := captureStdout(t, func() {
		if err := cmdStatus(nil); err != nil {
			t.Fatal(err)
		}
	})

	// Then
	if !strings.Contains(output, "inherited") || !strings.Contains(output, "enabled=true") || !strings.Contains(output, "streaming=true") {
		t.Fatalf("enabled provider missing effective streaming state:\n%s", output)
	}
	if !strings.Contains(output, "disabled") || !strings.Contains(output, "enabled=false") || !strings.Contains(output, "streaming=false") {
		t.Fatalf("disabled provider missing effective streaming state:\n%s", output)
	}
}

func writeStatusFixture(t *testing.T, home, name string, enabled bool, extra string) {
	t.Helper()
	dir := filepath.Join(home, "providers", name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := "name: " + name + "\nbase_url: http://127.0.0.1\nenabled: " + map[bool]string{true: "true", false: "false"}[enabled] + "\nmodels: []\n" + extra + "\n"
	if err := os.WriteFile(filepath.Join(dir, "manifest.yaml"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
}

func captureStdout(t *testing.T, run func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	original := os.Stdout
	os.Stdout = w
	t.Cleanup(func() { os.Stdout = original })
	run()
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	os.Stdout = original
	output, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	return string(output)
}
