// app_cache_retention_test.go tests the archive retention policy (Spec 020):
// config get/set validation, install-time handling, and expired-archive
// cleanup.
package gui

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/xmdemt/xdev/internal/config"
)

// retentionApp builds an App wired to a temp dir with a writable config.
func retentionApp(t *testing.T) (*App, string) {
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
	if err := os.MkdirAll(cfg.Core.CacheDir, 0o755); err != nil {
		t.Fatalf("mkdir cache: %v", err)
	}
	return &App{cfg: cfg}, tmp
}

// TestArchiveRetentionConfigDefaultAndSet verifies the config-level policy:
// default "immediate", valid values accepted, invalid values rejected.
func TestArchiveRetentionConfigDefaultAndSet(t *testing.T) {
	tmp := t.TempDir()
	cfg := &config.Config{Core: config.CoreConfig{XMDevDir: tmp}}

	if got := cfg.GetArchiveRetention(); got != "immediate" {
		t.Errorf("GetArchiveRetention() = %q, want %q (default)", got, "immediate")
	}

	for _, valid := range []string{"immediate", "7d", "30d", "forever"} {
		if err := cfg.SetArchiveRetention(valid); err != nil {
			t.Errorf("SetArchiveRetention(%q) failed: %v", valid, err)
			continue
		}
		if got := cfg.GetArchiveRetention(); got != valid {
			t.Errorf("GetArchiveRetention() = %q, want %q after Set", got, valid)
		}
	}

	for _, bad := range []string{"", "1d", "never", "IMMEDIATE", "7D", "always"} {
		if err := cfg.SetArchiveRetention(bad); err == nil {
			t.Errorf("SetArchiveRetention(%q) should fail", bad)
		}
	}
	if got := cfg.GetArchiveRetention(); got != "forever" {
		t.Errorf("invalid Set should leave policy unchanged, got %q", got)
	}

	// Invalid on-disk value falls back to the default.
	cfg.Core.ArchiveRetention = "garbage"
	if got := cfg.GetArchiveRetention(); got != "immediate" {
		t.Errorf("GetArchiveRetention() = %q, want %q for invalid stored value", got, "immediate")
	}
}

// TestApplyArchiveRetention_ImmediateDeletes verifies the immediate policy
// deletes the tool's archives plus .sha256/.part companions — but not other
// tools' archives and not metadata files under the same glob.
func TestApplyArchiveRetention_ImmediateDeletes(t *testing.T) {
	app, tmp := retentionApp(t)

	writeSizedFile(t, filepath.Join(tmp, "cache", "go-1.26.5.zip"), 100)
	writeSizedFile(t, filepath.Join(tmp, "cache", "go-1.26.5.zip.sha256"), 20)
	writeSizedFile(t, filepath.Join(tmp, "cache", "go-1.26.5.zip.part"), 30)
	writeSizedFile(t, filepath.Join(tmp, "cache", "go-cache.dat"), 10) // 元数据，不得删除
	writeSizedFile(t, filepath.Join(tmp, "cache", "llvm-22.1.8.tar.xz"), 200)

	app.applyArchiveRetention("go")

	for _, gone := range []string{"go-1.26.5.zip", "go-1.26.5.zip.sha256", "go-1.26.5.zip.part"} {
		if _, err := os.Stat(filepath.Join(tmp, "cache", gone)); err == nil {
			t.Errorf("%s should have been deleted (immediate policy)", gone)
		}
	}
	for _, kept := range []string{"go-cache.dat", "llvm-22.1.8.tar.xz"} {
		if _, err := os.Stat(filepath.Join(tmp, "cache", kept)); err != nil {
			t.Errorf("%s should have been kept (immediate policy): %v", kept, err)
		}
	}
}

// TestApplyArchiveRetention_Keep verifies 7d/30d/forever policies keep the
// archives after install.
func TestApplyArchiveRetention_Keep(t *testing.T) {
	app, tmp := retentionApp(t)
	app.cfg.Core.ArchiveRetention = "7d"

	writeSizedFile(t, filepath.Join(tmp, "cache", "go-1.26.5.zip"), 100)
	writeSizedFile(t, filepath.Join(tmp, "cache", "go-1.26.5.zip.sha256"), 20)

	app.applyArchiveRetention("go")

	for _, name := range []string{"go-1.26.5.zip", "go-1.26.5.zip.sha256"} {
		if _, err := os.Stat(filepath.Join(tmp, "cache", name)); err != nil {
			t.Errorf("%s should have been kept (7d policy): %v", name, err)
		}
	}
}

