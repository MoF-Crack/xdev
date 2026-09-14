// app_cache_delete_test.go tests the per-file and per-category cache
// deletion APIs (Spec 019 F6): path-traversal protection, companion
// deletion, category-scoped cleaning, and idempotency.
package gui

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// cacheFileNames returns the sorted top-level file names in the cache dir.
func cacheFileNames(t *testing.T, cacheDir string) []string {
	t.Helper()
	entries, err := os.ReadDir(cacheDir)
	if err != nil {
		t.Fatalf("ReadDir(%s): %v", cacheDir, err)
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	return names
}

func assertCacheFiles(t *testing.T, cacheDir string, want ...string) {
	t.Helper()
	got := cacheFileNames(t, cacheDir)
	sort.Strings(want)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("cache files = %v, want %v", got, want)
	}
}

// TestDeleteCacheFile verifies deleting an archive also removes its
// .sha256/.part companions, and that a second delete is idempotent (nil).
func TestDeleteCacheFile(t *testing.T) {
	app, tmp := retentionApp(t)

	zipPath := filepath.Join(tmp, "cache", "go-1.26.5.zip")
	writeSizedFile(t, zipPath, 100)
	writeSizedFile(t, zipPath+".sha256", 20)
	writeSizedFile(t, zipPath+".part", 30)
	writeSizedFile(t, filepath.Join(tmp, "cache", "llvm-22.1.8.tar.xz"), 200)

	if err := app.DeleteCacheFile(zipPath); err != nil {
		t.Fatalf("DeleteCacheFile failed: %v", err)
	}
	assertCacheFiles(t, filepath.Join(tmp, "cache"), "llvm-22.1.8.tar.xz")

	// Idempotent: deleting an already-gone file is not an error.
	if err := app.DeleteCacheFile(zipPath); err != nil {
		t.Fatalf("second DeleteCacheFile should be idempotent, got: %v", err)
	}
}

// TestDeleteCacheFile_TraversalRejected verifies traversal paths are
// rejected and the target file outside the cache dir stays untouched.
func TestDeleteCacheFile_TraversalRejected(t *testing.T) {
	app, tmp := retentionApp(t)

	// File outside the cache dir.
	secret := filepath.Join(tmp, "secret.txt")
	writeSizedFile(t, secret, 10)

	cacheDir := filepath.Join(tmp, "cache")
	err := app.DeleteCacheFile(filepath.Join(cacheDir, "..", "secret.txt"))
	if err == nil {
		t.Fatal("traversal path should be rejected")
	}
	if _, statErr := os.Stat(secret); statErr != nil {
		t.Errorf("file outside cache dir must not be deleted: %v", statErr)
	}
	assertCacheFiles(t, cacheDir)

	// Deep traversal variant.
	err = app.DeleteCacheFile(filepath.Join(cacheDir, "..", "..", "..", "etc", "passwd"))
	if err == nil {
		t.Fatal("deep traversal path should be rejected")
	}
}

// TestDeleteCacheFile_OutsideCacheDir verifies an absolute path outside the
// cache dir is rejected.
func TestDeleteCacheFile_OutsideCacheDir(t *testing.T) {
	app, tmp := retentionApp(t)

	outside := filepath.Join(tmp, "outside.txt")
	writeSizedFile(t, outside, 10)

	if err := app.DeleteCacheFile(outside); err == nil {
		t.Fatal("path outside cache dir should be rejected")
	}
	if _, statErr := os.Stat(outside); statErr != nil {
		t.Errorf("file outside cache dir must not be deleted: %v", statErr)
	}
}

// TestDeleteCacheFile_DirectoryRejected verifies a directory inside the
// cache dir is not deleted.
func TestDeleteCacheFile_DirectoryRejected(t *testing.T) {
	app, tmp := retentionApp(t)

	dir := filepath.Join(tmp, "cache", "subdir")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := app.DeleteCacheFile(dir); err == nil {
		t.Fatal("directory path should be rejected")
	}
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		t.Error("directory must not be deleted")
	}
}

// TestCleanCacheCategory verifies category-scoped cleaning: only matching
// files are removed, directories are never touched, "all" removes every
// file, and unknown categories error.
func TestCleanCacheCategory(t *testing.T) {
	app, tmp := retentionApp(t)
	cacheDir := filepath.Join(tmp, "cache")

	// A directory that must survive every category.
	if err := os.MkdirAll(filepath.Join(cacheDir, "subdir"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeSizedFile(t, filepath.Join(cacheDir, "subdir", "inner.bin"), 5)

	files := []string{
		"llvm-22.1.8.tar.xz",   // archives
		"java-17.zip",          // archives
		"java-17.zip.sha256",   // shaFiles
		"go-1.26.5.zip.part",   // partFiles
		"tool_scan_cache.json", // scanCache
		"sniff-cache.json",     // otherFiles
		"manifest-cache.dat",   // otherFiles
		"favorites.json",       // otherFiles
	}
	for _, f := range files {
		writeSizedFile(t, filepath.Join(cacheDir, f), 10)
	}

	// Unknown category → error, nothing removed.
	if err := app.CleanCacheCategory("bogus"); err == nil {
		t.Fatal("unknown category should error")
	}
	assertCacheFiles(t, cacheDir, files...)

	// archives: only the two archives removed.
	if err := app.CleanCacheCategory("archives"); err != nil {
		t.Fatalf("archives cleanup failed: %v", err)
	}
	assertCacheFiles(t, cacheDir,
		"favorites.json", "go-1.26.5.zip.part", "java-17.zip.sha256",
		"manifest-cache.dat", "sniff-cache.json", "tool_scan_cache.json")

	// shaFiles.
	if err := app.CleanCacheCategory("shaFiles"); err != nil {
		t.Fatal(err)
	}
	assertCacheFiles(t, cacheDir,
		"favorites.json", "go-1.26.5.zip.part",
		"manifest-cache.dat", "sniff-cache.json", "tool_scan_cache.json")

	// partFiles.
	if err := app.CleanCacheCategory("partFiles"); err != nil {
		t.Fatal(err)
	}
	assertCacheFiles(t, cacheDir,
		"favorites.json", "manifest-cache.dat", "sniff-cache.json", "tool_scan_cache.json")

	// scanCache: only tool_scan_cache.json.
	if err := app.CleanCacheCategory("scanCache"); err != nil {
		t.Fatal(err)
	}
	assertCacheFiles(t, cacheDir, "favorites.json", "manifest-cache.dat", "sniff-cache.json")

	// otherFiles: every remaining non-archive/non-sidecar file.
	if err := app.CleanCacheCategory("otherFiles"); err != nil {
		t.Fatal(err)
	}
	assertCacheFiles(t, cacheDir)

	// all: recreate and remove everything (files only).
	for _, f := range files {
		writeSizedFile(t, filepath.Join(cacheDir, f), 10)
	}
	if err := app.CleanCacheCategory("all"); err != nil {
		t.Fatal(err)
	}
	assertCacheFiles(t, cacheDir)

	// The subdir (and its content) must have survived everything.
	if fi, err := os.Stat(filepath.Join(cacheDir, "subdir")); err != nil || !fi.IsDir() {
		t.Error("directories must never be deleted by CleanCacheCategory")
	}
	if _, err := os.Stat(filepath.Join(cacheDir, "subdir", "inner.bin")); err != nil {
		t.Error("files inside subdirectories must never be deleted")
	}
}
