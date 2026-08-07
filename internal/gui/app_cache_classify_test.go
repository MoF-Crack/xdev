package gui

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/xmdemt/xdev/internal/config"
)

// TestIsArchiveName verifies archive suffix detection so .tar.xz and friends
// are not miscategorized as "other" (regression for Spec 019/020 classification).
func TestIsArchiveName(t *testing.T) {
	cases := map[string]bool{
		"llvm-22.1.8.tar.xz": true,
		"java-25.zip":        true,
		"go-1.26.5.tar.gz":   true,
		"node-20.tgz":        true,
		"rustup-init.exe":    true,
		"tool-1.0.7z":        true,
		"manifest-cache.dat": false,
		"sniff-cache.json":   false,
		"favorites.json":     false,
		"keystore.dat":       false,
		"java-17.zip.sha256": false,
		"go-1.26.5.zip.part": false,
		"mirror-health.json": false,
	}
	for name, want := range cases {
		if got := isArchiveName(name); got != want {
			t.Errorf("isArchiveName(%q) = %v, want %v", name, got, want)
		}
	}
}

// TestGetCacheBreakdown_ArchiveSuffixes verifies .tar.xz archives land in
// Archives (not OtherFiles) in the breakdown.
func TestGetCacheBreakdown_ArchiveSuffixes(t *testing.T) {
	dir := t.TempDir()
	write := func(name string, size int) {
		if err := os.WriteFile(filepath.Join(dir, name), make([]byte, size), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("llvm-22.1.8.tar.xz", 862053924) // would previously fall into OtherFiles
	write("java-25.zip", 215706445)
	write("manifest-cache.dat", 3066694)
	write("tool_scan_cache.json", 1526)
	write("java-25.zip.sha256", 78)

	cfg := &config.Config{
		Core: config.CoreConfig{
			CacheDir: dir,
		},
	}
	a := &App{cfg: cfg}
	bd := a.GetCacheBreakdown()

	if bd.Archives != 862053924+215706445 {
		t.Errorf("Archives = %d, want %d", bd.Archives, 862053924+215706445)
	}
	if bd.ShaFiles != 78 {
		t.Errorf("ShaFiles = %d, want 78", bd.ShaFiles)
	}
	if bd.ScanCache != 1526 {
		t.Errorf("ScanCache = %d, want 1526", bd.ScanCache)
	}
	if bd.OtherFiles != 3066694 {
		t.Errorf("OtherFiles = %d, want 3066694 (manifest-cache.dat only)", bd.OtherFiles)
	}
	if bd.TotalCount != 5 {
		t.Errorf("TotalCount = %d, want 5", bd.TotalCount)
	}
}