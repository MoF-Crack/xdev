// negative_strip_test.go — F-20 guards: a negative strip value must never
// reach a slice expression. Pre-fix stripPath only guarded the upper bound
// (strip >= len(parts)) so stripPath("a/b/c", -1) panicked with
// "slice bounds out of range [-1:]" — and Extract had no entry-level
// validation, so any manifest with `strip: -1` crashed the whole install.
package extractor

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestStripPath_NegativeStripDoesNotPanic asserts stripPath clamps a
// negative strip to 0 (treat as "strip nothing") instead of panicking.
func TestStripPath_NegativeStripDoesNotPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("stripPath panicked on a negative strip: %v", r)
		}
	}()

	got, ok := stripPath("a/b/c", -1)
	if !ok {
		t.Fatalf("stripPath(\"a/b/c\", -1) = (%q, false), want the path kept (clamped to strip=0)", got)
	}
	if got != "a/b/c" {
		t.Errorf("stripPath(\"a/b/c\", -1) = %q, want %q (negative strip clamped to 0)", got, "a/b/c")
	}

	// The clamp must hold for deeper negatives too.
	if got, ok := stripPath("a/b/c", -99); !ok || got != "a/b/c" {
		t.Errorf("stripPath(\"a/b/c\", -99) = (%q, %v), want (\"a/b/c\", true)", got, ok)
	}

	// A single-component path must still behave.
	if got, ok := stripPath("single.txt", -1); !ok || got != "single.txt" {
		t.Errorf("stripPath(\"single.txt\", -1) = (%q, %v), want (\"single.txt\", true)", got, ok)
	}

	// "." is still skipped regardless of the clamp.
	if _, ok := stripPath(".", -1); ok {
		t.Error("stripPath(\".\", -1) reported ok=true, want false")
	}
}

// TestExtract_RejectsNegativeStrip asserts the Extract entry point refuses a
// negative strip outright (defence in depth: the value comes from the
// manifest and was previously unvalidated end to end).
func TestExtract_RejectsNegativeStrip(t *testing.T) {
	tmpDir := t.TempDir()
	zipPath := filepath.Join(tmpDir, "neg.zip")
	createTestZip(t, zipPath, []string{"top/bin/tool|data"})

	err := Extract(zipPath, filepath.Join(tmpDir, "out"), "zip", -1)
	if err == nil {
		t.Fatal("Extract accepted strip=-1, want a rejection error")
	}
	if !strings.Contains(err.Error(), "strip") {
		t.Errorf("error = %q, want it to mention the strip parameter", err)
	}
}

// TestExtract_RejectsNegativeStrip_TarGz covers a second format so the guard
// is proven to live at the shared entry point, not in one branch.
func TestExtract_RejectsNegativeStrip_TarGz(t *testing.T) {
	tmpDir := t.TempDir()
	tarPath := filepath.Join(tmpDir, "neg.tar.gz")
	createTestTarGz(t, tarPath, []string{"top/bin/tool|data"})

	err := Extract(tarPath, filepath.Join(tmpDir, "out"), "tar.gz", -1)
	if err == nil {
		t.Fatal("Extract accepted strip=-1 for tar.gz, want a rejection error")
	}
	if !strings.Contains(err.Error(), "strip") {
		t.Errorf("error = %q, want it to mention the strip parameter", err)
	}
}

// TestExtractZip_NegativeStripDoesNotPanic drives the real zip path (rather
// than the helper) to prove the whole extraction entry is panic-free.
func TestExtractZip_NegativeStripDoesNotPanic(t *testing.T) {
	tmpDir := t.TempDir()
	zipPath := filepath.Join(tmpDir, "neg2.zip")
	createTestZip(t, zipPath, []string{"top/bin/tool|data"})

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("extractZip panicked on a negative strip: %v", r)
		}
	}()

	// Direct call: the helper must be safe even when reached without the
	// Extract guard (it is also called from the tar paths).
	if err := extractZip(zipPath, filepath.Join(tmpDir, "out"), -1); err != nil {
		t.Fatalf("extractZip with strip=-1: %v", err)
	}
}
