package shim

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
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
