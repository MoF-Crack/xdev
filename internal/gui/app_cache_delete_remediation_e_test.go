package gui

import (
	"errors"
	"github.com/xmdemt/xdev/internal/downloader"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/xmdemt/xdev/internal/util"
)

func TestRemediationE_GUICleanupProtectsBusyGeneration(t *testing.T) {
	for _, mode := range []string{"all", "partFiles", "per-file", "archive-companions", "immediate", "expired", "settings"} {
		t.Run(mode, func(t *testing.T) {
			app, tmp := retentionApp(t)
			dest := filepath.Join(tmp, "cache", "demo-1.zip")
			p := dest + ".0123456789abcdef.part"
			writeSizedFile(t, p, 4)
			writeSizedFile(t, p+".meta", 4)
			lock, err := util.AcquireFileLock(p+".lock", 0)
			if err != nil {
				t.Fatal(err)
			}
			defer lock.Release()
			switch mode {
			case "immediate":
				err = app.applyArchiveRetention("demo", "1", "zip")
			case "expired":
				writeSizedFile(t, dest, 3)
				setModTime(t, dest, time.Now().Add(-9*24*time.Hour))
				app.cfg.Core.ArchiveRetention = "7d"
				_, err = app.cleanupExpiredArchives()
			case "settings":
				err = app.CleanCache("partFiles")
			case "per-file":
				err = app.DeleteCacheFile(p)
			case "archive-companions":
				err = app.DeleteCacheFile(dest)
			default:
				err = app.CleanCacheCategory(mode)
			}
			if !errors.Is(err, downloader.ErrPartialBusy) {
				t.Errorf("GUI must expose busy/partial outcome: %v", err)
			}
			for _, path := range []string{p, p + ".meta", p + ".lock"} {
				if _, err := os.Stat(path); err != nil {
					t.Errorf("busy generation changed: %s: %v", path, err)
				}
			}
		})
	}
}

func TestRemediationE_CleanupReportCounts(t *testing.T) {
	app, root := retentionApp(t)
	dir := filepath.Join(root, "cache")
	busy := filepath.Join(dir, "busy.zip.part")
	writeSizedFile(t, busy, 1)
	lock, err := util.AcquireFileLock(busy+".lock", 0)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Release()
	writeSizedFile(t, filepath.Join(dir, "complete.zip"), 1)
	if err := os.Mkdir(filepath.Join(dir, "broken.zip.part"), 0700); err != nil {
		t.Fatal(err)
	}
	report := app.CleanCacheCategoryResult("all")
	if report.Removed != 1 || report.Busy != 1 || report.Failed != 1 || report.Detail == "" {
		t.Fatalf("expected mixed outcome, got %+v", report)
	}
	archive := filepath.Join(dir, "second.zip")
	writeSizedFile(t, archive, 1)
	writeSizedFile(t, archive+".sha256", 1)
	if r := app.DeleteCacheFileResult(archive); r.Removed != 2 || r.Failed != 0 || r.Busy != 0 || r.Detail != "" {
		t.Fatalf("per-file result: %+v", r)
	}
	if r := app.DeleteCacheFileResult(archive); r != (CacheCleanupReport{}) {
		t.Fatalf("already absent: %+v", r)
	}
	if r := app.CleanCacheCategoryResult("invalid"); r.Failed != 1 || r.Detail == "" {
		t.Fatalf("invalid request: %+v", r)
	}
}

func TestRemediationE_GUIPartCategoryCleansOrphanMeta(t *testing.T) {
	app, tmp := retentionApp(t)
	cache := filepath.Join(tmp, "cache")
	p := filepath.Join(cache, "demo-1.zip.0123456789abcdef.part")
	writeSizedFile(t, p+".meta", 4)
	if err := app.CleanCacheCategory("partFiles"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(p + ".meta"); !os.IsNotExist(err) {
		t.Errorf("orphan generation meta survives part cleanup: %v", err)
	}
}
