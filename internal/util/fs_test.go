package util

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestAtomicWriteFile(t *testing.T) {
	dir := t.TempDir()
	if err := AtomicWriteFile(filepath.Join(dir, "test.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatalf("AtomicWriteFile failed: %v", err)
	}
}

func TestAtomicWriteFileOverwrites(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")
	if err := AtomicWriteFile(path, []byte("first"), 0o644); err != nil {
		t.Fatalf("first write: %v", err)
	}
	if err := AtomicWriteFile(path, []byte("second"), 0o644); err != nil {
		t.Fatalf("second write: %v", err)
	}

	got, _ := os.ReadFile(path)
	if string(got) != "second" {
		t.Errorf("content = %q, want %q", string(got), "second")
	}
}

func TestActiveVersionNotFound(t *testing.T) {
	dir := t.TempDir()
	ver, ok := ActiveVersion(dir, "nonexistent-tool")
	if ok {
		t.Errorf("ActiveVersion() = (%q, true), want ('', false)", ver)
	}
}

func TestActiveVersionFound(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink requires privileges on Windows")
	}
	dir := t.TempDir()
	installPath := filepath.Join(dir, "install", "myapp", "1.0.0")
	if err := os.MkdirAll(installPath, 0o755); err != nil {
		t.Fatal(err)
	}
	currentDir := filepath.Join(dir, "current")
	if err := os.MkdirAll(currentDir, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(currentDir, "myapp")
	if err := os.Symlink(installPath, link); err != nil {
		t.Skipf("symlink failed: %v", err)
	}

	ver, ok := ActiveVersion(dir, "myapp")
	if !ok {
		t.Fatal("ActiveVersion() = ('', false), want (ver, true)")
	}
	if ver != "1.0.0" {
		t.Errorf("ActiveVersion() = %q, want %q", ver, "1.0.0")
	}
}

func TestActiveInstallPath(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink requires privileges on Windows")
	}
	dir := t.TempDir()
	installPath := filepath.Join(dir, "install", "tool", "2.0.0")
	if err := os.MkdirAll(installPath, 0o755); err != nil {
		t.Fatal(err)
	}
	currentDir := filepath.Join(dir, "current")
	if err := os.MkdirAll(currentDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(installPath, filepath.Join(currentDir, "tool")); err != nil {
		t.Skipf("symlink failed: %v", err)
	}

	got := ActiveInstallPath(dir, "tool")
	if got != installPath {
		t.Errorf("ActiveInstallPath() = %q, want %q", got, installPath)
	}
}

func TestActiveInstallPathNotFound(t *testing.T) {
	dir := t.TempDir()
	if got := ActiveInstallPath(dir, "ghost"); got != "" {
		t.Errorf("ActiveInstallPath() = %q, want ''", got)
	}
}

func TestCopyDirFollowsSymlinkedDirectory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink/junction creation is environment-dependent on Windows")
	}
	src := t.TempDir()
	realApps := filepath.Join(src, "apps-real")
	if err := os.MkdirAll(filepath.Join(realApps, "scoop"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(realApps, "scoop", "scoop.ps1"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(src, "apps"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(realApps, filepath.Join(src, "apps", "current")); err != nil {
		t.Skipf("symlink failed: %v", err)
	}
	dst := filepath.Join(t.TempDir(), "copy")
	if err := CopyDir(src, dst); err != nil {
		t.Fatalf("CopyDir: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(dst, "apps", "current", "scoop", "scoop.ps1"))
	if err != nil {
		t.Fatalf("copied junction target missing: %v", err)
	}
	if string(got) != "hi" {
		t.Errorf("copied content = %q, want hi", got)
	}
}
