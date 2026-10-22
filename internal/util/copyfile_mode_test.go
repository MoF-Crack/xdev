package util

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// Ticket 38 (P2-32): CopyFile must take permission bits from the SOURCE
// (forcing 0755 turned copied data files executable) and must not
// re-permission an existing destination.
func TestCopyFile_PreservesSourceMode(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.dat")
	dst := filepath.Join(dir, "dst.dat")

	if err := os.WriteFile(src, []byte("payload"), 0o644); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		if err := os.Chmod(src, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	if err := CopyFile(src, dst); err != nil {
		t.Fatalf("CopyFile: %v", err)
	}
	got, err := os.ReadFile(dst)
	if err != nil || string(got) != "payload" {
		t.Fatalf("copied content = %q err=%v", got, err)
	}
	if runtime.GOOS == "windows" {
		// Windows has no POSIX permission bits — CreateFile ignores the perm
		// argument; the setuid/mode assertions are meaningful on Unix (CI).
		t.Logf("windows: dst mode = %v (POSIX bits not applicable)", mustStat(t, dst).Mode())
		return
	}
	if mode := mustStat(t, dst).Mode().Perm(); mode != 0o644 {
		t.Errorf("dst mode = %v, want 0644 (source's bits, not the old hard-coded 0755)", mode)
	}

	// An existing destination keeps its own mode.
	if err := os.Chmod(dst, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := CopyFile(src, dst); err != nil {
		t.Fatalf("second CopyFile: %v", err)
	}
	if mode := mustStat(t, dst).Mode().Perm(); mode != 0o600 {
		t.Errorf("dst mode = %v after overwrite, want the existing 0600 preserved", mode)
	}
}

func mustStat(t *testing.T, path string) os.FileInfo {
	t.Helper()
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	return fi
}
