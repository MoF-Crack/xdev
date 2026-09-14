//go:build integration

package integration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// CLI-01: init against a fresh temp dir creates the canonical skeleton + config.
func TestInit_FreshDir(t *testing.T) {
	bin := buildBin(t)
	home := t.TempDir()
	out, code := run(t, bin, home, "", "init", "--yes", "--no-path-fix")
	assertCode(t, 0, code, "init")
	contains(t, out, "config written", "init")
	for _, d := range []string{"install", "shims", "cache", "manifests", "downloads", "env"} {
		if !fileExists(filepath.Join(home, d)) {
			t.Errorf("init: %s/ not created", d)
		}
	}
	if !fileExists(filepath.Join(home, "config.yaml")) {
		t.Error("init: config.yaml not written")
	}
}

// CLI-02: init is idempotent — re-running it against an existing home
// either succeeds or prompts gracefully; with --force it must not error.
func TestInit_Idempotent(t *testing.T) {
	bin := buildBin(t)
	home := initHome(t, bin)
	_, code := run(t, bin, home, "", "init", "--yes", "--force", "--no-path-fix")
	assertCode(t, 0, code, "init --force on existing home")
}

// CLI-03: available lists all known tools (>=38), including the core trio.
func TestAvailable_ListsAllTools(t *testing.T) {
	bin := buildBin(t)
	home := initHome(t, bin)
	out, code := run(t, bin, home, "", "available")
	assertCode(t, 0, code, "available")
	for _, name := range []string{"java", "python", "node"} {
		if !strings.Contains(out, name) {
			t.Errorf("available: %q missing from output", name)
		}
	}
	// Header row + ~38 tool rows. Loose lower bound to tolerate catalog churn.
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) < 30 {
		t.Errorf("available: expected >=30 output lines, got %d", len(lines))
	}
}

// CLI-04: search returns matching tools; a nonsense query yields "no tool matches".
func TestSearch_MatchAndMiss(t *testing.T) {
	bin := buildBin(t)
	home := initHome(t, bin)

	hits, code := run(t, bin, home, "", "search", "python")
	assertCode(t, 0, code, "search python")
	contains(t, hits, "python", "search python")
	contains(t, hits, "Python", "search python display name")

	miss, code := run(t, bin, home, "", "search", "zzz-nosuchtool")
	assertCode(t, 0, code, "search zzz")
	contains(t, miss, "no tool matches", "search miss")
}

// CLI-05: doctor on an empty install reports success without crashing.
func TestDoctor_EmptyEnvironment(t *testing.T) {
	bin := buildBin(t)
	home := initHome(t, bin)
	out, code := run(t, bin, home, "", "doctor")
	// doctor may return non-zero when it detects PATH issues; the key
	// expectation is that it runs and produces a report rather than crashing.
	_ = code
	if out == "" {
		t.Error("doctor produced no output")
	}
	notContains(t, out, "panic:", "doctor")
}

// CLI-06: config get/set/list round-trips a value. mirror.<tool> is a
// supported top-level config section (proxy has its own subcommand).
func TestConfig_RoundTrip(t *testing.T) {
	bin := buildBin(t)
	home := initHome(t, bin)

	_, code := run(t, bin, home, "", "config", "set", "mirror.python", "https://example.org/py")
	assertCode(t, 0, code, "config set mirror.python")

	got, code := run(t, bin, home, "", "config", "get", "mirror.python")
	assertCode(t, 0, code, "config get mirror.python")
	contains(t, got, "https://example.org/py", "config get mirror.python round-trip")

	list, code := run(t, bin, home, "", "config", "list")
	assertCode(t, 0, code, "config list")
	contains(t, list, "mirror.python", "config list")
	contains(t, list, "https://example.org/py", "config list reflects set")
}

// CLI-07: local <tool>@<ver> writes .xdev.toml in the cwd and shows it.
func TestLocal_SetAndShow(t *testing.T) {
	bin := buildBin(t)
	home := initHome(t, bin)
	// Run from a project dir so .xdev.toml lands there, not in home.
	proj := t.TempDir()

	out, code := run(t, bin, home, proj, "local", "node@20")
	assertCode(t, 0, code, "local set")
	contains(t, out, "set node", "local set")

	tomlPath := filepath.Join(proj, ".xdev.toml")
	if !fileExists(tomlPath) {
		t.Fatalf(".xdev.toml not created in %s", proj)
	}
	data, err := os.ReadFile(tomlPath)
	if err != nil {
		t.Fatalf("read toml: %v", err)
	}
	if !strings.Contains(string(data), "node") {
		t.Errorf(".xdev.toml missing node: %s", data)
	}
	if !strings.Contains(string(data), "20") {
		t.Errorf(".xdev.toml missing version 20: %s", data)
	}

	show, code := run(t, bin, home, proj, "local", "--show")
	assertCode(t, 0, code, "local --show")
	contains(t, show, "node", "local --show")
}

// CLI-08: local --unset removes a tool from .xdev.toml.
func TestLocal_Unset(t *testing.T) {
	bin := buildBin(t)
	home := initHome(t, bin)
	proj := t.TempDir()

	_, code := run(t, bin, home, proj, "local", "python@3.12")
	assertCode(t, 0, code, "local set python")

	out, code := run(t, bin, home, proj, "local", "--unset", "python")
	assertCode(t, 0, code, "local --unset")
	contains(t, out, "removed python", "local --unset")

	data, _ := os.ReadFile(filepath.Join(proj, ".xdev.toml"))
	// The tool name must be gone from the [tools] table. (Empty [tools]
	// header may remain, which is expected — only the entry is removed.)
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "python") {
			t.Errorf("python still present in .xdev.toml after unset: %s", data)
		}
	}
}

// CLI-09: hook --shell bash with no .xdev.toml produces empty output and exit 0.
func TestHook_NoProjectEmptyOutput(t *testing.T) {
	bin := buildBin(t)
	home := initHome(t, bin)
	empty := t.TempDir()

	out, code := run(t, bin, home, empty, "hook", "--shell", "bash")
	assertCode(t, 0, code, "hook bash (no project)")
	if strings.TrimSpace(out) != "" {
		t.Errorf("hook with no .xdev.toml should emit nothing, got: %q", out)
	}
}

// CLI-10: task list with no .xdev.toml prints a friendly hint and exits 0.
func TestTaskList_NoProject(t *testing.T) {
	bin := buildBin(t)
	home := initHome(t, bin)
	empty := t.TempDir()

	out, code := run(t, bin, home, empty, "task", "list")
	assertCode(t, 0, code, "task list (no project)")
	contains(t, out, "No tasks", "task list no-project hint")
}

// CLI-11: installing an unknown tool fails with a clear error and exit 1.
func TestInstall_UnknownTool(t *testing.T) {
	bin := buildBin(t)
	home := initHome(t, bin)

	out, code := run(t, bin, home, "", "install", "nosuchtool@1")
	if code == 0 {
		t.Errorf("install unknown tool: exit 0, want non-zero\noutput: %s", out)
	}
	contains(t, out, "unsupported tool", "install unknown tool")
}
