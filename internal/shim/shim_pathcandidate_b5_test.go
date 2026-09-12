// shim_pathcandidate_b5_test.go — 审计修复 Batch 5（票 39 / P2-09）守卫：
//
//	lookupOnPath 的候选校验：Unix 用 os.Stat（跟随符号链接）并校验执行位；
//	Windows 按 PATHEXT 展开，裸名末位兜底。
//
// 平台差异说明：Windows 没有执行位（os.Chmod 只影响只读属性），本机
// （Windows）无法构造"有执行位/无执行位"的文件，因此执行位的端到端用例
// 在非 Windows 上运行（CI/ubuntu），本机用平台无关的 hasExecBit 纯函数
// 用例覆盖判定语义；PATHEXT 顺序与裸名兜底用例则在 Windows 上端到端覆盖。
package shim

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestHasExecBit_UnixSemantics：执行位判定语义（平台无关）。
func TestHasExecBit_UnixSemantics(t *testing.T) {
	cases := []struct {
		mode os.FileMode
		want bool
	}{
		{0o755, true},
		{0o700, true},
		{0o111, true},
		{0o644, false},
		{0o600, false},
		{0o666, false},
		{0o000, false},
		{0o755 | os.ModeSymlink, true}, // 跟随符号链接后的目标模式
		{os.ModeDir | 0o755, true},     // IsDir 由调用方另行判掉
	}
	for _, c := range cases {
		if got := hasExecBit(c.mode); got != c.want {
			t.Errorf("hasExecBit(%v) = %v, want %v", c.mode, got, c.want)
		}
	}
}

// TestCandidatePaths_WindowsPathExtOrder：Windows 候选集合与顺序——
// PATHEXT 展开在前、裸名**末位**兜底；alias 已带扩展名时不再追加。
func TestCandidatePaths_WindowsPathExtOrder(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("PATHEXT 展开只存在于 Windows 语义")
	}
	t.Setenv("PATHEXT", ".EXE;.CMD")

	dir := `C:\fakebin`
	got := candidatePaths(dir, "git")
	want := []string{
		filepath.Join(dir, "git.exe"),
		filepath.Join(dir, "git.cmd"),
		filepath.Join(dir, "git"), // 裸名必须是最后一项
	}
	if len(got) != len(want) {
		t.Fatalf("candidatePaths(%q) = %v, want %v", dir, got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("candidatePaths[%d] = %q, want %q (PATHEXT 顺序 + 裸名末位)", i, got[i], want[i])
		}
	}

	// 已带 PATHEXT 扩展名 → 只保留自身（不产出 git.exe.exe）。
	if got := candidatePaths(dir, "git.exe"); len(got) != 1 || got[0] != filepath.Join(dir, "git.exe") {
		t.Errorf("candidatePaths(git.exe) = %v, want exactly [git.exe]", got)
	}

	// PATHEXT 缺失/为空 → Windows 默认扩展名集合。
	t.Setenv("PATHEXT", "")
	gotDefault := candidatePaths(dir, "node")
	if len(gotDefault) != 5 || !strings.HasSuffix(gotDefault[0], ".com") || gotDefault[4] != filepath.Join(dir, "node") {
		t.Errorf("candidatePaths with empty PATHEXT = %v, want .com/.exe/.bat/.cmd + bare name last", gotDefault)
	}
}

// TestLookupOnPath_PrefersPathExtOverBareName（P2-09 主判据，Windows 端到端）：
// 同一目录里既有无扩展名的同名文件又有 git.exe 时，必须命中 git.exe——
// 修复前裸名列在首位，返回的是那个无扩展名文件（cmd/CreateProcess 根本不
// 会执行它，透传结果等于把错误路径交给调用方 exec）。
func TestLookupOnPath_PrefersPathExtOverBareName(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("裸名与 .exe 同名竞争只存在于 Windows（PATHEXT）语义")
	}
	t.Setenv("PATHEXT", ".EXE;.CMD")

	dir := t.TempDir()
	bare := filepath.Join(dir, "git") // 无扩展名副本（历史布局残留）
	if err := os.WriteFile(bare, []byte("BARE"), 0o644); err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(dir, "git.exe")
	if err := os.WriteFile(exe, []byte("EXE"), 0o755); err != nil {
		t.Fatal(err)
	}

	got, err := lookupOnPath("git", dir, excludedFrom(t.TempDir()))
	if err != nil {
		t.Fatalf("lookupOnPath: %v", err)
	}
	if !sameFilePath(got, exe) {
		t.Errorf("resolved %q, want %q — PATHEXT 候选必须优先于裸名 (P2-09)", got, exe)
	}
}

// TestLookupOnPath_BareNameStillResolves（P2-09 末位兜底回归）：目录里只有
// 无扩展名的可执行文件时，裸名兜底必须继续命中——不能因为 PATHEXT 展开
// 而丢掉这条路径。
func TestLookupOnPath_BareNameStillResolves(t *testing.T) {
	dir := t.TempDir()
	bare := filepath.Join(dir, "onlybare")
	if err := os.WriteFile(bare, []byte("BARE"), 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := lookupOnPath("onlybare", dir, excludedFrom(t.TempDir()))
	if err != nil {
		t.Fatalf("lookupOnPath: %v", err)
	}
	if got == "" {
		t.Fatal("bare-name candidate must remain as the last-resort fallback (P2-09)")
	}
	if !sameFilePath(got, bare) {
		t.Errorf("resolved %q, want %q", got, bare)
	}
}

// TestLookupOnPath_UnixRequiresExecBit（P2-09 执行位端到端，Unix）：
// PATH 里先出现一个不可执行的同名文件时，必须跳过它继续找后面真正可执行的
// 工具；修复前只判 !IsDir()，返回的是那个不可执行文件。
func TestLookupOnPath_UnixRequiresExecBit(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows 无执行位概念，判定为 executableCandidate 的平台分支；见 TestHasExecBit_UnixSemantics")
	}
	dataDir := t.TempDir()
	binDir := t.TempDir()

	notExec := filepath.Join(dataDir, "mytool")
	if err := os.WriteFile(notExec, []byte("not a program\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	real := filepath.Join(binDir, "mytool")
	if err := os.WriteFile(real, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	pathValue := dataDir + string(os.PathListSeparator) + binDir
	got, err := lookupOnPath("mytool", pathValue, excludedFrom(t.TempDir()))
	if err != nil {
		t.Fatalf("lookupOnPath: %v", err)
	}
	if got == "" || !sameFilePath(got, real) {
		t.Fatalf("resolved %q, want the executable %q (non-executable candidate must be skipped)", got, real)
	}
}

// TestLookupOnPath_UnixFollowsSymlink（P2-09 os.Stat 语义，Unix）：
// 候选是符号链接、且目标可执行时必须命中（LookPath 跟随链接；若用 Lstat
// 会因链接自身没有执行位而漏掉 /usr/local/bin 这类常见布局）。
func TestLookupOnPath_UnixFollowsSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("文件符号链接在 Windows 需要特权，Unix 语义在 CI 覆盖")
	}
	realDir := t.TempDir()
	linkDir := t.TempDir()

	target := filepath.Join(realDir, "node-v20")
	if err := os.WriteFile(target, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(linkDir, "node")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("os.Symlink unavailable: %v", err)
	}

	got, err := lookupOnPath("node", linkDir, excludedFrom(t.TempDir()))
	if err != nil {
		t.Fatalf("lookupOnPath: %v", err)
	}
	if got == "" || !sameFilePath(got, link) {
		t.Fatalf("resolved %q, want the symlink %q (os.Stat must follow links)", got, link)
	}
}
