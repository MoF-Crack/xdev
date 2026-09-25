package shim

// 缺陷票 .scratch/shim-alias-case-bug/issue.md 的行为级守卫测试（plan.md §2）。
//
// 先红后绿：G1/G3/G4/G5 在修复前必然失败（红输出登记票卡 Comments），
// G6 在 Linux CI 上恒绿（Unix 大小写精确保留的回归门）。
// 根因：MaybeExec 用大小写敏感的 TrimSuffix 剥扩展名，而 Windows 命令解析
// （NTFS/PATHEXT/CreateProcess/Python shutil.which）大小写不敏感——
// argv[0]="npm.EXE" 带着扩展名进 Lookup → "shim alias \"npm.EXE\" not installed"。

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/xmdemt/xdev/internal/config"
)

// G1：MaybeExec 大小写矩阵端到端分发（Windows-gated：Unix run() 是 exec 覆换，
// 进程内断言不可行，正例由手动冒烟 MS-F1 覆盖——先例 shim_test.go L389）。
//
// 生产触发路径：Python shutil.which("npm") 按 PATHEXT（大写 .EXE）构造候选，
// NTFS 大小写不敏感命中磁盘小写文件后把大写路径作为 argv[0] 传入。
func TestMaybeExec_AliasCaseMatrixDispatches(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("positive dispatch exec-replaces the test process on unix; covered by manual smoke MS-F1")
	}

	home := t.TempDir()
	curDir := filepath.Join(home, "current", "fakepy")
	if err := os.MkdirAll(curDir, 0o755); err != nil {
		t.Fatal(err)
	}
	selfPath, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ext := exeSuffix()
	targetPath := filepath.Join(curDir, "fakepy"+ext)
	data, err := os.ReadFile(selfPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(targetPath, data, 0o755); err != nil {
		t.Fatal(err)
	}

	t.Setenv("XMDEV_HOME", home)
	config.ResetHomeDirForTest()
	InvalidateShimIndex()

	// MaybeExec 把 os.Args[1:] 透传给子进程；子进程是本测试二进制的副本，
	// 必须中和 test flags 防止递归跑套件（先例 shim_test.go L428）。
	origArgs := os.Args
	defer func() { os.Args = origArgs }()

	variants := []string{
		"fakepy" + ext, // 基线：小写（修复前即应通过）
		"fakepy.EXE",   // 生产事故形态（bugx → npm.EXE）
		"FAKEPY.EXE",   // 全大写
		"FakePy.Exe",   // 混合
	}
	for _, v := range variants {
		os.Args = []string{origArgs[0], "-test.run=^$"}
		handled, err := MaybeExec(v)
		if !handled {
			t.Fatalf("variant %q: handled = false, want true", v)
		}
		if err != nil {
			t.Errorf("variant %q: dispatch failed: %v", v, err)
		}
	}
}

// G3：Lookup 输入侧大小写折叠（Windows-gated）。跨工具别名布局
// （npm 位于 current/node/ 而非 current/npm/）——专有目录布局会被 NTFS
// 大小写不敏感的 os.Stat 直接救活，测不出索引键域缺陷，必须用跨工具路径。
func TestLookup_FoldsCrossToolAliasCase(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows folds alias case; Unix preserves it (TestLookup_PreservesCaseOnUnix)")
	}

	home := t.TempDir()
	nodeDir := filepath.Join(home, "current", "node")
	if err := os.MkdirAll(nodeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	npmPath := filepath.Join(nodeDir, "npm.exe")
	if err := os.WriteFile(npmPath, []byte("dummy"), 0o755); err != nil {
		t.Fatal(err)
	}

	t.Setenv("XMDEV_HOME", home)
	config.ResetHomeDirForTest()
	InvalidateShimIndex()

	lowerTarget, _, _, errLower := Lookup("npm", nil)
	if errLower != nil {
		t.Fatalf("baseline lowercase lookup failed: %v", errLower)
	}
	upperTarget, _, _, errUpper := Lookup("NPM", nil)
	if errUpper != nil {
		t.Fatalf("uppercase alias must resolve through the same case domain: %v", errUpper)
	}
	if !sameFilePath(lowerTarget, upperTarget) {
		t.Errorf("uppercase lookup resolved %q, want the same target as lowercase %q", upperTarget, lowerTarget)
	}
}

// G4：索引键折叠——磁盘上以大写命名的外壳文件（NPM.CMD）必须以折叠键
// "npm" 进入反向索引并被小写查找命中（Windows-gated；Unix 上 NPM.CMD 与
// npm 本就是两个不同文件，键保留原样才是正确语义）。
func TestShimIndex_FoldsUppercaseDiskNames(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("index key folding is a Windows case-domain requirement")
	}

	home := t.TempDir()
	demoDir := filepath.Join(home, "current", "demo")
	if err := os.MkdirAll(demoDir, 0o755); err != nil {
		t.Fatal(err)
	}
	upperTool := filepath.Join(demoDir, "NPM.CMD")
	if err := os.WriteFile(upperTool, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}

	t.Setenv("XMDEV_HOME", home)
	config.ResetHomeDirForTest()
	InvalidateShimIndex() // 触发内存+磁盘索引重建

	idx := buildShimIndexMap(home)
	if _, ok := idx["npm"]; !ok {
		t.Errorf("reverse index must fold key to %q, got keys like %v", "npm", mapKeys(idx))
	}
	target, _, _, err := Lookup("npm", nil)
	if err != nil {
		t.Fatalf("Lookup(npm) must hit current/demo/NPM.CMD via folded key: %v", err)
	}
	if !sameFilePath(target, upperTool) {
		t.Errorf("Lookup(npm) resolved %q, want %q", target, upperTool)
	}
}

