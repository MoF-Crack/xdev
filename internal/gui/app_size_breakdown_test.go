// app_size_breakdown_test.go tests the install/cache disk usage breakdown
// APIs (Spec 019).
package gui

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/xmdemt/xdev/internal/config"
)

// sizeBreakdownApp builds an App wired to a temp dir with install/ and
// cache/ subdirectories.
func sizeBreakdownApp(t *testing.T) (*App, string) {
	t.Helper()
	tmp := t.TempDir()
	cfg := &config.Config{
		Core: config.CoreConfig{
			XMDevDir:    tmp,
			InstallDir:  filepath.Join(tmp, "install"),
			CacheDir:    filepath.Join(tmp, "cache"),
			ShimDir:     filepath.Join(tmp, "shims"),
			ManifestDir: filepath.Join(tmp, "manifests"),
		},
		Path: config.PathConfig{Mode: "shim"},
	}
	for _, d := range []string{cfg.Core.InstallDir, cfg.Core.CacheDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
	}
	return &App{cfg: cfg}, tmp
}

// writeSizedFile writes a file with exactly n bytes of content.
func writeSizedFile(t *testing.T, path string, n int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, make([]byte, n), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// TestGetInstallSizeBreakdown verifies per-version install sizes, sorted
// descending by size, with correct Tool/Version/Path fields.
func TestGetInstallSizeBreakdown(t *testing.T) {
	app, tmp := sizeBreakdownApp(t)

	writeSizedFile(t, filepath.Join(tmp, "install", "go", "1.26.5", "bin", "go"), 300)
	writeSizedFile(t, filepath.Join(tmp, "install", "go", "1.25.3", "bin", "go"), 100)
	writeSizedFile(t, filepath.Join(tmp, "install", "llvm", "22.1.8", "bin", "llvm"), 500)

	entries := app.GetInstallSizeBreakdown()
	if len(entries) != 3 {
		t.Fatalf("len(entries) = %d, want 3", len(entries))
	}

	want := []struct {
		tool, version string
		size          int64
	}{
		{"llvm", "22.1.8", 500},
		{"go", "1.26.5", 300},
		{"go", "1.25.3", 100},
	}
	for i, w := range want {
		e := entries[i]
		if e.Tool != w.tool || e.Version != w.version || e.Size != w.size {
			t.Errorf("entries[%d] = {%s %s %d}, want {%s %s %d}", i, e.Tool, e.Version, e.Size, w.tool, w.version, w.size)
		}
		if e.Path != filepath.Join(tmp, "install", w.tool, w.version) {
			t.Errorf("entries[%d].Path = %q", i, e.Path)
		}
	}
}

// TestGetCacheSizeBreakdown verifies per-file cache sizes with tool/version
// parsing, stale marking, and descending size order. Metadata files
// (.json/.dat/.sha256) are shown as-is without stale marking.
func TestGetCacheSizeBreakdown(t *testing.T) {
	app, tmp := sizeBreakdownApp(t)

	// Install java/17 so java cache files are NOT stale.
	writeSizedFile(t, filepath.Join(tmp, "install", "java", "17", "bin", "java"), 10)

	writeSizedFile(t, filepath.Join(tmp, "cache", "java-17.zip"), 1000)
	writeSizedFile(t, filepath.Join(tmp, "cache", "java-17.zip.sha256"), 50)
	writeSizedFile(t, filepath.Join(tmp, "cache", "llvm-22.1.8.tar.xz"), 2000)
	writeSizedFile(t, filepath.Join(tmp, "cache", "manifest-cache.dat"), 20)
	writeSizedFile(t, filepath.Join(tmp, "cache", "tool_scan_cache.json"), 30)
	writeSizedFile(t, filepath.Join(tmp, "cache", "go-1.26.5.zip.part"), 500)

	entries := app.GetCacheSizeBreakdown()
	if len(entries) != 6 {
		t.Fatalf("len(entries) = %d, want 6", len(entries))
	}

	want := []struct {
		tool, version string
		size          int64
		stale         bool
		name          string
	}{
		{"llvm", "22.1.8", 2000, true, "llvm-22.1.8.tar.xz"},
		{"java", "17", 1000, false, "java-17.zip"},
		{"go", "1.26.5", 500, true, "go-1.26.5.zip.part"},
		// 元数据文件：Tool=完整文件名，Version="", IsStale=false。
		{"java-17.zip.sha256", "", 50, false, "java-17.zip.sha256"},
		{"tool_scan_cache.json", "", 30, false, "tool_scan_cache.json"},
		{"manifest-cache.dat", "", 20, false, "manifest-cache.dat"},
	}
	for i, w := range want {
		e := entries[i]
		if e.Tool != w.tool || e.Version != w.version || e.Size != w.size || e.IsStale != w.stale {
			t.Errorf("entries[%d] = {%s %s %d stale=%v}, want {%s %s %d stale=%v}",
				i, e.Tool, e.Version, e.Size, e.IsStale, w.tool, w.version, w.size, w.stale)
		}
		if e.Path != filepath.Join(tmp, "cache", w.name) {
			t.Errorf("entries[%d].Path = %q, want %q", i, e.Path, filepath.Join(tmp, "cache", w.name))
		}
	}
}

// TestGetCacheSizeBreakdown_MetadataNotStale verifies that xdev metadata
// files (even ones containing "-") are never parsed as tool-version pairs
// and never marked stale.
func TestGetCacheSizeBreakdown_MetadataNotStale(t *testing.T) {
	app, tmp := sizeBreakdownApp(t)

	writeSizedFile(t, filepath.Join(tmp, "cache", "sniff-cache.json"), 100)
	writeSizedFile(t, filepath.Join(tmp, "cache", "mirror-health.json"), 200)
	writeSizedFile(t, filepath.Join(tmp, "cache", "sniff-cache.json.lock"), 300)

	entries := app.GetCacheSizeBreakdown()
	if len(entries) != 3 {
		t.Fatalf("len(entries) = %d, want 3", len(entries))
	}

	want := []struct {
		tool string
		size int64
	}{
		{"sniff-cache.json.lock", 300},
		{"mirror-health.json", 200},
		{"sniff-cache.json", 100},
	}
	for i, w := range want {
		e := entries[i]
		if e.Tool != w.tool || e.Version != "" || e.Size != w.size || e.IsStale {
			t.Errorf("entries[%d] = {%s %q %d stale=%v}, want Tool=%q Version=\"\" size=%d stale=false",
				i, e.Tool, e.Version, e.Size, e.IsStale, w.tool, w.size)
		}
	}
}

// TestGetCacheSizeBreakdown_Category verifies each cache entry carries the
// same category used by the settings-page CacheBreakdown totals.
func TestGetCacheSizeBreakdown_Category(t *testing.T) {
	app, tmp := sizeBreakdownApp(t)

	writeSizedFile(t, filepath.Join(tmp, "cache", "llvm-22.1.8.tar.xz"), 2000)
	writeSizedFile(t, filepath.Join(tmp, "cache", "java-17.zip.sha256"), 50)
	writeSizedFile(t, filepath.Join(tmp, "cache", "go-1.26.5.zip.part"), 500)
	writeSizedFile(t, filepath.Join(tmp, "cache", "tool_scan_cache.json"), 30)
	writeSizedFile(t, filepath.Join(tmp, "cache", "sniff-cache.json"), 100)
	writeSizedFile(t, filepath.Join(tmp, "cache", "manifest-cache.dat"), 20)

	entries := app.GetCacheSizeBreakdown()
	if len(entries) != 6 {
		t.Fatalf("len(entries) = %d, want 6", len(entries))
	}

	want := map[string]string{
		"llvm-22.1.8.tar.xz":   "archives",
		"java-17.zip.sha256":   "shaFiles",
		"go-1.26.5.zip.part":   "partFiles",
		"tool_scan_cache.json": "scanCache",
		"sniff-cache.json":     "otherFiles",
		"manifest-cache.dat":   "otherFiles",
	}
	for _, e := range entries {
		wantCat, ok := want[filepath.Base(e.Path)]
		if !ok {
			t.Errorf("unexpected entry %s", e.Path)
			continue
		}
		if e.Category != wantCat {
			t.Errorf("entry %s Category = %q, want %q", filepath.Base(e.Path), e.Category, wantCat)
		}
	}

	// Install breakdown entries must NOT carry a category (omitempty).
	writeSizedFile(t, filepath.Join(tmp, "install", "go", "1.26.5", "bin", "go"), 100)
	installs := app.GetInstallSizeBreakdown()
	for _, e := range installs {
		if e.Category != "" {
			t.Errorf("install entry %s/%s should have empty Category, got %q", e.Tool, e.Version, e.Category)
		}
	}
}

// TestSizeBreakdown_EmptyDirs verifies both methods return an empty,
// non-nil slice when the directories are missing or empty.
func TestSizeBreakdown_EmptyDirs(t *testing.T) {
	app, _ := sizeBreakdownApp(t)

	install := app.GetInstallSizeBreakdown()
	if install == nil || len(install) != 0 {
		t.Errorf("GetInstallSizeBreakdown = %v (nil=%v), want empty non-nil slice", install, install == nil)
	}
	cache := app.GetCacheSizeBreakdown()
	if cache == nil || len(cache) != 0 {
		t.Errorf("GetCacheSizeBreakdown = %v (nil=%v), want empty non-nil slice", cache, cache == nil)
	}
}

// TestParseCacheName covers the tool/version parsing rules incl. .part and
// multi-segment archive extensions.
func TestParseCacheName(t *testing.T) {
	cases := []struct {
		name, tool, version string
	}{
		{"java-17.zip", "java", "17"},
		{"llvm-22.1.8.tar.xz", "llvm", "22.1.8"},
		{"go-1.26.5.zip.part", "go", "1.26.5"},
		{"rust-1.77.0.tar.gz", "rust", "1.77.0"},
		{"node-20.11.0.zip.sha256", "node", "20.11.0"},
		// Spec 021 D2：归档后缀表统一后不含 .dat（元数据后缀），因此
		// manifest-cache.dat 不再剥离 .dat，Version 保留 "cache.dat"。
		// （明细扫描中该类元数据文件本就走 isMetadataFile 分支，不调用本函数。）
		{"manifest-cache.dat", "manifest", "cache.dat"},
		{"tool_scan_cache.json", "-", ""},
		{"no-dash-here", "no", "dash-here"},
	}
	for _, c := range cases {
		tool, version := parseCacheName(c.name)
		if tool != c.tool || version != c.version {
			t.Errorf("parseCacheName(%q) = (%q, %q), want (%q, %q)", c.name, tool, version, c.tool, c.version)
		}
	}
}

// TestSizeBreakdown_CacheHits verifies the TTL cache: repeated calls within
// the TTL window return the cached snapshot even when the disk changes, and
// InvalidateSizeBreakdown forces a fresh scan.
func TestSizeBreakdown_CacheHits(t *testing.T) {
	app, tmp := sizeBreakdownApp(t)

	goFile := filepath.Join(tmp, "install", "go", "1.26.5", "bin", "go")
	writeSizedFile(t, goFile, 100)

	first := app.GetInstallSizeBreakdown()
	if len(first) != 1 || first[0].Size != 100 || first[0].Tool != "go" {
		t.Fatalf("first call = %+v, want 1 entry go/1.26.5 @100", first)
	}

	// Disk changes (a second version appears) — cached call must NOT see it.
	writeSizedFile(t, filepath.Join(tmp, "install", "llvm", "22.1.8", "bin", "llvm"), 500)
	second := app.GetInstallSizeBreakdown()
	if len(second) != 1 || second[0].Size != 100 {
		t.Fatalf("cached call = %+v, want cached single entry @100", second)
	}

	// Invalidate → third call reflects the disk.
	app.InvalidateSizeBreakdown()
	third := app.GetInstallSizeBreakdown()
	if len(third) != 2 {
		t.Fatalf("after invalidate len = %d, want 2", len(third))
	}
	if third[0].Tool != "llvm" || third[0].Size != 500 {
		t.Errorf("after invalidate third[0] = %+v, want llvm @500 (size desc)", third[0])
	}
}

// TestSizeBreakdown_CacheHitsCache verifies the cache breakdown honors the
// same TTL + invalidation behavior.
func TestSizeBreakdown_CacheHitsCache(t *testing.T) {
	app, tmp := sizeBreakdownApp(t)

	writeSizedFile(t, filepath.Join(tmp, "cache", "java-17.zip"), 100)

	first := app.GetCacheSizeBreakdown()
	if len(first) != 1 || first[0].Tool != "java" || first[0].Size != 100 {
		t.Fatalf("first call = %+v, want 1 entry java-17.zip @100", first)
	}

	// New archive appears — cached call must NOT see it.
	writeSizedFile(t, filepath.Join(tmp, "cache", "llvm-22.1.8.tar.xz"), 2000)
	second := app.GetCacheSizeBreakdown()
	if len(second) != 1 {
		t.Fatalf("cached call len = %d, want 1 (cached)", len(second))
	}

	app.InvalidateSizeBreakdown()
	third := app.GetCacheSizeBreakdown()
	if len(third) != 2 {
		t.Fatalf("after invalidate len = %d, want 2", len(third))
	}
	if third[0].Tool != "llvm" || third[0].Size != 2000 {
		t.Errorf("after invalidate third[0] = %+v, want llvm @2000 (size desc)", third[0])
	}
}

// TestSizeBreakdown_ReturnedSlicesAreCopies verifies callers cannot mutate
// the internal cache through the returned slice.
func TestSizeBreakdown_ReturnedSlicesAreCopies(t *testing.T) {
	app, tmp := sizeBreakdownApp(t)

	writeSizedFile(t, filepath.Join(tmp, "install", "go", "1.26.5", "bin", "go"), 100)

	a := app.GetInstallSizeBreakdown()
	if len(a) != 1 {
		t.Fatalf("len = %d, want 1", len(a))
	}
	a[0].Size = 999999 // caller-side mutation

	b := app.GetInstallSizeBreakdown() // cache hit
	if len(b) != 1 || b[0].Size != 100 {
		t.Fatalf("cache corrupted by caller mutation: %+v", b)
	}
}
