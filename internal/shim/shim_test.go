package shim

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/xmdemt/xdev/internal/config"
)

func sep() string {
	return string(os.PathListSeparator)
}

func TestPathPrependEmpty(t *testing.T) {
	got := PathPrepend("", "/shims")
	if got != "/shims" {
		t.Errorf("expected /shims, got %s", got)
	}
}

func TestPathPrependSingle(t *testing.T) {
	got := PathPrepend("/usr/bin", "/shims")
	want := "/shims" + sep() + "/usr/bin"
	if got != want {
		t.Errorf("expected %s, got %s", want, got)
	}
}

func TestPathPrependAlreadyPresent(t *testing.T) {
	existing := "/shims" + sep() + "/usr/bin"
	got := PathPrepend(existing, "/shims")
	// De-duplication: the second occurrence of /shims should be
	// dropped, so the result equals the input.
	if got != existing {
		t.Errorf("expected no duplication, got %s", got)
	}
}

func TestPathPrependMultiple(t *testing.T) {
	in := "/a" + sep() + "/b" + sep() + "/c"
	got := PathPrepend(in, "/shims")
	want := "/shims" + sep() + "/a" + sep() + "/b" + sep() + "/c"
	if got != want {
		t.Errorf("expected %s, got %s", want, got)
	}
}

// --- FindInDir tests ---

// helper: returns the executable suffix for the current OS
func testExeSuffix() string {
	if runtime.GOOS == "windows" {
		return ".exe"
	}
	return ""
}