// G5：未管理工具 + 大写 selfName → PATH 透传仍然可达（Windows-gated）。
// Spec 038 透传修复（bf69b85）对大写别名失效的代码级回归：candidatePaths
// 对已带 PATHEXT 扩展名的 alias 不追加候选（shim_fallthrough.go L178），
// 修复前 alias="mytool.EXE" 只有裸名候选 → 永不命中 → 遮蔽系统真实工具。
//
// 夹具必须是**扩展名不同**的真实工具（.cmd）：同名同扩展名（mytool.EXE vs
// mytool.exe）会被大小写不敏感的 os.Stat 裸名候选直接救活，测不出缺陷——
// 生产事故形态正是 npm.EXE（PATHEXT 大写构造）vs 磁盘 npm.cmd（Node 生态
// 的 .cmd shim；run 经 CreateProcess→cmd.exe 启动，先例见 FindInDir 注释）。
func TestMaybeExec_PassthroughUppercaseSelfName(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("positive dispatch exec-replaces the test process on unix")
	}

	home := t.TempDir() // 空 home：Lookup 必然未命中 → 走透传分支
	t.Setenv("XMDEV_HOME", home)
	config.ResetHomeDirForTest()
	InvalidateShimIndex()

	realDir := t.TempDir()
	realTool := filepath.Join(realDir, "mytool.cmd")
	if err := os.WriteFile(realTool, []byte("@echo off\r\nexit /b 0\r\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", realDir)

	origArgs := os.Args
	defer func() { os.Args = origArgs }()
	os.Args = []string{origArgs[0], "-test.run=^$"}

	handled, err := MaybeExec("mytool.EXE")
	if !handled {
		t.Fatal("handled = false, want true (shim path taken)")
	}
	if err != nil {
		t.Errorf("uppercase selfName must reach the PATH passthrough and dispatch the real tool: %v", err)
	}
}

// G6（Unix 回归门，Linux CI 运行）：别名大小写必须精确保留——折叠是
// Windows 专属语义，Unix 上 "FAKEPY" 与 "fakepy" 是两个不同的工具名。
func TestLookup_PreservesCaseOnUnix(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix exact-case regression gate")
	}

	home := t.TempDir()
	curDir := filepath.Join(home, "current", "fakepy")
	if err := os.MkdirAll(curDir, 0o755); err != nil {
		t.Fatal(err)
	}
	fake := filepath.Join(curDir, "fakepy")
	if err := os.WriteFile(fake, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	t.Setenv("XMDEV_HOME", home)
	config.ResetHomeDirForTest()
	InvalidateShimIndex()

	if _, _, _, err := Lookup("fakepy", nil); err != nil {
		t.Fatalf("baseline exact-case lookup failed: %v", err)
	}
	if _, _, _, err := Lookup("FAKEPY", nil); err == nil {
		t.Error("Unix aliases are case-sensitive: FAKEPY must not resolve to fakepy")
	}
}

// mapKeys 返回索引键快照（失败信息用）。
func mapKeys(m map[string]shimEntry) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// G2a：trimExeSuffix 表测——大小写折叠剥离。Unix 平台 exeSuffix 为空 → 恒等。
func TestTrimExeSuffix(t *testing.T) {
	ext := exeSuffix()
	if ext == "" {
		// Unix：恒等
		for _, in := range []string{"npm", "fakepy", "xdev.exe", ""} {
			if got := trimExeSuffix(in); got != in {
				t.Errorf("trimExeSuffix(%q) = %q on Unix, want identity", in, got)
			}
		}
		return
	}
	// Windows：大小写折叠剥离 ".exe"
	tests := []struct{ in, want string }{
		{"npm.exe", "npm"},
		{"npm.EXE", "npm"},
		{"NPM.EXE", "NPM"},
		{"FakePy.Exe", "FakePy"},
		{"npm", "npm"},
		{"npm.cmd", "npm.cmd"}, // 只剥 .exe——.cmd 由 addExecutables 处理
		{"", ""},               // 空
		{".EXE", ""},           // 只有扩展名 → 剥成空
		{"xdev-1.0.0.exe", "xdev-1.0.0"},
	}
	for _, tt := range tests {
		if got := trimExeSuffix(tt.in); got != tt.want {
			t.Errorf("trimExeSuffix(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// G2b：normalizeShimAlias 表测——Windows 折叠 / Unix 精确。
func TestNormalizeShimAlias(t *testing.T) {
	if os.PathSeparator == '\\' {
		tests := []struct{ in, want string }{
			{"NPM", "npm"}, {"npm", "npm"}, {"FaKePy", "fakepy"},
			{"FakePy.Exe", "fakepy.exe"},
		}
		for _, tt := range tests {
			if got := normalizeShimAlias(tt.in); got != tt.want {
				t.Errorf("normalizeShimAlias(%q) = %q, want %q", tt.in, got, tt.want)
			}
		}
		return
	}
	// Unix：精确保留
	for _, in := range []string{"npm", "Foo", "FAKEPY", "xdev.exe"} {
		if got := normalizeShimAlias(in); got != in {
			t.Errorf("normalizeShimAlias(%q) = %q on Unix, want identity", in, got)
		}
	}
}
