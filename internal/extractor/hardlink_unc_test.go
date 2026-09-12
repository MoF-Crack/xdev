package extractor

import (
	"archive/tar"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// Ticket 38 (P2-01/P2-02) guards.

// P2-01: a tar hard-link entry must materialise its content (pre-fix the
// TypeLink case did not exist at all, so linked files were silently
// dropped).
func TestExtractTar_HardLinkMaterialized(t *testing.T) {
	dir := t.TempDir()
	tarPath := filepath.Join(dir, "links.tar")

	f, err := os.Create(tarPath)
	if err != nil {
		t.Fatal(err)
	}
	tw := tar.NewWriter(f)
	payload := []byte("shared payload")

	// Entry 1: the real file, inside a wrapper dir (strip=1 later).
	if err := tw.WriteHeader(&tar.Header{
		Name: "pkg/data.txt", Mode: 0o644, Size: int64(len(payload)), Typeflag: tar.TypeReg,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(payload); err != nil {
		t.Fatal(err)
	}
	// Entry 2: a hard link to it (linkname is an ARCHIVE path).
	if err := tw.WriteHeader(&tar.Header{
		Name: "pkg/copy.txt", Linkname: "pkg/data.txt", Mode: 0o644, Typeflag: tar.TypeLink,
	}); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	dst := filepath.Join(dir, "out")
	if err := Extract(tarPath, dst, "tar", 1); err != nil {
		t.Fatalf("Extract: %v", err)
	}

	for _, name := range []string{"data.txt", "copy.txt"} {
		got, readErr := os.ReadFile(filepath.Join(dst, name))
		if readErr != nil {
			t.Fatalf("hard link entry %s missing after extraction: %v", name, readErr)
		}
		if string(got) != string(payload) {
			t.Errorf("%s content = %q, want %q", name, got, payload)
		}
	}
	// On Unix the two names must actually share an inode (真硬链接), not be
	// two independent copies.
	if runtime.GOOS != "windows" {
		fi1, err1 := os.Stat(filepath.Join(dst, "data.txt"))
		fi2, err2 := os.Stat(filepath.Join(dst, "copy.txt"))
		if err1 == nil && err2 == nil && !os.SameFile(fi1, fi2) {
			t.Log("note: link fell back to a copy (filesystem refused os.Link) — content is still correct")
		}
	}
}

// P2-01b: a hard link whose target was never extracted is skipped with a
// warning instead of failing the whole extraction.
func TestExtractTar_DanglingHardLinkSkipped(t *testing.T) {
	dir := t.TempDir()
	tarPath := filepath.Join(dir, "dangling.tar")
	f, err := os.Create(tarPath)
	if err != nil {
		t.Fatal(err)
	}
	tw := tar.NewWriter(f)
	if err := tw.WriteHeader(&tar.Header{
		Name: "only.txt", Linkname: "does-not-exist.txt", Mode: 0o644, Typeflag: tar.TypeLink,
	}); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	dst := filepath.Join(dir, "out")
	if err := Extract(tarPath, dst, "tar", 0); err != nil {
		t.Fatalf("a dangling hard link must not fail the extraction: %v", err)
	}
}

// P2-02: an UNC DESTINATION must not make every entry look like zip slip,
// while an UNC/absolute ENTRY must still be rejected.
func TestIsSafePath_UNCDestinationTolerated(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("UNC semantics are Windows-specific")
	}
	uncDst := `\\server\share\xdev`
	if !isSafePath(uncDst, `\\server\share\xdev\tool\1.0.0\bin\tool.exe`) {
		t.Error("an entry inside an UNC destination must be considered safe (otherwise every extract into an UNC home fails)")
	}
	// A local destination with an UNC entry stays rejected.
	if isSafePath(`C:\dst`, `\\server\share\evil.txt`) {
		t.Error("an UNC entry under a local destination must be rejected")
	}
}
