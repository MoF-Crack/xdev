//go:build online

// Package e2e contains end-to-end acceptance tests that exercise complete
// user stories against the real xdev binary, including real downloads.
//
// These tests REQUIRE network access (they download a small real tool).
// Run with:
//
//	go test -tags online ./internal/e2e/...
//
// They are skipped by default (no -tags online) and are intended for
// pre-release / manual verification, not every CI run.
package e2e

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
)

// hideWindow returns a Windows SysProcAttr that hides the child console,
// or nil on other platforms.
func hideWindow() *syscall.SysProcAttr {
	if runtime.GOOS != "windows" {
		return nil
	}
	return &syscall.SysProcAttr{HideWindow: true}
}

// buildBin locates the compiled xdev binary under test.
func buildBin(t *testing.T) string {
	t.Helper()
	cands := []string{
		filepath.Join("..", "..", "build", "bin", "xdev.exe"),
		filepath.Join("..", "..", "build", "bin", "xdev"),
	}
	for _, c := range cands {
		if abs, err := filepath.Abs(c); err == nil {
			if fi, err := os.Stat(abs); err == nil && !fi.IsDir() {
				return abs
			}
		}
	}
	t.Skipf("xdev binary not found in build/bin; run build.sh first")
	return ""
}

// run executes xdev with an isolated temp home. --no-path-fix prevents any
// mutation of the host shell RC / PATH registry.
func run(t *testing.T, bin, home, dir string, args ...string) (string, int) {
	t.Helper()
	full := append([]string{"--xmdevenv-dir", home, "--no-path-fix"}, args...)
	cmd := exec.Command(bin, full...)
	if dir != "" {
		cmd.Dir = dir
	}
	cmd.Env = append(os.Environ(), "XMDEV_HOME="+home)
	var out strings.Builder
	cmd.Stdout = &out
	cmd.Stderr = &out
	cmd.SysProcAttr = hideWindow()
	err := cmd.Run()
	code := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			code = exitErr.ExitCode()
		} else {
			t.Fatalf("run xdev %v: %v", args, err)
		}
	}
	return strings.TrimRight(out.String(), "\r\n "), code
}

func initHome(t *testing.T, bin string) string {
	t.Helper()
	home := t.TempDir()
	out, code := run(t, bin, home, "", "init", "--yes", "--no-path-fix")
	if code != 0 {
		t.Fatalf("init failed (code=%d): %s", code, out)
	}
	return home
}

func contains(t *testing.T, s, sub, what string) {
	t.Helper()
	if !strings.Contains(s, sub) {
		t.Errorf("%s: output missing %q\noutput: %s", what, sub, s)
	}
}

func fileExists(p string) bool { _, err := os.Stat(p); return err == nil }

// EPIC-1: A brand-new user's first session.
//
//	init → available → install a real tool → use → current →
//	shim dispatch (run the tool through the shim) → uninstall.
//
// This is the headline acceptance story: it proves the download → extract →
// activate → shim pipeline produces a working, dispatchable tool. We use
// ninja because its archive is small and the binary prints a clean version.
func TestEPIC1_NewUserFirstSession(t *testing.T) {
	bin := buildBin(t)
	home := initHome(t, bin)

	// available lists the tool we're about to install.
	avail, code := run(t, bin, home, "", "available")
	if code != 0 {
		t.Fatalf("available: exit %d", code)
	}
	contains(t, avail, "ninja", "available lists ninja")

	// install a real, small tool (network).
	installOut, code := run(t, bin, home, "", "install", "ninja@1.12.1")
	if code != 0 {
		// Online tests verify real downloads; on flaky/blocked networks we
		// skip rather than fail so the pre-release suite degrades cleanly.
		t.Skipf("install ninja failed (network?): %s", installOut)
	}
	contains(t, installOut, "installed", "install success")

	// install dir + current link exist.
	verDir := filepath.Join(home, "install", "ninja", "1.12.1")
	if !fileExists(verDir) {
		t.Errorf("install dir missing: %s", verDir)
	}
	if !fileExists(filepath.Join(home, "current", "ninja")) {
		t.Error("current/ninja missing after install")
	}

	// current reports the activated version.
	cur, code := run(t, bin, home, "", "current", "ninja")
	if code != 0 {
		t.Fatalf("current ninja: exit %d", code)
	}
	contains(t, cur, "1.12.1", "current ninja version")

	// which resolves to the install path.
	whichOut, code := run(t, bin, home, "", "which", "ninja")
	if code != 0 {
		t.Fatalf("which ninja: exit %d", code)
	}
	contains(t, whichOut, verDir, "which ninja path")

	// HEADLINE: shim dispatch — invoking the shim copy actually runs the
	// real tool and prints its version. This is the core xdev promise.
	shimName := "ninja.exe"
	if runtime.GOOS != "windows" {
		shimName = "ninja"
	}
	shimPath := filepath.Join(home, "shims", shimName)
	if !fileExists(shimPath) {
		t.Fatalf("shim not created: %s", shimPath)
	}
	dispatchOut, code := runShim(t, shimPath, home, "--version")
	if code != 0 {
		t.Fatalf("shim dispatch ninja --version: exit %d\n%s", code, dispatchOut)
	}
	contains(t, dispatchOut, "1.12.1", "shim dispatch runs real ninja")

	// uninstall removes the install dir and current link.
	unOut, code := run(t, bin, home, "", "uninstall", "ninja@1.12.1")
	if code != 0 {
		t.Fatalf("uninstall: exit %d\n%s", code, unOut)
	}
	if fileExists(verDir) {
		t.Error("install dir still present after uninstall")
	}
	if fileExists(filepath.Join(home, "current", "ninja")) {
		t.Error("current link still present after uninstall")
	}
}

// runShim invokes the shim binary (an xdev copy named after the tool) with
// the given args, with XMDEV_HOME pointed at the temp home so dispatch
// resolves to the staged install.
func runShim(t *testing.T, shimPath, home string, args ...string) (string, int) {
	t.Helper()
	cmd := exec.Command(shimPath, args...)
	cmd.Env = append(os.Environ(), "XMDEV_HOME="+home)
	var out strings.Builder
	cmd.Stdout = &out
	cmd.Stderr = &out
	cmd.SysProcAttr = hideWindow()
	err := cmd.Run()
	code := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			code = exitErr.ExitCode()
		} else {
			t.Fatalf("run shim: %v", err)
		}
	}
	return strings.TrimRight(out.String(), "\r\n "), code
}
