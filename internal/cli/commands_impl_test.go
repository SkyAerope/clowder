package cli

import (
	"bufio"
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
	lines := map[string][]string{}
	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) >= 4 && (fields[0] == "inherited" || fields[0] == "disabled") {
			lines[fields[0]] = fields[1:]
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(lines["inherited"], " "); got != "enabled=true streaming=true keys=0/0 models=0" {
		t.Fatalf("inherited provider fields = %q", got)
	}
	if got := strings.Join(lines["disabled"], " "); got != "enabled=false streaming=false keys=0/0 models=0" {
		t.Fatalf("disabled provider fields = %q", got)
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
