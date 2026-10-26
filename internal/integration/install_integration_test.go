//go:build integration

package integration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// stageToolDir pre-creates install/<tool>/<ver> with a bin/<tool> file,
// simulating an already-extracted (already-installed) tool. This lets us
// exercise use/current/which/list/uninstall without any network or
// extraction. Returns the version dir path.
func stageToolDir(t *testing.T, home, tool, ver string) string {
	t.Helper()
	verDir := filepath.Join(home, "install", tool, ver)
	binDir := filepath.Join(verDir, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("stage %s/%s: %v", tool, ver, err)
	}
	binName := tool
	if isWindows() && !strings.HasSuffix(binName, ".exe") {
		binName += ".exe"
	}
	if err := os.WriteFile(filepath.Join(binDir, binName), []byte("#!/bin/sh\necho hi\n"), 0o755); err != nil {
		t.Fatalf("write bin: %v", err)
	}
	return verDir
}

func isWindows() bool {
	return filepath.Separator == '\\'
}

// INST-A1: use on a pre-staged install dir switches the active version and
// updates current/<tool>. The --no-path-fix flag (injected by run()) now
// also prevents registry env-var writes, so the host environment is left
// untouched.
func TestUse_SwitchesActiveVersion(t *testing.T) {
	bin := buildBin(t)
	home := initHome(t, bin)
	stageToolDir(t, home, "node", "20.18.0")

	out, code := run(t, bin, home, "", "use", "node@20.18.0")
	if code != 0 {
		t.Fatalf("use failed (code=%d): %s", code, out)
	}
	contains(t, out, "active", "use success")

	link := filepath.Join(home, "current", "node")
	if !fileExists(link) {
		t.Fatalf("current/node not created after use")
	}
	// current/<tool> should resolve to the staged install dir.
	if target, err := os.Readlink(link); err != nil {
		t.Errorf("current/node is not a link: %v", err)
	} else if filepath.Clean(target) != filepath.Clean(filepath.Join(home, "install", "node", "20.18.0")) {
		t.Errorf("current/node -> %q, want install/node/20.18.0", target)
	}
}

// INST-A2: current reports the activated version after use.
func TestCurrent_ReportsActiveVersion(t *testing.T) {
	bin := buildBin(t)
	home := initHome(t, bin)
	stageToolDir(t, home, "python", "3.12.0")
	_, code := run(t, bin, home, "", "use", "python@3.12.0")
	if code != 0 {
		t.Fatal("use python precondition failed")
	}

	out, code := run(t, bin, home, "", "current", "python")
	if code != 0 {
		t.Fatalf("current python: exit %d, out=%s", code, out)
	}
	contains(t, out, "3.12.0", "current python version")
}

// INST-A3: which <tool> prints the install path of the active version.
func TestWhich_ResolvesInstallPath(t *testing.T) {
	bin := buildBin(t)
	home := initHome(t, bin)
	verDir := stageToolDir(t, home, "go", "1.22.0")
	_, code := run(t, bin, home, "", "use", "go@1.22.0")
	if code != 0 {
		t.Fatal("use go precondition failed")
	}

	out, code := run(t, bin, home, "", "which", "go")
	if code != 0 {
		t.Fatalf("which go: exit %d, out=%s", code, out)
	}
	contains(t, out, verDir, "which go path")
}

// INST-A4: list shows the installed tool with its activated version.
func TestList_ShowsInstalledTool(t *testing.T) {
	bin := buildBin(t)
	home := initHome(t, bin)
	stageToolDir(t, home, "node", "20.18.0")
	_, code := run(t, bin, home, "", "use", "node@20.18.0")
	if code != 0 {
		t.Fatal("use node precondition failed")
	}

	out, code := run(t, bin, home, "", "list")
	if code != 0 {
		t.Fatalf("list: exit %d, out=%s", code, out)
	}
	contains(t, out, "node", "list node")
	contains(t, out, "20.18.0", "list node version")
}

// INST-A5: uninstall removes the install dir AND the current link.
func TestUninstall_RemovesInstallAndCurrent(t *testing.T) {
	bin := buildBin(t)
	home := initHome(t, bin)
	verDir := stageToolDir(t, home, "node", "20.18.0")
	_, code := run(t, bin, home, "", "use", "node@20.18.0")
	if code != 0 {
		t.Fatal("use node precondition failed")
	}

	out, code := run(t, bin, home, "", "uninstall", "node@20.18.0")
	if code != 0 {
		t.Fatalf("uninstall: exit %d, out=%s", code, out)
	}
	if fileExists(verDir) {
		t.Errorf("install dir %s still exists after uninstall", verDir)
	}
	if fileExists(filepath.Join(home, "current", "node")) {
		t.Error("current/link still exists after uninstall")
	}
}

// INST-A6: switching between two staged versions re-points current/.
func TestUse_SwitchesBetweenVersions(t *testing.T) {
	bin := buildBin(t)
	home := initHome(t, bin)
	stageToolDir(t, home, "node", "20.18.0")
	stageToolDir(t, home, "node", "22.4.0")

	_, code := run(t, bin, home, "", "use", "node@20.18.0")
	if code != 0 {
		t.Fatal("use 20.18.0 precondition failed")
	}
	if tgt, err := os.Readlink(filepath.Join(home, "current", "node")); err == nil && !strings.HasSuffix(tgt, "20.18.0") {
		t.Errorf("expected current/node -> 20.18.0, got %s", tgt)
	}

	_, code = run(t, bin, home, "", "use", "node@22.4.0")
	if code != 0 {
		t.Fatal("use 22.4.0 failed")
	}
	if tgt, err := os.Readlink(filepath.Join(home, "current", "node")); err == nil && !strings.HasSuffix(tgt, "22.4.0") {
		t.Errorf("expected current/node -> 22.4.0, got %s", tgt)
	}
	// The previously active dir must still exist (switching is non-destructive).
	if !fileExists(filepath.Join(home, "install", "node", "20.18.0")) {
		t.Error("20.18.0 install dir was removed by switch")
	}
}

// INST-L1 / INST-L2 已删除（2026-10-26）。
//
// 它们测的是 `xdev install --locked` 与 `--from-local` 两个标志，而这两个
// 标志在当前实现里**根本不存在**：
//   - internal/cli/install.go 只注册了 4 个 flag；
//   - `internal/lockfile` 已在 Spec 012 删除（见 docs/architecture.md）。
//
// 实测：`error: unknown flag: --locked` / `error: unknown flag: --from-local`。
// 这两个用例此前一直是 SKIP（integration job 缺 go build），漂移从未被发现；
// CI 修好后立刻暴露为失败。
//
// 相关文档也需清理（docs/usage-manual.md 仍文档化这两个标志），
// 属同一批"文档 / 测试 / 代码"三方漂移。
