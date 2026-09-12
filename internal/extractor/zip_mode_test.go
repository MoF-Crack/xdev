package extractor

import (
	"archive/zip"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// I2 (ticket 26 / A-08): zip entries carrying setuid/setgid/sticky bits must
// land with plain permission bits only. The pre-fix code passed the raw
// zip.FileMode to os.OpenFile, so a hostile archive could plant a
// setuid-privileged file inside the install tree.
func TestUnzipFile_StripsSetuid(t *testing.T) {
	dir := t.TempDir()
	zipPath := filepath.Join(dir, "evil.zip")
	f, err := os.Create(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	w := zip.NewWriter(f)
	hdr := &zip.FileHeader{Name: "evil"}
	// 0o755 plus setuid/setgid/sticky.
	hdr.SetMode(os.FileMode(0o755) | os.ModeSetuid | os.ModeSetgid | os.ModeSticky)
	entry, err := w.CreateHeader(hdr)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := entry.Write([]byte("payload")); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	dst := filepath.Join(dir, "out")
	if err := Extract(zipPath, dst, "zip", 0); err != nil {
		t.Fatalf("Extract: %v", err)
	}

	fi, err := os.Stat(filepath.Join(dst, "evil"))
	if err != nil {
		t.Fatalf("extracted file missing: %v", err)
	}
	mode := fi.Mode()

	// Windows has no POSIX permission bits: CreateFile ignores the perm
	// argument entirely (the extracted file lands with the default mode), so
	// the setuid/permission assertions below are meaningful only on Unix —
	// where CI runs them. On Windows this test still guards that a hostile
	// mode does not break extraction.
	if runtime.GOOS == "windows" {
		t.Logf("windows: extracted mode = %v (POSIX bits not applicable); content-level checks only", mode)
		got, readErr := os.ReadFile(filepath.Join(dst, "evil"))
		if readErr != nil || string(got) != "payload" {
			t.Errorf("extracted content = %q err=%v, want payload", got, readErr)
		}
		return
	}
	if mode&os.ModeSetuid != 0 {
		t.Error("extracted file kept the setuid bit")
	}
	if mode&os.ModeSetgid != 0 {
		t.Error("extracted file kept the setgid bit")
	}
	if mode&os.ModeSticky != 0 {
		t.Error("extracted file kept the sticky bit")
	}
	if mode.Perm() != 0o755 {
		t.Errorf("permission bits = %v, want 0755 (Mode().Perm())", mode.Perm())
	}
}
