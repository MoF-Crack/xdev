package extractor

import (
	"archive/zip"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// TestExtractZip_RestoresExecutableBit covers L7: zips created on Windows/FAT
// carry no POSIX mode (Go reports 0666), so extracted binaries landed
// non-executable on Linux and every shim exec failed with EACCES.
func TestExtractZip_RestoresExecutableBit(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits")
	}
	dir := t.TempDir()
	zipPath := filepath.Join(dir, "tool.zip")
	f, err := os.Create(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	add := func(name string) {
		h := &zip.FileHeader{Name: name, Method: zip.Deflate}
		h.SetMode(0o666) // FAT-style: no execute bit
		w, err := zw.CreateHeader(h)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write([]byte("payload"))
	}
	add("tool/bin/tool")
	add("tool/README.md")
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	dst := filepath.Join(dir, "out")
	if err := Extract(zipPath, dst, "zip", 0); err != nil {
		t.Fatalf("Extract: %v", err)
	}
	binInfo, err := os.Stat(filepath.Join(dst, "tool", "bin", "tool"))
	if err != nil {
		t.Fatal(err)
	}
	if binInfo.Mode().Perm()&0o111 == 0 {
		t.Errorf("bin/tool mode = %v, want an execute bit", binInfo.Mode().Perm())
	}
	readmeInfo, err := os.Stat(filepath.Join(dst, "tool", "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	if readmeInfo.Mode().Perm()&0o111 != 0 {
		t.Errorf("README.md mode = %v, must stay non-executable", readmeInfo.Mode().Perm())
	}
}

// TestExtractAppImage: AppImage is a single executable image, not an archive.
func TestExtractAppImage(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("AppImage is a Linux format")
	}
	dir := t.TempDir()
	src := filepath.Join(dir, "Cursor-3.14.7-x86_64.AppImage")
	if err := os.WriteFile(src, []byte("appimage-payload"), 0o644); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(dir, "out")
	if err := Extract(src, dst, "appimage", 0); err != nil {
		t.Fatalf("Extract appimage: %v", err)
	}
	got := filepath.Join(dst, filepath.Base(src))
	fi, err := os.Stat(got)
	if err != nil {
		t.Fatalf("AppImage not copied: %v", err)
	}
	if fi.Mode().Perm()&0o111 == 0 {
		t.Errorf("mode = %v, want executable", fi.Mode().Perm())
	}
}
