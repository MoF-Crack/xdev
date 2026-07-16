package extractor

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// createTestZip creates a zip file at dstPath with the given entries.
// Each entry is "path|content" — if content is empty it's treated as a directory.
func createTestZip(t *testing.T, dstPath string, entries []string) {
	t.Helper()
	f, err := os.Create(dstPath)
	if err != nil {
		t.Fatalf("create zip: %v", err)
	}
	defer f.Close()

	w := zip.NewWriter(f)
	for _, e := range entries {
		parts := strings.SplitN(e, "|", 2)
		name := parts[0]
		content := ""
		if len(parts) == 2 {
			content = parts[1]
		}

		if strings.HasSuffix(name, "/") || content == "" && strings.Contains(name, "/") && !strings.Contains(name[len(strings.TrimRight(name, "/"))-1:], ".") {
			// directory entry
			if !strings.HasSuffix(name, "/") {
				name += "/"
			}
			if _, err := w.Create(name); err != nil {
				t.Fatalf("zip create dir %s: %v", name, err)
			}
			continue
		}

		out, err := w.Create(name)
		if err != nil {
			t.Fatalf("zip create %s: %v", name, err)
		}
		if _, err := out.Write([]byte(content)); err != nil {
			t.Fatalf("zip write %s: %v", name, err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}
}
// createTestTarGz creates a tar.gz file at dstPath with the given entries.
// Each entry is "path|content" — if content is empty and path ends with "/", it's a directory.
func createTestTarGz(t *testing.T, dstPath string, entries []string) {
	t.Helper()
	f, err := os.Create(dstPath)
	if err != nil {
		t.Fatalf("create tar.gz: %v", err)
	}
	defer f.Close()

	gw := gzip.NewWriter(f)
	tw := tar.NewWriter(gw)

	for _, e := range entries {
		parts := strings.SplitN(e, "|", 2)
		name := parts[0]
		content := ""
		if len(parts) == 2 {
			content = parts[1]
		}

		if strings.HasSuffix(name, "/") {
			hdr := &tar.Header{
				Typeflag: tar.TypeDir,
				Name:     name,
				Mode:     0o755,
			}
			if err := tw.WriteHeader(hdr); err != nil {
				t.Fatalf("tar header dir %s: %v", name, err)
			}
			continue
		}

		data := []byte(content)
		hdr := &tar.Header{
			Typeflag: tar.TypeReg,
			Name:     name,
			Size:     int64(len(data)),
			Mode:     0o644,
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatalf("tar header %s: %v", name, err)
		}
		if _, err := tw.Write(data); err != nil {
			t.Fatalf("tar write %s: %v", name, err)
		}
	}

	if err := tw.Close(); err != nil {
		t.Fatalf("tar close: %v", err)
	}
	if err := gw.Close(); err != nil {
		t.Fatalf("gzip close: %v", err)
	}
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

// ---------- Tests ----------

func TestExtract_Zip(t *testing.T) {
	tmpDir := t.TempDir()
	zipPath := filepath.Join(tmpDir, "test.zip")
	dstDir := filepath.Join(tmpDir, "out")

	createTestZip(t, zipPath, []string{
		"topdir/",
		"topdir/hello.txt|hello world",
		"topdir/sub/",
		"topdir/sub/nested.txt|nested content",
	})

	if err := Extract(zipPath, dstDir, "zip", 0); err != nil {
		t.Fatalf("Extract zip: %v", err)
	}

	if !fileExists(filepath.Join(dstDir, "topdir", "hello.txt")) {
		t.Error("expected topdir/hello.txt to exist")
	}
	if readFile(t, filepath.Join(dstDir, "topdir", "hello.txt")) != "hello world" {
		t.Error("hello.txt content mismatch")
	}
	if !fileExists(filepath.Join(dstDir, "topdir", "sub", "nested.txt")) {
		t.Error("expected topdir/sub/nested.txt to exist")
	}
	if readFile(t, filepath.Join(dstDir, "topdir", "sub", "nested.txt")) != "nested content" {
		t.Error("nested.txt content mismatch")
	}
}

func TestExtract_TarGz(t *testing.T) {
	tmpDir := t.TempDir()
	tgzPath := filepath.Join(tmpDir, "test.tar.gz")
	dstDir := filepath.Join(tmpDir, "out")

	createTestTarGz(t, tgzPath, []string{
		"mydir/",
		"mydir/alpha.txt|alpha",
		"mydir/inner/",
		"mydir/inner/beta.txt|beta",
	})

	if err := Extract(tgzPath, dstDir, "tar.gz", 0); err != nil {
		t.Fatalf("Extract tar.gz: %v", err)
	}

	if !fileExists(filepath.Join(dstDir, "mydir", "alpha.txt")) {
		t.Error("expected mydir/alpha.txt to exist")
	}
	if readFile(t, filepath.Join(dstDir, "mydir", "alpha.txt")) != "alpha" {
		t.Error("alpha.txt content mismatch")
	}
	if !fileExists(filepath.Join(dstDir, "mydir", "inner", "beta.txt")) {
		t.Error("expected mydir/inner/beta.txt to exist")
	}
	if readFile(t, filepath.Join(dstDir, "mydir", "inner", "beta.txt")) != "beta" {
		t.Error("beta.txt content mismatch")
	}
}

func TestExtract_TarGz_AliasTgz(t *testing.T) {
	tmpDir := t.TempDir()
	tgzPath := filepath.Join(tmpDir, "test.tgz")
	dstDir := filepath.Join(tmpDir, "out")

	createTestTarGz(t, tgzPath, []string{
		"root.txt|rootfile",
	})

	if err := Extract(tgzPath, dstDir, "tgz", 0); err != nil {
		t.Fatalf("Extract tgz: %v", err)
	}

	if !fileExists(filepath.Join(dstDir, "root.txt")) {
		t.Error("expected root.txt to exist")
	}
	if readFile(t, filepath.Join(dstDir, "root.txt")) != "rootfile" {
		t.Error("root.txt content mismatch")
	}
}

func TestExtract_Strip1(t *testing.T) {
	tmpDir := t.TempDir()
	zipPath := filepath.Join(tmpDir, "strip.zip")
	dstDir := filepath.Join(tmpDir, "out")

	createTestZip(t, zipPath, []string{
		"topdir/",
		"topdir/file.txt|stripped content",
		"topdir/subdir/",
		"topdir/subdir/deep.txt|deep",
	})

	if err := Extract(zipPath, dstDir, "zip", 1); err != nil {
		t.Fatalf("Extract zip strip=1: %v", err)
	}

	// With strip=1, "topdir" is removed, so file.txt should be at the root
	if !fileExists(filepath.Join(dstDir, "file.txt")) {
		t.Error("expected file.txt at root after strip=1")
	}
	if readFile(t, filepath.Join(dstDir, "file.txt")) != "stripped content" {
		t.Error("file.txt content mismatch")
	}
	if !fileExists(filepath.Join(dstDir, "subdir", "deep.txt")) {
		t.Error("expected subdir/deep.txt after strip=1")
	}
	if readFile(t, filepath.Join(dstDir, "subdir", "deep.txt")) != "deep" {
		t.Error("deep.txt content mismatch")
	}

	// The top-level directory should NOT exist
	if fileExists(filepath.Join(dstDir, "topdir")) {
		t.Error("topdir should not exist after strip=1")
	}
}

func TestExtract_Strip1_TarGz(t *testing.T) {
	tmpDir := t.TempDir()
	tgzPath := filepath.Join(tmpDir, "strip.tar.gz")
	dstDir := filepath.Join(tmpDir, "out")

	createTestTarGz(t, tgzPath, []string{
		"pkg-1.0/",
		"pkg-1.0/README.md|readme",
		"pkg-1.0/src/",
		"pkg-1.0/src/main.go|package main",
	})

	if err := Extract(tgzPath, dstDir, "tar.gz", 1); err != nil {
		t.Fatalf("Extract tar.gz strip=1: %v", err)
	}

	if !fileExists(filepath.Join(dstDir, "README.md")) {
		t.Error("expected README.md at root after strip=1")
	}
	if readFile(t, filepath.Join(dstDir, "README.md")) != "readme" {
		t.Error("README.md content mismatch")
	}
	if !fileExists(filepath.Join(dstDir, "src", "main.go")) {
		t.Error("expected src/main.go after strip=1")
	}
	if readFile(t, filepath.Join(dstDir, "src", "main.go")) != "package main" {
		t.Error("main.go content mismatch")
	}
}

func TestExtract_UnsupportedFormat(t *testing.T) {
	tmpDir := t.TempDir()
	dstDir := filepath.Join(tmpDir, "out")

	err := Extract("dummy.src", dstDir, "rar", 0)
	if err == nil {
		t.Fatal("expected error for unsupported format")
	}
	if !strings.Contains(err.Error(), "unsupported format") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestExtract_NonExistentSource(t *testing.T) {
	tmpDir := t.TempDir()
	dstDir := filepath.Join(tmpDir, "out")
	nonExistent := filepath.Join(tmpDir, "no_such_file.zip")

	err := Extract(nonExistent, dstDir, "zip", 0)
	if err == nil {
		t.Fatal("expected error for non-existent source")
	}
}

func TestExtract_TarGz_NonExistentSource(t *testing.T) {
	tmpDir := t.TempDir()
	dstDir := filepath.Join(tmpDir, "out")
	nonExistent := filepath.Join(tmpDir, "no_such_file.tar.gz")

	err := Extract(nonExistent, dstDir, "tar.gz", 0)
	if err == nil {
		t.Fatal("expected error for non-existent tar.gz source")
	}
}

func TestStripPath(t *testing.T) {
	tests := []struct {
		input string
		strip int
		want  string
		ok    bool
	}{
		{"foo/bar.txt", 0, "foo/bar.txt", true},
		{"foo/bar.txt", 1, "bar.txt", true},
		{"foo/bar.txt", 2, "", false},
		{"a/b/c.txt", 1, "b/c.txt", true},
		{"a/b/c.txt", 2, "c.txt", true},
		{"a/b/c.txt", 3, "", false},
		{".", 0, "", false},
		{"single.txt", 0, "single.txt", true},
		{"single.txt", 1, "", false},
	}

	for _, tt := range tests {
		got, ok := stripPath(tt.input, tt.strip)
		if ok != tt.ok || got != tt.want {
			t.Errorf("stripPath(%q, %d) = (%q, %v), want (%q, %v)",
				tt.input, tt.strip, got, ok, tt.want, tt.ok)
		}
	}
}

func TestExtract_Zip_EmptyDirs(t *testing.T) {
	tmpDir := t.TempDir()
	zipPath := filepath.Join(tmpDir, "empty.zip")
	dstDir := filepath.Join(tmpDir, "out")

	createTestZip(t, zipPath, []string{
		"emptydir/",
		"emptydir/file.txt|data",
	})

	if err := Extract(zipPath, dstDir, "zip", 0); err != nil {
		t.Fatalf("Extract zip: %v", err)
	}

	if !fileExists(filepath.Join(dstDir, "emptydir")) {
		t.Error("expected emptydir directory to exist")
	}
}

func TestExtract_TarGz_StripAll(t *testing.T) {
	tmpDir := t.TempDir()
	tgzPath := filepath.Join(tmpDir, "all.tar.gz")
	dstDir := filepath.Join(tmpDir, "out")

	createTestTarGz(t, tgzPath, []string{
		"onlyfile.txt|content",
	})

	// strip=1 on a single-component path should skip the entry entirely
	if err := Extract(tgzPath, dstDir, "tar.gz", 1); err != nil {
		t.Fatalf("Extract tar.gz strip=1 on single component: %v", err)
	}

	// The file should not exist because strip=1 removes the only path component
	if fileExists(filepath.Join(dstDir, "onlyfile.txt")) {
		t.Error("onlyfile.txt should not exist after strip=1")
	}
}

// --- Zip Slip security tests ---
//
// These guard the isSafePath check in extractZip/extractTarFromReader
// against Zip Slip / Tar Slip path-traversal attacks, where a malicious
// archive entry like "../escape.txt" or an absolute path would write
// outside the destination directory.

func TestIsSafePath_InsideDestination(t *testing.T) {
	dst := t.TempDir()
	out := filepath.Join(dst, "sub", "file.txt")
	if !isSafePath(dst, out) {
		t.Error("path inside destination should be safe")
	}
}

func TestIsSafePath_DestinationItself(t *testing.T) {
	dst := t.TempDir()
	if !isSafePath(dst, dst) {
		t.Error("destination itself should be safe")
	}
}

func TestIsSafePath_SiblingPrefix(t *testing.T) {
	// /tmp/foo vs /tmp/foobar must NOT match via prefix (the separator guard).
	tmp := t.TempDir()
	dst := filepath.Join(tmp, "foo")
	os.MkdirAll(dst, 0o755)
	sibling := filepath.Join(tmp, "foobar", "file.txt")
	if isSafePath(dst, sibling) {
		t.Error("sibling with shared prefix must be rejected (no false prefix match)")
	}
}

func TestIsSafePath_EscapesViaParent(t *testing.T) {
	dst := t.TempDir()
	// Join cleans "../.." so build the escape explicitly and verify rejection.
	escape := filepath.Join(dst, "..", "..", "escaped.txt")
	if isSafePath(dst, escape) {
		t.Error("path escaping via ../ must be rejected")
	}
}

// createMaliciousZip builds a zip whose entry name is an absolute path,
// which filepath.Join cannot coerce back under dst. This exercises the
// isSafePath guard end to end through extractZip.
func createMaliciousZip(t *testing.T, dstPath, entryName string) {
	t.Helper()
	f, err := os.Create(dstPath)
	if err != nil {
		t.Fatalf("create zip: %v", err)
	}
	defer f.Close()
	w := zip.NewWriter(f)
	out, err := w.Create(entryName)
	if err != nil {
		t.Fatalf("zip create %s: %v", entryName, err)
	}
	if _, err := out.Write([]byte("pwned")); err != nil {
		t.Fatalf("zip write: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}
}

func TestExtract_ZipSlip_AbsolutePathRejected(t *testing.T) {
	tmpDir := t.TempDir()
	zipPath := filepath.Join(tmpDir, "evil.zip")
	dstDir := filepath.Join(tmpDir, "out")

	// An absolute entry path cannot be coerced under dst by filepath.Join.
	// The isSafePath guard (or a downstream path failure) must prevent the
	// entry from being written outside the destination.
	absTarget := filepath.Join(tmpDir, "escaped.txt")
	createMaliciousZip(t, zipPath, absTarget)

	err := Extract(zipPath, dstDir, "zip", 0)
	// Either the slip guard rejects it explicitly, or the malformed joined
	// path fails to create. Either way, extraction must error out.
	if err == nil {
		t.Fatal("expected error for absolute-path Zip Slip entry, got nil")
	}
	// The escaped file must NOT have been written — the core security assertion.
	if fileExists(absTarget) {
		t.Errorf("absolute-path entry was written to %s (Zip Slip succeeded!)", absTarget)
	}
	// The destination must remain empty (no partial extraction to an unsafe path).
	if entries, _ := os.ReadDir(dstDir); len(entries) != 0 {
		t.Errorf("expected empty dst, got %d entries", len(entries))
	}
}

func TestExtract_ZipSlip_NormalZipStillWorks(t *testing.T) {
	// Regression guard: the slip check must not reject legitimate archives.
	tmpDir := t.TempDir()
	zipPath := filepath.Join(tmpDir, "ok.zip")
	dstDir := filepath.Join(tmpDir, "out")

	createTestZip(t, zipPath, []string{
		"bin/",
		"bin/hello.txt|hi",
	})
	if err := Extract(zipPath, dstDir, "zip", 0); err != nil {
		t.Fatalf("legitimate zip rejected: %v", err)
	}
	if !fileExists(filepath.Join(dstDir, "bin", "hello.txt")) {
		t.Error("legitimate file missing after extract")
	}
}
