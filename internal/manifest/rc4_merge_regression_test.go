package manifest

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
	"time"
)

// Exercise the merged production loader, not just the version comparison helper.
func TestRC4CatalogPromotionUsesToolSchemeAndTrustedChannel(t *testing.T) {
	for _, tc := range []struct {
		name, tool, current, candidate string
		stable                         bool
		want                           string
	}{
		{"ndk revision", "android-ndk", "r29", "r29b", true, "r29b"},
		{"ndk next release", "android-ndk", "r29", "30", true, "30"},
		{"unknown ordinary label", "demo", "r29", "30", false, "r29"},
		{"stable label", "demo", "1.0.0", "2.0.0-rc.1", true, "1.0.0"},
		{"stable metadata", "demo", "1.0.0-rc.1", "2.0.0-rc.1", true, "1.0.0-rc.1"},
		{"cross channel", "demo", "1.0.0-rc.1", "2.0.0-beta.1", false, "1.0.0-rc.1"},
		{"same channel", "demo", "1.0.0-rc.1", "2.0.0-rc.1", false, "2.0.0-rc.1"},
		{"graduate", "demo", "1.0.0-rc.1", "1.0.0", false, "1.0.0"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			archive := []Archive{{OS: "any", Arch: "any", URL: "https://trusted.test/tool.zip", Format: "zip"}}
			base := &Tool{Name: tc.tool, Latest: tc.current, VersionList: []Version{{Version: tc.current, Stable: tc.stable, Archives: archive}}}
			data, err := MarshalTool(base)
			if err != nil {
				t.Fatal(err)
			}
			setCatalogTestEmbedded(t, fstest.MapFS{"tool.yaml": &fstest.MapFile{Data: data}})
			dir, cache := t.TempDir(), t.TempDir()
			// The winning disk source uses a different file name and nested origin.
			writeCatalogTestTool(t, filepath.Join(dir, "third-party", "vendor.yaml"), tc.tool, tc.candidate, time.Now())
			got, _ := checkCatalogCache(t, dir, cache).Tool(tc.tool)
			if got.DefaultVersion() != tc.want {
				t.Fatalf("default=%q want=%q", got.DefaultVersion(), tc.want)
			}
		})
	}
}

func TestRC4CatalogCacheBindsActualSourceRoot(t *testing.T) {
	setCatalogTestEmbedded(t, nil)
	first, second, cache := t.TempDir(), t.TempDir(), t.TempDir()
	stamp := time.Unix(1700000000, 0)
	for _, dir := range []string{first, second} {
		writeCatalogTestTool(t, filepath.Join(dir, "third-party", "same.yaml"), "same", "1.0.0", stamp)
	}
	fingerprint := func(dir string) string {
		t.Helper()
		if _, err := LoadCatalogCached(dir, cache); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(filepath.Join(cache, "manifest-cache.dat"))
		if err != nil {
			t.Fatal(err)
		}
		var entry catalogCacheEntry
		if err := json.Unmarshal(data, &entry); err != nil {
			t.Fatal(err)
		}
		if entry.Schema != 2 {
			t.Fatalf("schema=%d", entry.Schema)
		}
		return entry.Fingerprint
	}
	if fingerprint(first) == fingerprint(second) {
		t.Fatal("same bytes from different actual roots reused source identity")
	}
}
