package script

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"testing"
	"time"
)

func TestCheck_decodes_last_valid_JSON_line(t *testing.T) {
	// Given
	t.Setenv("CLOWDER_SCRIPT_HELPER", "last-json")
	runner := NewRunner(0, 0)
	command := fmt.Sprintf("%q -test.run=^TestScriptHelperProcess$", os.Args[0])

	// When
	got, err := runner.Check(context.Background(), t.TempDir(), command, "key", 10*time.Second)

	// Then
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "active" || got.TTFTMS != 17 || got.Metadata["source"] != "helper" {
		t.Fatalf("Check() = %#v", got)
	}
}

func TestCheck_rejects_unknown_status(t *testing.T) {
	// Given
	t.Setenv("CLOWDER_SCRIPT_HELPER", "unknown-status")
	runner := NewRunner(0, 0)
	command := fmt.Sprintf("%q -test.run=^TestScriptHelperProcess$", os.Args[0])

	// When
	_, err := runner.Check(context.Background(), t.TempDir(), command, "key", 10*time.Second)

	// Then
	if err == nil {
		t.Fatal("Check() error = nil")
	}
}

func TestCheck_preserves_process_exit_error_class(t *testing.T) {
	// Given
	t.Setenv("CLOWDER_SCRIPT_HELPER", "exit-failure")
	runner := NewRunner(0, 0)
	command := fmt.Sprintf("%q -test.run=^TestScriptHelperProcess$", os.Args[0])

	// When
	_, err := runner.Check(context.Background(), t.TempDir(), command, "key", 10*time.Second)

	// Then
	var exitError *exec.ExitError
	if !errors.As(err, &exitError) {
		t.Fatalf("Check() error type = %T, want wrapped *exec.ExitError", err)
	}
}

func TestCheck_returns_timeout_error_when_process_exceeds_deadline(t *testing.T) {
	// Given
	t.Setenv("CLOWDER_SCRIPT_HELPER", "timeout")
	runner := NewRunner(0, 0)
	command := fmt.Sprintf("%q -test.run=^TestScriptHelperProcess$", os.Args[0])
	timeout := 100 * time.Millisecond
	started := time.Now()

	// When
	_, err := runner.Check(context.Background(), t.TempDir(), command, "key", timeout)
	elapsed := time.Since(started)

	// Then
	if err == nil {
		t.Fatal("Check() error = nil")
	}
	if elapsed < timeout/2 || elapsed > 5*time.Second {
		t.Fatalf("Check() elapsed = %v, want timeout-driven termination near %v", elapsed, timeout)
	}
}

func TestScriptHelperProcess(t *testing.T) {
	switch os.Getenv("CLOWDER_SCRIPT_HELPER") {
	case "":
		return
	case "last-json":
		fmt.Println("starting check")
		fmt.Println(`{"status":"active","metadata":{"source":"helper"},"ttft_ms":17}`)
		fmt.Println("trailing diagnostic")
	case "unknown-status":
		fmt.Println(`{"status":"unknown"}`)
	case "exit-failure":
		os.Exit(23)
	case "timeout":
		for {
		}
	default:
		os.Exit(2)
	}
	os.Exit(0)
}
