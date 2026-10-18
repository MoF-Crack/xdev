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

// TestCreateJunction_RejectsUnsafeCharacters：无法在 cmd 命令行里安全表达的字符
// 必须被**明确拒绝**（而不是静默截断或展开）。
func TestCreateJunction_RejectsUnsafeCharacters(t *testing.T) {
	dir := t.TempDir()
	link := filepath.Join(dir, "link")
	target := filepath.Join(dir, "target")

	cases := []struct {
		name   string
		link   string
		target string
	}{
		{"double quote in target", link, filepath.Join(dir, `tar"get`)},
		{"double quote in link", filepath.Join(dir, `li"nk`), target},
		{"percent in target", link, filepath.Join(dir, `tar%get`)},
		{"percent in link", filepath.Join(dir, `li%nk`), target},
		{"bang in target", link, filepath.Join(dir, `tar!get`)},
		{"empty target", link, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := CreateJunction(tc.link, tc.target)
			if err == nil {
				t.Errorf("应拒绝该路径（含无法安全表达的字符），但返回 nil")
				return
			}
			if !strings.Contains(err.Error(), "refusing to create a junction") {
				t.Errorf("错误信息应表明是主动拒绝，got: %v", err)
			}
		})
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
