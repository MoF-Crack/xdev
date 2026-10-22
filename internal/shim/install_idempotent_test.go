package shim

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// 20261022 CI（Ubuntu/macOS）：并发激活同一个版本时，第二个 InstallShim
// 直接 os.Symlink → EEXIST。Windows 分支早已幂等，Unix 分支必须补齐。
func TestInstallShim_Unix_IsIdempotentAndReplacesStaleLink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix symlink semantics")
	}
	shimDir := t.TempDir()
	self1 := filepath.Join(t.TempDir(), "xdev-1")
	self2 := filepath.Join(t.TempDir(), "xdev-2")
	for p, body := range map[string]string{self1: "one", self2: "two"} {
		if err := os.WriteFile(p, []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	if err := InstallShim(shimDir, "demo", self1); err != nil {
		t.Fatalf("first InstallShim: %v", err)
	}
	if err := InstallShim(shimDir, "demo", self1); err != nil {
		t.Fatalf("second InstallShim must be a no-op, got: %v", err)
	}
	if cur, err := os.Readlink(filepath.Join(shimDir, "demo")); err != nil || cur != self1 {
		t.Fatalf("link = %q (err=%v), want %q", cur, err, self1)
	}

	// 指向旧源时必须原子替换，而不是 EEXIST。
	if err := InstallShim(shimDir, "demo", self2); err != nil {
		t.Fatalf("stale link replacement: %v", err)
	}
	if cur, _ := os.Readlink(filepath.Join(shimDir, "demo")); cur != self2 {
		t.Errorf("link = %q, want %q", cur, self2)
	}
}

// 普通文件不能被 shim 安装覆盖（避免吞用户文件）。
func TestInstallShim_Unix_RefusesRegularFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix symlink semantics")
	}
	shimDir := t.TempDir()
	self := filepath.Join(t.TempDir(), "xdev")
	if err := os.WriteFile(self, []byte("xdev"), 0o755); err != nil {
		t.Fatal(err)
	}
	victim := filepath.Join(shimDir, "demo")
	if err := os.WriteFile(victim, []byte("user-file"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := InstallShim(shimDir, "demo", self); err == nil {
		t.Fatal("InstallShim must refuse to overwrite a regular file")
	}
	if got, _ := os.ReadFile(victim); string(got) != "user-file" {
		t.Errorf("regular file was modified: %q", got)
	}
}
