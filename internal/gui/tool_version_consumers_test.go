package gui

import (
	"github.com/xmdemt/xdev/internal/manifest"
	"os"
	"path/filepath"
	"testing"
)

func TestD18_GUIRetainsNDKSnapshotAndRanksCatalog(t *testing.T) {
	const tool = "android-ndk"
	app := newB5App(t, t.TempDir())
	cfg := app.getCfg().Core
	for _, d := range []string{cfg.CacheDir, cfg.ManifestDir} {
		if err := os.MkdirAll(d, 0755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(cfg.ManifestDir, tool+".yaml"), []byte("name: android-ndk\nlatest: r29\nversions:\n  - version: 27.2.12479018\n  - version: r29\n  - version: unknown\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := app.reloadCatalog(); err != nil {
		t.Fatal(err)
	}
	linkCurrent(t, cfg.XMDevDir, filepath.Join(cfg.InstallDir, "android-sdk"), tool, "r26")
	for _, tc := range []struct {
		version string
		low     bool
	}{{"27", true}, {"26.0.123", false}} {
		st := app.checkDependency(manifest.Dependency{Tool: tool, MinVersion: tc.version})
		if st.Satisfied == tc.low || (tc.low && st.ReasonCode != "version_low") || st.ReasonCode == "version_unknown" {
			t.Errorf("dependency %s = %+v", tc.version, st)
		}
	}
	versions := app.GetAvailableVersions(tool)
	if len(versions) != 3 || versions[0].Version != "r29" || versions[2].Version != "unknown" {
		t.Fatalf("versions: %+v", versions)
	}
	cacheFile := filepath.Join(cfg.CacheDir, "sniff-cache.json")
	writeCache := func(text string) {
		t.Helper()
		if err := os.WriteFile(cacheFile, []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
	}
	writeCache(`{"tools":{"android-ndk":{"last_success":"2026-01-01T10:00:00Z","last_attempt":"2026-01-02T10:00:00Z","retry_after":"2026-01-02T11:00:00Z","status":"rate_limited","error":"HTTP 429","versions":["unknown","27.2.12479018","r30"]}}}`)
	got := app.GetAvailableUpdates()
	if len(got) != 1 {
		t.Fatalf("updates: %+v", got)
	}
	u := got[0]
	if u.CurrentVersion != "r26" || u.LatestVersion != "r30" || !u.Stale || u.RefreshError != "HTTP 429" || u.RefreshStatus != "rate_limited" || u.LastSuccess != "2026-01-01T10:00:00Z" || u.LastAttempt != "2026-01-02T10:00:00Z" || u.RetryAfter != "2026-01-02T11:00:00Z" {
		t.Fatalf("snapshot metadata: %+v", u)
	}
	if got := app.GetLatestVersionForTool(tool); got != "r30" {
		t.Fatal(got)
	}
	writeCache(`{"tools":{"android-ndk":{"last_sniff":"2026-01-01T10:00:00Z","versions":["r30"]}}}`)
	got = app.GetAvailableUpdates()
	if len(got) != 1 || got[0].Stale || got[0].LastSuccess != "2026-01-01T10:00:00Z" {
		t.Fatalf("legacy cache: %+v", got)
	}
	writeCache(`{"tools":{"android-ndk":{"versions":["broken9"]}}}`)
	if got := app.GetLatestVersionForTool(tool); got != "r29" {
		t.Fatalf("unknown list displaced catalog latest: %q", got)
	}
	if got := app.GetAvailableUpdates(); len(got) != 0 {
		t.Fatalf("unknown update: %+v", got)
	}
}
