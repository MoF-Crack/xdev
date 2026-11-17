package shim

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// TestFindInDir_PrefersExecutable: when several candidates match the alias,
// a runnable one must win over a non-executable data file.
func TestFindInDir_PrefersExecutable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX execute bits")
	}
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "bin", "tool"), []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(dir, "tool")
	if err := os.WriteFile(want, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	got, ok := FindInDir(dir, "tool")
	if !ok || got != want {
		t.Fatalf("FindInDir = (%q,%v), want (%q,true)", got, ok, want)
	}
}

// TestFindInDir_SingleFilePrefix: AppImage-style payloads keep their upstream
// name; a `shims/cursor` dispatcher must still find it (L16).
func TestFindInDir_SingleFilePrefix(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("AppImage is a Linux format")
	}
	dir := t.TempDir()
	want := filepath.Join(dir, "Cursor-3.14.7-x86_64.AppImage")
	if err := os.WriteFile(want, []byte("appimage"), 0o755); err != nil {
		t.Fatal(err)
	}
	got, ok := FindInDir(dir, "cursor")
	if !ok || got != want {
		t.Fatalf("FindInDir = (%q,%v), want (%q,true)", got, ok, want)
	}
	// Unrelated prefixes must not match (e.g. "cursors" vs "cursor").
	if _, ok := FindInDir(dir, "curs"); ok {
		t.Fatal("partial alias without separator must not match")
	}
}

// TestFindInDir_RejectsSourceHelperPrefix is the git -v regression: a git
// source tree contains GIT-VERSION-GEN, which starts with "git-" but is a
// build helper, not the git binary. It must not be dispatched.
func TestFindInDir_RejectsSourceHelperPrefix(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX execute bits")
	}
	dir := t.TempDir()
	helper := filepath.Join(dir, "GIT-VERSION-GEN")
	if err := os.WriteFile(helper, []byte("#!/bin/sh\necho usage\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got, ok := FindInDir(dir, "git"); ok {
		t.Fatalf("FindInDir dispatched source helper %q as git", got)
	}
	// Version-suffixed single-file payloads still match.
	app := filepath.Join(dir, "git-2.55.0-static")
	if err := os.WriteFile(app, []byte("binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got, ok := FindInDir(dir, "git"); !ok || got != app {
		t.Fatalf("FindInDir = (%q,%v), want (%q,true)", got, ok, app)
	}
}

func TestShimIndexFieldRoundTrip(t *testing.T) {
	cases := []string{
		"plain",
		"with\tTab",
		"with\nNewline",
		`with\Backslash`,
		`\t-not-a-tab`,
		"混合\t中\n文",
	}
	for _, in := range cases {
		enc := encodeIndexField(in)
		if got := decodeIndexField(enc); got != in {
			t.Errorf("round trip %q -> %q -> %q", in, enc, got)
		}
	}
}
