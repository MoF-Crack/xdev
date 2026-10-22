package util

import (
	"bytes"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// Ticket 09 (audit-fixes / P0-04 + N-01) PoC guard tests.
//
// The historical bug: for a directory-symlink entry copyDir returned
// filepath.SkipDir — which, for a NON-directory entry, tells filepath.Walk
// to skip the REMAINING SIBLINGS of the parent directory. Every entry
// sorted after the link was silently not copied. Callers (migration flows)
// RemoveAll the source right afterwards → unrecoverable data loss.
//
// Windows junctions additionally surfaced as ModeIrregular (not
// ModeSymlink) and were fed to CopyFile, failing loudly.
//
// Broken/unresolvable links were dropped silently (second data-loss path).

// C1: every sibling sorted after a symlink entry must still be copied.
// Layout (walk order): aaa.txt < current(link) < realdir < v2.0.1 < zzz.txt.
func TestCopyDir_SymlinkSiblingsAllCopied(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation requires privileges on Windows — junction variant is TestCopyDir_JunctionFollowed_Windows")
	}
	src := t.TempDir()
	for _, d := range []string{"realdir", "v2.0.1"} {
		if err := os.MkdirAll(filepath.Join(src, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(src, "realdir", "inner.txt"), []byte("inner"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "v2.0.1", "ver.txt"), []byte("v"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "aaa.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "zzz.txt"), []byte("z"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(src, "realdir"), filepath.Join(src, "current")); err != nil {
		t.Skipf("symlink failed: %v", err)
	}

	dst := filepath.Join(t.TempDir(), "copy")
	if err := CopyDir(src, dst); err != nil {
		t.Fatalf("CopyDir: %v", err)
	}
	for _, p := range []string{
		"aaa.txt",
		"current" + string(os.PathSeparator) + "inner.txt",
		"realdir" + string(os.PathSeparator) + "inner.txt",
		"v2.0.1" + string(os.PathSeparator) + "ver.txt",
		"zzz.txt",
	} {
		if _, err := os.Stat(filepath.Join(dst, p)); err != nil {
			t.Errorf("entry lost after link sibling: %s: %v", p, err)
		}
	}
}

// C3: Windows junction (ModeIrregular) entries must resolve to their
// target and be copied recursively — plus siblings after the junction
// must survive.
func TestCopyDir_JunctionFollowed_Windows(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("junction-specific test — Windows only")
	}
	src := t.TempDir()
	realdir := filepath.Join(src, "realdir")
	if err := os.MkdirAll(realdir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(realdir, "inner.txt"), []byte("inner"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(src, "v2.0.1"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "v2.0.1", "ver.txt"), []byte("v"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "aaa.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "zzz.txt"), []byte("z"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := CreateJunction(filepath.Join(src, "current"), realdir); err != nil {
		t.Skipf("junction creation failed: %v", err)
	}

	dst := filepath.Join(t.TempDir(), "copy")
	if err := CopyDir(src, dst); err != nil {
		t.Fatalf("CopyDir: %v", err)
	}
	for _, p := range []string{
		"aaa.txt",
		"current" + string(os.PathSeparator) + "inner.txt",
		"v2.0.1" + string(os.PathSeparator) + "ver.txt",
		"zzz.txt",
	} {
		if _, err := os.Stat(filepath.Join(dst, p)); err != nil {
			t.Errorf("entry lost at junction layout: %s: %v", p, err)
		}
	}
}

// C4: a broken link entry must be a VISIBLE skip — err==nil (copy itself
// succeeds), skipped==1, a WARN log naming the entry, and the normal
// siblings copied. Previously the entry vanished silently (N-01).
func TestCopyDir_BrokenLinkSkippedWithWarning(t *testing.T) {
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "normal.txt"), []byte("n"), 0o644); err != nil {
		t.Fatal(err)
	}
	danglingTarget := filepath.Join(t.TempDir(), "does-not-exist")
	if runtime.GOOS == "windows" {
		// mklink /J happily creates a junction to a missing target.
		if err := CreateJunction(filepath.Join(src, "dangling"), danglingTarget); err != nil {
			t.Skipf("junction creation failed: %v", err)
		}
	} else {
		if err := os.Symlink(danglingTarget, filepath.Join(src, "dangling")); err != nil {
			t.Skipf("symlink failed: %v", err)
		}
	}

	var logBuf bytes.Buffer
	log.SetOutput(&logBuf)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })

	dst := filepath.Join(t.TempDir(), "copy")
	files, skipped, err := CopyDirReport(src, dst, nil)
	if err != nil {
		t.Fatalf("CopyDirReport: %v (copy itself must succeed)", err)
	}
	if skipped != 1 {
		t.Errorf("skipped = %d, want 1 (broken link must be a visible skip — previously silent)", skipped)
	}
	if files < 1 {
		t.Errorf("files = %d, want >= 1 (normal.txt must be copied)", files)
	}
	if _, err := os.Stat(filepath.Join(dst, "normal.txt")); err != nil {
		t.Errorf("normal sibling not copied: %v", err)
	}
	if out := logBuf.String(); !strings.Contains(out, "WARN") || !strings.Contains(out, "dangling") {
		t.Errorf("warning log missing or does not name the broken entry; log: %q", out)
	}
}

// C5: a self-referential link must not recurse (guard) — the copy
// terminates cleanly and the entry is reported as skipped.
func TestCopyDir_SelfReferentialGuard(t *testing.T) {
	src := t.TempDir()
	self := filepath.Join(src, "selfloop")
	if runtime.GOOS == "windows" {
		// junction pointing at its own path
		if err := CreateJunction(self, self); err != nil {
			t.Skipf("self-junction creation failed: %v", err)
		}
	} else {
		if err := os.Symlink(self, self); err != nil {
			t.Skipf("self-symlink creation failed: %v", err)
		}
	}

	done := make(chan struct{})
	var files, skipped int
	var err error
	go func() {
		defer close(done)
		files, skipped, err = CopyDirReport(src, t.TempDir(), nil)
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("CopyDirReport did not terminate — self-referential recursion")
	}
	if err != nil {
		t.Fatalf("CopyDirReport: %v", err)
	}
	if skipped < 1 {
		t.Errorf("skipped = %d, want >= 1 (self-referential entry must be reported)", skipped)
	}
	_ = files
}