func TestFindInDir_BinSubdir(t *testing.T) {
	dir := t.TempDir()
	// Create bin/node.exe (or bin/node on Unix)
	binDir := filepath.Join(dir, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	binFile := filepath.Join(binDir, "node"+testExeSuffix())
	if err := os.WriteFile(binFile, []byte("#!/bin/sh"), 0o755); err != nil {
		t.Fatal(err)
	}

	got, ok := FindInDir(dir, "node")
	if !ok {
		t.Fatal("expected to find node in bin/ subdirectory")
	}
	want := filepath.Join(dir, "bin", "node"+testExeSuffix())
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestFindInDir_RootDir(t *testing.T) {
	dir := t.TempDir()
	// Create node.exe directly in the tool root (no bin/ subdirectory)
	rootFile := filepath.Join(dir, "node"+testExeSuffix())
	if err := os.WriteFile(rootFile, []byte("#!/bin/sh"), 0o755); err != nil {
		t.Fatal(err)
	}

	got, ok := FindInDir(dir, "node")
	if !ok {
		t.Fatal("expected to find node in root directory")
	}
	want := filepath.Join(dir, "node"+testExeSuffix())
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestFindInDir_ExeSuffix(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows-specific test")
	}
	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	exeFile := filepath.Join(binDir, "java.exe")
	if err := os.WriteFile(exeFile, []byte("fake"), 0o755); err != nil {
		t.Fatal(err)
	}

	got, ok := FindInDir(dir, "java")
	if !ok {
		t.Fatal("expected to find java.exe")
	}
	if got != exeFile {
		t.Errorf("got %q, want %q", got, exeFile)
	}
}

func TestFindInDir_CmdSuffix(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows-specific test")
	}
	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cmdFile := filepath.Join(binDir, "npm.cmd")
	if err := os.WriteFile(cmdFile, []byte("@echo off"), 0o755); err != nil {
		t.Fatal(err)
	}

	got, ok := FindInDir(dir, "npm")
	if !ok {
		t.Fatal("expected to find npm.cmd")
	}
	if got != cmdFile {
		t.Errorf("got %q, want %q", got, cmdFile)
	}
}

func TestFindInDir_NotFound(t *testing.T) {
	dir := t.TempDir()
	_, ok := FindInDir(dir, "nonexistent")
	if ok {
		t.Error("expected false for non-existent tool")
	}
}

func TestFindInDir_Directory(t *testing.T) {
	dir := t.TempDir()
	// Create a directory named "node" – should NOT match as an executable
	nodeDir := filepath.Join(dir, "node")
	if err := os.MkdirAll(nodeDir, 0o755); err != nil {
		t.Fatal(err)
	}

	_, ok := FindInDir(dir, "node")
	if ok {
		t.Error("directory with the same name should not match as an executable")
	}
}

func TestFindInDir_NestedSubdir(t *testing.T) {
	dir := t.TempDir()
	// Simulate strip:0 layout: dir/<toolname>/bin/<alias>
	// e.g. cangjie/bin/cjc.exe or go/bin/go.exe
	nestedBin := filepath.Join(dir, "cangjie", "bin")
	if err := os.MkdirAll(nestedBin, 0o755); err != nil {
		t.Fatal(err)
	}
	exeFile := filepath.Join(nestedBin, "cjc"+testExeSuffix())
	if err := os.WriteFile(exeFile, []byte("#!/bin/sh"), 0o755); err != nil {
		t.Fatal(err)
	}

	got, ok := FindInDir(dir, "cjc")
	if !ok {
		t.Fatal("expected to find cjc in nested subdirectory cangjie/bin/")
	}
	if got != exeFile {
		t.Errorf("got %q, want %q", got, exeFile)
	}
}

func TestFindInDir_NestedSubdirRoot(t *testing.T) {
	dir := t.TempDir()
	// Simulate strip:0 layout with executable at dir/<toolname>/<alias> (no bin/)
	nestedDir := filepath.Join(dir, "some-tool")
	if err := os.MkdirAll(nestedDir, 0o755); err != nil {
		t.Fatal(err)
	}
	exeFile := filepath.Join(nestedDir, "mytool"+testExeSuffix())
	if err := os.WriteFile(exeFile, []byte("#!/bin/sh"), 0o755); err != nil {
		t.Fatal(err)
	}

	got, ok := FindInDir(dir, "mytool")
	if !ok {
		t.Fatal("expected to find mytool in nested subdirectory root")
	}
	if got != exeFile {
		t.Errorf("got %q, want %q", got, exeFile)
	}
}

// --- Lookup tests ---

func TestLookup_DirectTool(t *testing.T) {
	home := t.TempDir()
	// Set up: current/node/bin/node.exe
	curDir := filepath.Join(home, "current", "node", "bin")
	if err := os.MkdirAll(curDir, 0o755); err != nil {
		t.Fatal(err)
	}
	nodeExe := filepath.Join(curDir, "node"+testExeSuffix())
	if err := os.WriteFile(nodeExe, []byte("#!/bin/sh"), 0o755); err != nil {
		t.Fatal(err)
	}

	t.Setenv("XMDEV_HOME", home)
	config.ResetHomeDirForTest()
	InvalidateShimIndex()

	target, toolName, args, err := Lookup("node", nil)
	if err != nil {
		t.Fatalf("Lookup error: %v", err)
	}
	if toolName != "node" {
		t.Errorf("toolName = %q, want %q", toolName, "node")
	}
	if target != nodeExe {
		t.Errorf("target = %q, want %q", target, nodeExe)
	}
	if len(args) != 0 {
		t.Errorf("args = %v, want empty", args)
	}
}

func TestDiskShimIndex_RoundTripAndLookup(t *testing.T) {
	home := t.TempDir()
	curDir := filepath.Join(home, "current", "node", "bin")
	if err := os.MkdirAll(curDir, 0o755); err != nil {
		t.Fatal(err)
	}
	nodeExe := filepath.Join(curDir, "node"+testExeSuffix())
	npmName := "npm"
	if runtime.GOOS == "windows" {
		npmName = "npm.cmd"
	}
	npmExe := filepath.Join(curDir, npmName)
	if err := os.WriteFile(nodeExe, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(npmExe, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XMDEV_HOME", home)
	config.ResetHomeDirForTest()

	// InvalidateShimIndex now also writes the on-disk index.
	InvalidateShimIndex()

	// Clear the in-process index so Lookup must use the disk file.
	shimIndexMu.Lock()
	shimIndex = nil
	shimIndexOnce = sync.Once{}
	shimIndexMu.Unlock()

	target, toolName, _, err := Lookup("npm", nil)
	if err != nil {
		t.Fatalf("Lookup via disk index: %v", err)
	}
	if toolName != "node" {
		t.Errorf("toolName = %q, want node", toolName)
	}
	if target != npmExe {
		t.Errorf("target = %q, want %q", target, npmExe)
	}
}

func TestLookup_CrossToolScan(t *testing.T) {
	home := t.TempDir()
	// Set up: current/node/bin/npm.cmd (npm lives inside the node directory)
	curDir := filepath.Join(home, "current", "node", "bin")
	if err := os.MkdirAll(curDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Create node executable so the directory is valid
	nodeExe := filepath.Join(curDir, "node"+testExeSuffix())
	if err := os.WriteFile(nodeExe, []byte("#!/bin/sh"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Create npm as a .cmd file (Windows) or plain file (Unix)
	npmName := "npm"
	if runtime.GOOS == "windows" {
		npmName = "npm.cmd"
	}
	npmFile := filepath.Join(curDir, npmName)
	if err := os.WriteFile(npmFile, []byte("#!/bin/sh"), 0o755); err != nil {
		t.Fatal(err)
	}

	t.Setenv("XMDEV_HOME", home)
	config.ResetHomeDirForTest()
	InvalidateShimIndex()

	target, toolName, _, err := Lookup("npm", nil)
	if err != nil {
		t.Fatalf("Lookup error: %v", err)
	}
	if toolName != "node" {
		t.Errorf("toolName = %q, want %q (cross-tool scan should find npm under node)", toolName, "node")
	}
	if target != npmFile {
		t.Errorf("target = %q, want %q", target, npmFile)
	}
}

func TestLookup_NotInstalled(t *testing.T) {
	home := t.TempDir()
	t.Setenv("XMDEV_HOME", home)
	config.ResetHomeDirForTest()
	InvalidateShimIndex()

	_, _, _, err := Lookup("nonexistent-tool", nil)
	if err == nil {
		t.Error("expected error for uninstalled tool")
	}
	if !strings.Contains(err.Error(), "not installed") {
		t.Errorf("error = %q, want 'not installed' message", err.Error())
	}
}

// --- MaybeExec tests ---

func TestMaybeExec_XdevName(t *testing.T) {
	didExec, err := MaybeExec("xdev")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if didExec {
		t.Error("xdev name should not trigger shim dispatch")
	}
}

func TestMaybeExec_XdevExeName(t *testing.T) {
	name := "xdev"
	if runtime.GOOS == "windows" {
		name = "xdev.exe"
	}
	didExec, err := MaybeExec(name)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if didExec {
		t.Error("xdev.exe name should not trigger shim dispatch")
	}
}

func TestMaybeExec_ShimName(t *testing.T) {
	// When invoked as "node", MaybeExec should attempt to look up the
	// shim.  Since no XMDEV_HOME is configured with a real node binary,
	// it will return (true, error).  The key assertion is that didExec
	// is true, meaning the shim path was taken.
	home := t.TempDir()
	t.Setenv("XMDEV_HOME", home)
	config.ResetHomeDirForTest()
	InvalidateShimIndex()

	didExec, _ := MaybeExec("node")
	if !didExec {
		t.Error("non-xdev name should trigger shim dispatch (didExec = true)")
	}
}

// TestInstallShim_RejectsInvalidNames verifies InstallShim refuses names
// that could escape the shims directory or are otherwise unsafe (F-08 /
// D11) — validation happens before any filesystem access.
func TestInstallShim_RejectsInvalidNames(t *testing.T) {
	tmp := t.TempDir()
	shimDir := filepath.Join(tmp, "shims")
	// selfPath 不需要存在：无效名称在触碰文件系统前就被拒绝。
	selfPath := filepath.Join(tmp, "xdev")

	badNames := []string{"", ".", "..", "../outside", `..\outside`, "a/b", "a\\b", "C:evil", "CON", "bad\x00name"}
	for _, name := range badNames {
		if err := InstallShim(shimDir, name, selfPath); err == nil {
			t.Errorf("InstallShim(%q) should be rejected", name)
		}
	}

	// 拒绝后 shim 目录不应被创建（验证先于 MkdirAll/写入）。
	if _, err := os.Stat(shimDir); err == nil {
		t.Error("shim dir should not be created for invalid names")
	}
}

// TestInstallShim_AcceptsValidName verifies a valid shim name still works.
func TestInstallShim_AcceptsValidName(t *testing.T) {
	tmp := t.TempDir()
	shimDir := filepath.Join(tmp, "shims")
	selfPath := filepath.Join(tmp, "xdev")
	if err := os.WriteFile(selfPath, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := InstallShim(shimDir, "validtool", selfPath); err != nil {
		t.Fatalf("InstallShim(validtool) failed: %v", err)
	}
	// Unix: symlink 或文件；Windows: 副本 + .exe 副本。
	base := filepath.Join(shimDir, "validtool")
	if _, err := os.Lstat(base); err != nil {
		t.Errorf("shim %s missing: %v", base, err)
	}
	if runtime.GOOS == "windows" {
		if _, err := os.Stat(base + ".exe"); err != nil {
			t.Errorf("shim %s.exe missing: %v", base, err)
		}
	}
}

func TestInstallShimWindows_HardLinkWhenPossible(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows hard-link shim")
	}
	tmp := t.TempDir()
	src := filepath.Join(tmp, "xdev.exe")
	dst := filepath.Join(tmp, "go.exe")
	if err := os.WriteFile(src, []byte("shim-payload-0123456789"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := installShimWindows(src, dst); err != nil {
		t.Fatalf("installShimWindows: %v", err)
	}
	si, err := os.Stat(src)
	if err != nil {
		t.Fatal(err)
	}
	di, err := os.Stat(dst)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(si, di) {
		t.Fatal("expected hard link (SameFile) when src and dst share a volume")
	}
	if err := installShimWindows(src, dst); err != nil {
		t.Fatalf("second installShimWindows (already linked): %v", err)
	}
}
