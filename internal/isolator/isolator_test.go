package isolator

import (
	"context"
	"os"
	"strings"
	"testing"
)

func TestNewIsolatorNone(t *testing.T) {
	iso, err := NewIsolator(ModeNone)
	if err != nil {
		t.Fatalf("NewIsolator(ModeNone) error: %v", err)
	}
	if iso.Mode() != ModeNone {
		t.Errorf("Mode() = %q, want %q", iso.Mode(), ModeNone)
	}
}

func TestNewIsolatorEmptyString(t *testing.T) {
	iso, err := NewIsolator("")
	if err != nil {
		t.Fatalf("NewIsolator('') error: %v", err)
	}
	if iso.Mode() != ModeNone {
		t.Errorf("Mode() = %q, want %q", iso.Mode(), ModeNone)
	}
}

func TestNewIsolatorInvalid(t *testing.T) {
	_, err := NewIsolator(IsolationMode("bogus"))
	if err == nil {
		t.Fatal("expected error for invalid isolation mode")
	}
	if !strings.Contains(err.Error(), "unsupported") {
		t.Errorf("error = %q, want it to contain 'unsupported'", err.Error())
	}
}

func TestNoneIsolatorSetupTeardown(t *testing.T) {
	iso, err := NewIsolator(ModeNone)
	if err != nil {
		t.Fatalf("NewIsolator(ModeNone) error: %v", err)
	}

	ctx := context.Background()
	testKey := "XDEV_TEST_ISOLATOR_KEY"
	testValue := "xdev_test_value"

	// Save original value
	origValue := os.Getenv(testKey)

	spec := &EnvironmentSpec{
		Env: map[string]string{
			testKey: testValue,
		},
	}

	// Setup should set the env var
	if err := iso.Setup(ctx, spec); err != nil {
		t.Fatalf("Setup error: %v", err)
	}

	got := os.Getenv(testKey)
	if got != testValue {
		t.Errorf("after Setup, os.Getenv(%q) = %q, want %q", testKey, got, testValue)
	}

	// Teardown should restore the original value
	if err := iso.Teardown(ctx); err != nil {
		t.Fatalf("Teardown error: %v", err)
	}

	afterTeardown := os.Getenv(testKey)
	if afterTeardown != origValue {
		t.Errorf("after Teardown, os.Getenv(%q) = %q, want %q", testKey, afterTeardown, origValue)
	}

	// Clean up in case the var didn't exist before
	if origValue == "" {
		os.Unsetenv(testKey)
	}
}

func TestNoneIsolatorSetupTeardownOverwrite(t *testing.T) {
	iso, err := NewIsolator(ModeNone)
	if err != nil {
		t.Fatalf("NewIsolator(ModeNone) error: %v", err)
	}

	ctx := context.Background()
	testKey := "XDEV_TEST_OVERWRITE_KEY"

	// Set an original value
	os.Setenv(testKey, "original_value")
	defer os.Unsetenv(testKey)

	spec := &EnvironmentSpec{
		Env: map[string]string{
			testKey: "new_value",
		},
	}

	if err := iso.Setup(ctx, spec); err != nil {
		t.Fatalf("Setup error: %v", err)
	}

	if got := os.Getenv(testKey); got != "new_value" {
		t.Errorf("after Setup, os.Getenv(%q) = %q, want %q", testKey, got, "new_value")
	}

	if err := iso.Teardown(ctx); err != nil {
		t.Fatalf("Teardown error: %v", err)
	}

	if got := os.Getenv(testKey); got != "original_value" {
		t.Errorf("after Teardown, os.Getenv(%q) = %q, want %q", testKey, got, "original_value")
	}
}

func TestNoneIsolatorExec(t *testing.T) {
	iso, err := NewIsolator(ModeNone)
	if err != nil {
		t.Fatalf("NewIsolator(ModeNone) error: %v", err)
	}

	ctx := context.Background()

	var cmd []string
	if os.PathSeparator == '\\' {
		cmd = []string{"cmd", "/c", "echo", "hello"}
	} else {
		cmd = []string{"echo", "hello"}
	}

	result, err := iso.Exec(ctx, cmd)
	if err != nil {
		t.Fatalf("Exec error: %v", err)
	}
	if result.ExitCode != 0 {
		t.Errorf("ExitCode = %d, want 0", result.ExitCode)
	}
	if !strings.Contains(result.Stdout, "hello") {
		t.Errorf("Stdout = %q, want it to contain 'hello'", result.Stdout)
	}
}

func TestNoneIsolatorExecEmpty(t *testing.T) {
	iso, err := NewIsolator(ModeNone)
	if err != nil {
		t.Fatalf("NewIsolator(ModeNone) error: %v", err)
	}

	ctx := context.Background()

	result, err := iso.Exec(ctx, []string{})
	if err == nil {
		t.Fatal("expected error for empty command")
	}
	if result != nil {
		t.Errorf("expected nil result for empty command, got %+v", result)
	}
}

func TestNoneIsolatorExecWithWorkingDir(t *testing.T) {
	iso, err := NewIsolator(ModeNone)
	if err != nil {
		t.Fatalf("NewIsolator(ModeNone) error: %v", err)
	}

	ctx := context.Background()
	tmpDir := t.TempDir()

	spec := &EnvironmentSpec{
		WorkingDir: tmpDir,
	}
	if err := iso.Setup(ctx, spec); err != nil {
		t.Fatalf("Setup error: %v", err)
	}
	defer iso.Teardown(ctx)

	var cmd []string
	if os.PathSeparator == '\\' {
		cmd = []string{"cmd", "/c", "echo", "ok"}
	} else {
		cmd = []string{"echo", "ok"}
	}

	result, err := iso.Exec(ctx, cmd)
	if err != nil {
		t.Fatalf("Exec error: %v", err)
	}
	if result.ExitCode != 0 {
		t.Errorf("ExitCode = %d, want 0", result.ExitCode)
	}
}

func TestNoneIsolatorTeardownWithoutSetup(t *testing.T) {
	iso, err := NewIsolator(ModeNone)
	if err != nil {
		t.Fatalf("NewIsolator(ModeNone) error: %v", err)
	}

	ctx := context.Background()
	// Teardown without Setup should not panic or error
	if err := iso.Teardown(ctx); err != nil {
		t.Errorf("Teardown without Setup error: %v", err)
	}
}

func TestNoneIsolatorDoubleTeardown(t *testing.T) {
	iso, err := NewIsolator(ModeNone)
	if err != nil {
		t.Fatalf("NewIsolator(ModeNone) error: %v", err)
	}

	ctx := context.Background()
	testKey := "XDEV_TEST_DOUBLE_TD"

	spec := &EnvironmentSpec{
		Env: map[string]string{
			testKey: "value",
		},
	}

	iso.Setup(ctx, spec)
	iso.Teardown(ctx)

	// Second teardown should be safe (origEnv is nil)
	if err := iso.Teardown(ctx); err != nil {
		t.Errorf("second Teardown error: %v", err)
	}
}
