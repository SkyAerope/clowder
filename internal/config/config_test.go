package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestDefault_populates_operational_defaults(t *testing.T) {
	// Given
	want := &Config{
		BindAddr: "0.0.0.0",
		Port:     12100,
		Dashboard: DashboardConfig{
			Public: true,
			Admin:  true,
		},
		ProvidersDir: "providers/",
		Pool: PoolConfig{
			MinActiveKeys:              5,
			MaxTotalKeys:               20,
			AutoRegister:               false,
			RegisterInterval:           300,
			RegisterBackoffBaseSeconds: 60,
			RegisterBackoffMaxSeconds:  3600,
		},
		Routing: RoutingConfig{
			MaxRetries:          10,
			MaxParallelAttempts: 2,
			UpstreamStreaming:   true,
		},
		Backoff: BackoffConfig{BaseSeconds: 5, MaxSeconds: 600},
		Capture: CaptureConfig{MaxRecords: 1000, ResponseBufferKB: 256},
	}

	// When
	got := Default()

	// Then
	if !strings.HasPrefix(got.APIKey, "clowder-local-") || len(got.APIKey) <= len("clowder-local-") {
		t.Fatalf("APIKey does not have a generated local-key value: %q", got.APIKey)
	}
	if !strings.HasPrefix(got.Dashboard.AdminKey, "clowder-admin-") || len(got.Dashboard.AdminKey) <= len("clowder-admin-") {
		t.Fatalf("AdminKey does not have a generated admin-key value: %q", got.Dashboard.AdminKey)
	}
	got.APIKey = ""
	got.Dashboard.AdminKey = ""
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Default() = %#v, want %#v", got, want)
	}
}

func TestLoad_rejects_invalid_boundary_values(t *testing.T) {
	tests := []struct {
		name string
		yaml string
	}{
		{name: "port below range", yaml: "port: 0\n"},
		{name: "port above range", yaml: "port: 65536\n"},
		{name: "negative retries", yaml: "port: 12100\nrouting:\n  max_retries: -1\n  max_parallel_attempts: 1\n"},
		{name: "zero parallel attempts", yaml: "port: 12100\nrouting:\n  max_parallel_attempts: 0\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Given
			home := t.TempDir()
			t.Setenv("CLOWDER_HOME", home)
			if err := os.WriteFile(filepath.Join(home, "config.yaml"), []byte(tt.yaml), 0o600); err != nil {
				t.Fatal(err)
			}

			// When
			_, err := Load()

			// Then
			if err == nil {
				t.Fatal("Load() error = nil")
			}
		})
	}
}

func TestSave_then_Load_round_trips_config(t *testing.T) {
	// Given
	t.Setenv("CLOWDER_HOME", t.TempDir())
	want := &Config{
		BindAddr: "127.0.0.1",
		Port:     43210,
		APIKey:   "api-key",
		Dashboard: DashboardConfig{
			Public:   false,
			Admin:    true,
			AdminKey: "admin-key",
		},
		ProvidersDir: "/tmp/providers",
		Pool: PoolConfig{
			MinActiveKeys:              2,
			MaxTotalKeys:               8,
			AutoRegister:               true,
			RegisterInterval:           45,
			RegisterBackoffBaseSeconds: 3,
			RegisterBackoffMaxSeconds:  30,
		},
		Routing: RoutingConfig{MaxRetries: 4, MaxParallelAttempts: 3},
		Backoff: BackoffConfig{BaseSeconds: 2, MaxSeconds: 40},
		Capture: CaptureConfig{Redact: true, MaxRecords: 20, ResponseBufferKB: 64},
	}
	if err := want.Save(); err != nil {
		t.Fatal(err)
	}

	// When
	got, err := Load()

	// Then
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Load() after Save() = %#v, want %#v", got, want)
	}
}
