//go:build integration

package integration

import (
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

// CLI-07 / CLI-08 已删除（2026-10-26）。
//
// 它们测的 `xdev local` 命令与 `.xdev.toml` 配置格式都已不存在：
//   - `xdev local` 不再是注册的子命令（实测 `error: unknown command "local"`）；
//   - 项目级配置改为 `.xdev.yaml`（见 internal/project），不是 TOML。
//
// 这两个用例此前一直是 SKIP 状态（integration job 缺 go build，二进制不存在
// 导致 buildBin 跳过），所以漂移从未被发现。CI 修好后它们立刻暴露为失败 ——
// 删掉而不是改写，是因为要对齐的是「当前实现」，而不是恢复一个已被移除的命令。
//
// 项目级版本的真实覆盖在 internal/project 的单元测试里。

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

// CLI-10 已删除（2026-10-26）。
//
// 它测的 `xdev task` 命令已不存在（实测 `error: unknown command "task"`）。
// 同样此前一直是 SKIP，CI 修好后暴露为失败。
//
// 顺带：docs/usage-manual.md 里也有 task 相关的 TOML 示例，属同一批文档漂移
// （见 docs/backlog.md 的待清理项）。

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
