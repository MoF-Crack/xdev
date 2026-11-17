package env

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestInjectShellRC_WritesLoginProfileToo covers the WSL/Linux fix for
// "init reports ✔ but `bash -lc` still has no shims": Ubuntu bash reads
// ~/.bashrc only in interactive shells, while login shells read ~/.profile.
func TestInjectShellRC_WritesLoginProfileToo(t *testing.T) {
	home := t.TempDir()
	rc := filepath.Join(home, ".bashrc")
	profile := filepath.Join(home, ".profile")
	if err := os.WriteFile(rc, []byte("# rc\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(profile, []byte("# profile\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	info := ShellInfo{Shell: "bash", RCFile: rc, LoginRCFile: profile}

	if err := InjectShellRC(info, "/h/.xdev", "/h/.xdev/shims"); err != nil {
		t.Fatalf("InjectShellRC: %v", err)
	}
	for _, p := range []string{rc, profile} {
		data, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(data), marker) {
			t.Errorf("%s is missing the xdev block:\n%s", p, data)
		}
	}

	if err := RemoveShellRC(info); err != nil {
		t.Fatalf("RemoveShellRC: %v", err)
	}
	for p, want := range map[string]string{rc: "# rc\n", profile: "# profile\n"} {
		data, _ := os.ReadFile(p)
		if string(data) != want {
			t.Errorf("%s after remove = %q, want %q", p, data, want)
		}
	}
}

// TestRenderShellBlock_PathPrependGuarded: the block now goes into two files
// (and login interactive shells source both), so the PATH prepend must be
// idempotent rather than a bare `export PATH=...:$PATH`.
func TestRenderShellBlock_PathPrependGuarded(t *testing.T) {
	block := renderShellBlock("bash", "/h/.xdev", "/h/.xdev/shims")
	if !strings.Contains(block, `case ":$PATH:"`) {
		t.Fatalf("PATH prepend is not guarded:\n%s", block)
	}
	if !strings.Contains(block, "_xdev_shim") {
		t.Fatalf("expected the guard variable in:\n%s", block)
	}
	if strings.Contains(block, `export PATH='/h/.xdev/shims':"$PATH"`) {
		t.Fatalf("unguarded PATH prepend still present:\n%s", block)
	}
	// XMDEV_HOME is still exported.
	if !strings.Contains(block, "export XMDEV_HOME='/h/.xdev'") {
		t.Fatalf("XMDEV_HOME export missing:\n%s", block)
	}
}

// TestRenderShellBlock_FishQuotesSingleQuote: fish uses backslash escaping
// inside single quotes; the POSIX '\” idiom is wrong there.
func TestRenderShellBlock_FishQuotesSingleQuote(t *testing.T) {
	block := renderShellBlock("fish", "/h/it's", "/h/shim's")
	if !strings.Contains(block, `set -gx XMDEV_HOME '/h/it\'s'`) {
		t.Fatalf("fish XMDEV_HOME is not fish-quoted:\n%s", block)
	}
	if strings.Contains(block, `'\''`) {
		t.Fatalf("POSIX quoting leaked into fish output:\n%s", block)
	}
}

func TestDetectUnixShell_LoginProfileDefault(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux/WSL-only login profile shape")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SHELL", "/bin/bash")
	info := detectUnixShell()
	if info.RCFile != filepath.Join(home, ".bashrc") {
		t.Errorf("RCFile = %q", info.RCFile)
	}
	if info.LoginRCFile != filepath.Join(home, ".profile") {
		t.Errorf("LoginRCFile = %q, want ~/.profile", info.LoginRCFile)
	}
	// An existing .bash_profile must win (bash's documented login order).
	if err := os.WriteFile(filepath.Join(home, ".bash_profile"), []byte("#"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := detectUnixShell().LoginRCFile; got != filepath.Join(home, ".bash_profile") {
		t.Errorf("LoginRCFile = %q, want ~/.bash_profile", got)
	}
}
