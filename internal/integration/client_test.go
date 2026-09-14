//go:build integration

// Package integration contains end-to-end tests that drive the real xdev
// binary (built into build/bin) against an isolated temporary xdev home.
//
// Run with:
//
//	go test -tags integration ./internal/integration/...
//
// Tests never touch the user's real xdev home, PATH registry, or shell RC:
// every test seeds a fresh temp dir as --xmdevenv-dir and runs `init` with
// --no-path-fix so the host environment is left untouched.
package integration

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// buildBin returns the path to the compiled xdev binary used as the
// system-under-test. It prefers build/bin/xdev(.exe), falling back to
// the stdlib `go test` helper package (none here). Tests skip if the
// binary is missing so `go test ./internal/...` (without -tags) still
// works.
func buildBin(t *testing.T) string {
	t.Helper()
	cands := []string{
		filepath.Join("..", "..", "build", "bin", "xdev.exe"),
		filepath.Join("..", "..", "build", "bin", "xdev"),
		filepath.Join("..", "..", "xdev-1.1.7.exe"),
	}
	for _, c := range cands {
		if abs, err := filepath.Abs(c); err == nil {
			if fi, err := os.Stat(abs); err == nil && !fi.IsDir() {
				return abs
			}
		}
	}
	t.Skipf("xdev binary not found in build/bin; run build.sh first (checked relative candidates)")
	return ""
}

// run executes the xdev binary with the given global flags + args, in dir.
// Returns combined output (stdout+stderr trimmed) and the process exit code.
// All invocations inject --no-path-fix to avoid mutating the host PATH/shell
// RC AND to prevent registry env-var writes (e.g. GOROOT, NODE_HOME) that
// would pollute the host environment. The xdev home (tmpdir) is passed via
// --xmdevenv-dir and the XMDEV_HOME env var so lib code paths that read the
// env directly also isolate.
func run(t *testing.T, bin, home, dir string, args ...string) (string, int) {
	t.Helper()
	full := append([]string{"--xmdevenv-dir", home, "--no-path-fix"}, args...)
	cmd := exec.Command(bin, full...)
	if dir != "" {
		cmd.Dir = dir
	}
	// Isolate env: copy host env then force XMDEV_HOME to the temp home.
	cmd.Env = append(os.Environ(), "XMDEV_HOME="+home)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	// On Windows hide the console window to avoid spawning popups.
	if runtime.GOOS == "windows" {
		cmd.SysProcAttr = hideWindowAttr()
	}
	err := cmd.Run()
	code := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			code = exitErr.ExitCode()
		} else {
			t.Fatalf("failed to run xdev %v: %v", args, err)
		}
	}
	return strings.TrimRight(out.String(), "\r\n "), code
}

// initHome runs `xdev init --yes --no-path-fix` against a fresh temp dir,
// returning the home path. Asserts init succeeds and creates the expected
// directory skeleton. Use this as the per-test bootstrap.
func initHome(t *testing.T, bin string) string {
	t.Helper()
	home := t.TempDir()
	out, code := run(t, bin, home, "", "init", "--yes", "--no-path-fix")
	if code != 0 {
		t.Fatalf("init failed (code=%d): %s", code, out)
	}
	// Verify the canonical skeleton was created. current/ is created lazily
	// on first Activate(), so init itself does not pre-create it.
	for _, d := range []string{"install", "shims", "cache", "manifests", "downloads", "env"} {
		if _, err := os.Stat(filepath.Join(home, d)); err != nil {
			t.Errorf("init did not create %s/: %v", d, err)
		}
	}
	if _, err := os.Stat(filepath.Join(home, "config.yaml")); err != nil {
		t.Errorf("init did not write config.yaml: %v", err)
	}
	return home
}

// assertCode compares an exit code, failing with context on mismatch.
func assertCode(t *testing.T, want, got int, what string) {
	t.Helper()
	if got != want {
		t.Errorf("%s: exit code = %d, want %d", what, got, want)
	}
}

// contains asserts substr is in s.
func contains(t *testing.T, s, substr, what string) {
	t.Helper()
	if !strings.Contains(s, substr) {
		t.Errorf("%s: output missing %q\noutput: %s", what, substr, s)
	}
}

// notContains asserts substr is absent from s.
func notContains(t *testing.T, s, substr, what string) {
	t.Helper()
	if strings.Contains(s, substr) {
		t.Errorf("%s: output unexpectedly contains %q\noutput: %s", what, substr, s)
	}
}

// fileExists reports whether path exists (file or dir).
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// hideWindowAttr returns a SysProcAttr that hides the child console window
// on Windows (no-op stub on other platforms). Defined in client_windows_test.go.
// fmt is imported here to keep gofmt happy if the helper is unused on Unix.
var _ = fmt.Sprintf
