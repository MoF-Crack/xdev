package util

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// 第十一轮审计（Round 10 报告待办 5 / P3-7）：`ActiveVersion` 对**悬空**链接
// 仍返回「已激活」。
//
// 缺陷现场：`fs.go` 只做 `os.Readlink`，**从不检查目标是否存在**：
//
//	target, err := os.Readlink(link)
//	if err != nil { return "", false }
//	return filepath.Base(target), true     // ← 目标可能根本不存在
//
// 触发路径（F-23 场景）：解压失败后 `RemoveAll(dest)` 删掉了 install/<tool>/<ver>，
// 但 `current/<tool>` 链接还在 → 链接悬空。实测（修复前）：
//
//	ActiveVersion = "1.0.0"  ok=true  而  target_exists=false
//
// 危害：`xdev current`、`IsUpgradeAvailable`（经 `currentVersion`）、GUI 的
// `GetActiveVersion`、`doctor`、`profile` 全都看到**幻影**激活版本 —— 用户以为
// 工具可用（PATH 里也有它的 bin），实际二进制不存在。升级判断还会因此认为
// 「已装 1.0.0」，从而**不提示**重新安装。
//
// 修复：`Readlink` 之后再 `os.Stat` 目标，缺失则返回 `("", false)`。
// 这与 Round 10 在 GUI `SetPathMode` 修的方向一致（那里也是「跟随链接后确认
// 确实是目录」）。注意必须用 `os.Stat`（跟随链接），不能用 `os.Lstat`。

// linkTool 在 current/<tool> 建立指向 target 的链接（Windows 用 junction）。
//
// 注意：必须**创建 target 目录本身**（`MkdirAll(target)`），而不是只创建它的父目录
// —— 否则链接从一开始就是悬空的（本守卫初版踩过这个坑：`MkdirAll(filepath.Dir(target))`
// 只建出 `.../tool`，`.../tool/1.0.0` 并不存在，于是「目标存在」的对照用例
// 在修复后反而变红）。
func linkTool(t *testing.T, xmdeDir, tool, target string) {
	t.Helper()
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(xmdeDir, "current", tool)
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS == "windows" {
		// CreateJunction 是仓库既有的 Windows 链接原语（本包内）。
		if err := CreateJunction(link, target); err != nil {
			t.Skipf("cannot create junction (needs no privilege but may be blocked): %v", err)
		}
		return
	}
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("cannot create symlink: %v", err)
	}
}

// TestActiveVersion_DanglingLinkIsNotActive 是核心守卫（P3-7）。
func TestActiveVersion_DanglingLinkIsNotActive(t *testing.T) {
	home := t.TempDir()
	const tool = "danglingtool"

	// 目标目录**先创建**（让链接可建立），随后删除 —— 精确复刻 F-23：
	// 解压失败后 RemoveAll(install/<tool>/<ver>)，但 current/<tool> 还在。
	target := filepath.Join(home, "install", tool, "1.0.0")
	linkTool(t, home, tool, target)

	// 前提断言：链接确实建立成功且当前可解析。
	if ver, ok := ActiveVersion(home, tool); !ok || ver != "1.0.0" {
		t.Fatalf("测试前提不成立：目标存在时 ActiveVersion = (%q, %v)，want (1.0.0, true)", ver, ok)
	}

	// 现在删掉目标 → 链接悬空。
	if err := os.RemoveAll(target); err != nil {
		t.Fatal(err)
	}

	ver, ok := ActiveVersion(home, tool)
	if ok {
		t.Errorf("P3-7 复现：目标已被删除（%s 不存在），ActiveVersion 仍报已激活 = %q —— "+
			"`xdev current`/升级判断/GUI 会看到幻影版本", target, ver)
	}
	if ver != "" {
		t.Errorf("悬空时应返回空版本号，got %q", ver)
	}
}

// 对照：目标存在时必须照常报告（修复不得把正常路径弄坏）。
func TestActiveVersion_ExistingTargetStillReported(t *testing.T) {
	home := t.TempDir()
	const tool = "livetool"
	target := filepath.Join(home, "install", tool, "2.1.0")
	linkTool(t, home, tool, target)

	ver, ok := ActiveVersion(home, tool)
	if !ok || ver != "2.1.0" {
		t.Errorf("ActiveVersion = (%q, %v), want (2.1.0, true)", ver, ok)
	}
}

// 对照：链接本身不存在时仍返回 ("", false)（既有契约）。
func TestActiveVersion_NoLinkIsNotActive(t *testing.T) {
	home := t.TempDir()
	if ver, ok := ActiveVersion(home, "neverlinked"); ok || ver != "" {
		t.Errorf("无链接时 ActiveVersion = (%q, %v), want (\"\", false)", ver, ok)
	}
}

// 对照：目标是**普通文件**（非目录）时也应视为有效 —— 只要存在就报告。
// 这条防止修复写成「必须是目录」而误伤。
func TestActiveVersion_FileTargetCountsAsPresent(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows junction 只能指向目录；文件链接需要 symlink 特权")
	}
	home := t.TempDir()
	const tool = "filetarget"
	target := filepath.Join(home, "install", tool, "3.0.0")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(home, "current", tool)
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("cannot create symlink: %v", err)
	}
	if ver, ok := ActiveVersion(home, tool); !ok || ver != "3.0.0" {
		t.Errorf("存在文件目标时 ActiveVersion = (%q, %v), want (3.0.0, true)", ver, ok)
	}
}

// `ActiveInstallPath` 是同一函数的姊妹（doctor/GUI 用它拿路径）：
// 悬空时必须返回 ""，否则调用方会拿到一条不存在的路径。
func TestActiveInstallPath_DanglingLinkReturnsEmpty(t *testing.T) {
	home := t.TempDir()
	const tool = "danglingpath"
	target := filepath.Join(home, "install", tool, "1.0.0")
	linkTool(t, home, tool, target)

	if got := ActiveInstallPath(home, tool); got == "" {
		t.Fatalf("测试前提不成立：目标存在时 ActiveInstallPath 应返回路径")
	}
	if err := os.RemoveAll(target); err != nil {
		t.Fatal(err)
	}
	if got := ActiveInstallPath(home, tool); got != "" {
		t.Errorf("P3-7 复现：目标已删除，ActiveInstallPath 仍返回 %q（调用方会拿到不存在的路径）", got)
	}
}
