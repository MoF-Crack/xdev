//go:build windows

package util

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// 第九轮审计 P1-1 守卫（安全）：junction 创建不得让 cmd.exe 二次解析路径。
//
// 缺陷现场：`exec.Command("cmd", "/c", "mklink", "/J", link, target)`。os/exec 在
// Windows 上只对「含空格/制表符/引号」的参数加双引号，而 cmd.exe 会**再解析一遍**
// 命令行 —— `&` 是合法的文件名字符，于是含 `&` 的 tool name / version / 安装根目录
// 会把 mklink 的参数切成多条命令，构成 **manifest 可控的任意命令执行**。
//
// 实测（审计原始 PoC）：`Activate(tool="inj&probe&", ver="1.0.0")` → err=nil 且
// PATH 上的 probe.exe 被执行；cmd 同时报 `'.tmp-…' 不是内部或外部命令`。
//
// 修复后：命令行由实现自行构造并为每个路径加双引号（`& | < > ^ ( )` 在引号内是
// 字面量），且拒绝无法安全表达的字符（`"`、`%`、`!`、控制字符）。

// TestCreateJunction_AmpersandIsNotInterpreted 是本项的核心红→绿守卫。
//
// 构造要点（决定了守卫是否真的 discriminates）：路径**不能含空格**。
// os/exec 只对「含空格/制表符/引号」的参数加双引号 —— 含空格的 `&` 会被 Go 引起来、
// 于是 cmd 不会二次解析，测试就会假绿（初版踩过这个坑）。审计 PoC 用的正是不含空格的
// `inj&probe&`。这里用 `targetInj&exit1&`：若 cmd 二次解析，`&` 后的 `exit1` 会被当
// 命令执行、`mklink` 的参数被截断，命令行提前以非零退出并报「不是内部或外部命令」。
func TestCreateJunction_AmpersandIsNotInterpreted(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("cmd.exe command-line interpretation is Windows-specific")
	}

	dir := t.TempDir()
	// 关键：路径里不含空格，确保 os/exec 不会替我们加引号。
	target := filepath.Join(dir, "targetInj&exit1&")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatalf("mkdir target: %v", err)
	}
	link := filepath.Join(dir, "linkInj&exit1&")

	err := CreateJunction(link, target)
	if err != nil {
		// 允许失败，但错误必须可诊断，且**绝不能**是「命令被截断」导致的。
		if strings.Contains(err.Error(), "不是内部或外部命令") ||
			strings.Contains(err.Error(), "is not recognized as an internal") {
			t.Errorf("P1-1 复现：cmd.exe 二次解析了含 & 的路径（参数被切断，命令被执行）: %v", err)
		}
		t.Logf("CreateJunction 返回错误（可接受，只要不是被截断）: %v", err)
		return
	}

	// 成功路径：junction 必须真的指向**完整** target（而不是被截断到 "…/targetInj"）。
	got, rerr := os.Readlink(link)
	if rerr != nil {
		if _, serr := os.Stat(link); serr != nil {
			t.Fatalf("junction 创建成功但既不可 Readlink 也不可 Stat: readlink=%v stat=%v", rerr, serr)
		}
		return
	}
	if !strings.Contains(got, "targetInj&exit1&") {
		t.Errorf("P1-1 复现：junction 目标被截断（cmd 二次解析）: got %q, want 含 %q",
			got, "targetInj&exit1&")
	}
}

// TestCreateJunction_UnsafeForCmdCharactersNowWork 是 Round 11 对 Round 9 守卫的
// **契约更新**（如实记录，不是「改测试让测试过」）。
//
// Round 9 的 P1-1 修复分两层：
//
//	1.（安全层）命令行自行构造 + 每个路径加双引号 —— 让 `& | < > ^ ( )` 在引号内
//	   成为字面量；
//	2.（限制层）**拒绝**无法在 cmd 命令行里安全表达的字符：`"`、`%`、`!`、控制字符。
//
// 第 2 层是 **mklink 时代的产物**，不是安全目标本身。Round 11 把 junction 改成原生
// `FSCTL_SET_REPARSE_POINT`（不经 cmd.exe）后，`%` / `!` / `&` / `^` 这些在文件名里
// **合法**的字符不再有任何危险 —— 本机实测它们全部能正常创建并连通：
//
//	"tar%get": err=<nil> connected=true
//	"tar!get": err=<nil> connected=true
//	"tar&get": err=<nil> connected=true
//	"tar^get": err=<nil> connected=true
//
// 因此这条守卫从「必须拒绝」改为「必须可用」，并保留**真正的安全断言**：
//   - `"` 在 Windows 文件名里**非法**（文件系统层面就建不出目录），因此仍然失败，
//     但失败原因是 OS 而非我们主动拒绝 —— 这不需要我们额外拦。
//   - 空路径仍必须被**主动明确拒绝**。
//
// 旧行为保留为**独立收益**：`D:\100%Portable`、`E:\Tools!SDK` 这类安装根目录
// 在 Round 9/10 下**无法激活**（被拒绝），Round 11 后可用。
func TestCreateJunction_UnsafeForCmdCharactersNowWork(t *testing.T) {
	dir := t.TempDir()

	// 这些字符在 Windows 文件名里合法，原生 junction 必须支持。
	for _, name := range []string{`tar%get`, `tar!get`, `tar&get`, `tar^get`, `tar(paren)`} {
		t.Run("supports_"+name, func(t *testing.T) {
			target := filepath.Join(dir, name)
			if err := os.MkdirAll(target, 0o755); err != nil {
				t.Skipf("无法创建含 %q 的目录（文件系统限制）: %v", name, err)
			}
			link := filepath.Join(dir, "link-"+name)
			if err := CreateJunction(link, target); err != nil {
				t.Fatalf("原生 junction 应支持含 %q 的路径（Round 9 的 mklink 实现会拒绝）: %v", name, err)
			}
			// 必须真的连通（不能只是建了个空目录）。
			_ = os.WriteFile(filepath.Join(target, "p.txt"), []byte("x"), 0o644)
			if _, err := os.ReadFile(filepath.Join(link, "p.txt")); err != nil {
				t.Errorf("junction 未连通: %v", err)
			}
		})
	}
}

// 空路径必须被**主动明确拒绝**（这条与实现机制无关，永久有效）。
func TestCreateJunction_RejectsEmptyPaths(t *testing.T) {
	dir := t.TempDir()
	link := filepath.Join(dir, "link")
	if err := CreateJunction(link, ""); err == nil {
		t.Error("空 target 必须被拒绝")
	}
	if err := CreateJunction("", filepath.Join(dir, "t")); err == nil {
		t.Error("空 link 必须被拒绝")
	}
}

// 对照：普通路径仍能正常创建 junction（修复不得把正常路径弄红）。
func TestCreateJunction_NormalPathStillWorks(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link")

	if err := CreateJunction(link, target); err != nil {
		t.Fatalf("普通路径应能创建 junction: %v", err)
	}
	if _, err := os.Stat(link); err != nil {
		t.Errorf("junction 应可访问: %v", err)
	}
}

// 对照：含空格的路径（原先唯一被 os/exec 加引号的情形）必须仍然工作。
func TestCreateJunction_PathWithSpacesStillWorks(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "my tools", "sdk")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "my link")
	if err := CreateJunction(link, target); err != nil {
		t.Fatalf("含空格的路径应能创建 junction: %v", err)
	}
	if _, err := os.Stat(link); err != nil {
		t.Errorf("junction 应可访问: %v", err)
	}
}