// setModTime sets a file's ModTime to ts.
func setModTime(t *testing.T, path string, ts time.Time) {
	t.Helper()
	if err := os.Chtimes(path, ts, ts); err != nil {
		t.Fatalf("Chtimes(%s): %v", path, err)
	}
}

// TestCleanupExpiredArchives verifies 30d cleanup removes old archives and
// their companions, keeps fresh ones, and that forever/immediate policies
// remove nothing.
func TestCleanupExpiredArchives(t *testing.T) {
	app, tmp := retentionApp(t)
	app.cfg.Core.ArchiveRetention = "30d"

	oldArchive := filepath.Join(tmp, "cache", "go-1.20.0.zip")
	oldCompanion := oldArchive + ".sha256"
	writeSizedFile(t, oldArchive, 100)
	writeSizedFile(t, oldCompanion, 20)
	old := time.Now().Add(-31 * 24 * time.Hour)
	setModTime(t, oldArchive, old)
	setModTime(t, oldCompanion, old)

	writeSizedFile(t, filepath.Join(tmp, "cache", "java-17.zip"), 200) // 新文件，保留

	removed := app.CleanupExpiredArchives()
	if removed != 2 {
		t.Errorf("CleanupExpiredArchives() = %d, want 2 (archive + companion)", removed)
	}
	if _, err := os.Stat(oldArchive); err == nil {
		t.Error("expired archive should have been removed")
	}
	if _, err := os.Stat(oldCompanion); err == nil {
		t.Error("expired archive companion (.sha256) should have been removed")
	}
	if _, err := os.Stat(filepath.Join(tmp, "cache", "java-17.zip")); err != nil {
		t.Error("fresh archive should have been kept")
	}

	// forever policy: nothing removed.
	app.cfg.Core.ArchiveRetention = "forever"
	foreverArchive := filepath.Join(tmp, "cache", "llvm-20.0.0.tar.xz")
	writeSizedFile(t, foreverArchive, 300)
	setModTime(t, foreverArchive, old)
	if removed := app.CleanupExpiredArchives(); removed != 0 {
		t.Errorf("CleanupExpiredArchives(forever) = %d, want 0", removed)
	}
	if _, err := os.Stat(foreverArchive); err != nil {
		t.Error("archive should be kept under forever policy")
	}

	// immediate policy: nothing removed (cleanup is install-time only).
	app.cfg.Core.ArchiveRetention = "immediate"
	if removed := app.CleanupExpiredArchives(); removed != 0 {
		t.Errorf("CleanupExpiredArchives(immediate) = %d, want 0", removed)
	}
}

// TestCleanupExpiredArchives_SkipsMetadata verifies metadata files are never
// swept even when old.
func TestCleanupExpiredArchives_SkipsMetadata(t *testing.T) {
	app, tmp := retentionApp(t)
	app.cfg.Core.ArchiveRetention = "7d"

	old := time.Now().Add(-10 * 24 * time.Hour)
	for _, name := range []string{"manifest-cache.dat", "sniff-cache.json", "sniff-cache.json.lock"} {
		p := filepath.Join(tmp, "cache", name)
		writeSizedFile(t, p, 10)
		setModTime(t, p, old)
	}

	if removed := app.CleanupExpiredArchives(); removed != 0 {
		t.Errorf("CleanupExpiredArchives() = %d, want 0 (metadata must be kept)", removed)
	}
	for _, name := range []string{"manifest-cache.dat", "sniff-cache.json", "sniff-cache.json.lock"} {
		if _, err := os.Stat(filepath.Join(tmp, "cache", name)); err != nil {
			t.Errorf("metadata %s should not be swept: %v", name, err)
		}
	}
}

// TestArchiveRetentionGUIBindings verifies the Wails-facing get/set methods.
func TestArchiveRetentionGUIBindings(t *testing.T) {
	app, _ := retentionApp(t)

	if got := app.GetArchiveRetention(); got != "immediate" {
		t.Errorf("GetArchiveRetention() = %q, want %q", got, "immediate")
	}
	if err := app.SetArchiveRetention("30d"); err != nil {
		t.Fatalf("SetArchiveRetention(30d) failed: %v", err)
	}
	if got := app.GetArchiveRetention(); got != "30d" {
		t.Errorf("GetArchiveRetention() = %q, want %q", got, "30d")
	}
	if err := app.SetArchiveRetention("bogus"); err == nil {
		t.Error("SetArchiveRetention(bogus) should fail")
	}
	if got := app.GetArchiveRetention(); got != "30d" {
		t.Errorf("policy should be unchanged after invalid Set, got %q", got)
	}
}
