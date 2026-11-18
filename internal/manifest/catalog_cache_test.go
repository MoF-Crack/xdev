package manifest

import (
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"testing/fstest"
	"time"
)

func setCatalogTestEmbedded(t *testing.T, efs fs.FS) {
	t.Helper()
	previous := embeddedFS
	embeddedFS = efs
	t.Cleanup(func() { embeddedFS = previous })
}

func writeCatalogTestTool(t *testing.T, path, name, version string, stamp time.Time) {
	t.Helper()
	data, err := MarshalTool(&Tool{
		Name: name, DisplayName: name, Latest: version,
		VersionList: []Version{{Version: version, Archives: []Archive{{
			OS: "any", Arch: "any", URL: "https://example.test/" + version + ".zip", Format: "zip",
		}}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, stamp, stamp); err != nil {
		t.Fatal(err)
	}
}

// Compare uncached, invalidated and warm reads, and prove the unchanged-source
// read actually uses the cache instead of silently rewriting it every time.
func checkCatalogCache(t *testing.T, dir, cache string) *Catalog {
	t.Helper()
	cold, err := LoadCatalog(dir)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadCatalogCached(dir, cache)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(loaded.Tools(), cold.Tools()) {
		t.Fatalf("cached catalog differs from cold load: cached=%v cold=%v", loaded.Tools(), cold.Tools())
	}
	cachePath := filepath.Join(cache, "manifest-cache.dat")
	stamp := time.Unix(1_700_000_000, 0)
	if err := os.Chtimes(cachePath, stamp, stamp); err != nil {
		t.Fatal(err)
	}
	warm, err := LoadCatalogCached(dir, cache)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(warm.Tools(), cold.Tools()) {
		t.Fatal("warm catalog differs from cold load")
	}
	info, err := os.Stat(cachePath)
	if err != nil {
		t.Fatal(err)
	}
	if !info.ModTime().Equal(stamp) {
		t.Fatal("unchanged sources rewrote the cache instead of hitting it")
	}
	return warm
}

func requireCatalogVersion(t *testing.T, cat *Catalog, name, version string) {
	t.Helper()
	tool, ok := cat.Tool(name)
	if version == "" {
		if ok {
			t.Fatalf("removed tool %q remains in catalog", name)
		}
		return
	}
	if !ok {
		t.Fatalf("tool %q missing from catalog", name)
	}
	if got := tool.DefaultVersion(); got != version {
		t.Fatalf("%s default = %q, want %q", name, got, version)
	}
	if v, ok := tool.FindVersion(version); !ok || len(v.Archives) != 1 {
		t.Fatalf("%s@%s archive not loaded", name, version)
	}
}

func TestCatalogCache_SourceAddUpdateRemove(t *testing.T) {
	for _, mode := range []string{"disk-only", "embedded-and-disk"} {
		t.Run(mode, func(t *testing.T) {
			var efs fs.FS
			if mode == "embedded-and-disk" {
				efs = fstest.MapFS{"builtin.yaml": &fstest.MapFile{Data: []byte("name: builtin\nlatest: 1.0.0\n")}}
			}
			setCatalogTestEmbedded(t, efs)
			for _, source := range []string{".", "third-party"} {
				t.Run(source, func(t *testing.T) {
					dir, cache := t.TempDir(), t.TempDir()
					stamp := time.Unix(1_700_000_100, 0)
					// Keep an explicit source when the added tool is removed.
					writeCatalogTestTool(t, filepath.Join(dir, "anchor.yaml"), "anchor", "1.0.0", stamp)
					requireCatalogVersion(t, checkCatalogCache(t, dir, cache), "added", "")
					path := filepath.Join(dir, source, "added.yaml")
					writeCatalogTestTool(t, path, "added", "1.0.0", stamp)
					requireCatalogVersion(t, checkCatalogCache(t, dir, cache), "added", "1.0.0")
					// Same-size update with an explicit new mtime: no sleeps or
					// dependence on filesystem timestamp resolution.
					writeCatalogTestTool(t, path, "added", "2.0.0", stamp.Add(time.Second))
					requireCatalogVersion(t, checkCatalogCache(t, dir, cache), "added", "2.0.0")
					if err := os.Remove(path); err != nil {
						t.Fatal(err)
					}
					requireCatalogVersion(t, checkCatalogCache(t, dir, cache), "added", "")
				})
			}
		})
	}
}

func TestCatalogCache_ThirdPartyOnlyControlsFallback(t *testing.T) {
	setCatalogTestEmbedded(t, nil)
	dir, cache := filepath.Join(t.TempDir(), "manifests"), t.TempDir()
	fallback := checkCatalogCache(t, dir, cache) // requested directory missing
	if len(fallback.Tools()) == 0 {
		t.Fatal("test requires the repository fallback manifests")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := checkCatalogCache(t, dir, cache); !reflect.DeepEqual(got.Tools(), fallback.Tools()) {
		t.Fatal("empty directory changed fallback catalog")
	}
	path := filepath.Join(dir, "third-party", "standalone.yaml")
	stamp := time.Unix(1_700_000_100, 0)
	writeCatalogTestTool(t, path, "standalone", "1.0.0", stamp)
	cat := checkCatalogCache(t, dir, cache)
	if len(cat.Tools()) != 1 {
		t.Fatalf("third-party-only source fell back to repository: %v", toolNames(cat))
	}
	requireCatalogVersion(t, cat, "standalone", "1.0.0")
	writeCatalogTestTool(t, path, "standalone", "2.0.0", stamp.Add(time.Second))
	requireCatalogVersion(t, checkCatalogCache(t, dir, cache), "standalone", "2.0.0")
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if got := checkCatalogCache(t, dir, cache); !reflect.DeepEqual(got.Tools(), fallback.Tools()) {
		t.Fatal("removing the last source did not restore repository fallback")
	}
}

func TestCatalogCache_DuplicateSourcePrecedence(t *testing.T) {
	for _, mode := range []string{"disk-only", "embedded-and-disk"} {
		t.Run(mode, func(t *testing.T) {
			var efs fs.FS
			if mode == "embedded-and-disk" {
				efs = fstest.MapFS{"builtin.yaml": &fstest.MapFile{Data: []byte("name: builtin\n")}}
			}
			setCatalogTestEmbedded(t, efs)
			dir, cache := t.TempDir(), t.TempDir()
			stamp := time.Unix(1_700_000_100, 0)
			top := filepath.Join(dir, "same.yaml")
			thirdParty := filepath.Join(dir, "third-party", "same.yaml")
			writeCatalogTestTool(t, top, "same", "1.0.0", stamp)
			requireCatalogVersion(t, checkCatalogCache(t, dir, cache), "same", "1.0.0")
			writeCatalogTestTool(t, thirdParty, "same", "2.0.0", stamp)
			requireCatalogVersion(t, checkCatalogCache(t, dir, cache), "same", "2.0.0")
			// A changed shadowed source must be visible after removing the winner.
			writeCatalogTestTool(t, top, "same", "3.0.0", stamp.Add(time.Second))
			requireCatalogVersion(t, checkCatalogCache(t, dir, cache), "same", "2.0.0")
			if err := os.Remove(thirdParty); err != nil {
				t.Fatal(err)
			}
			requireCatalogVersion(t, checkCatalogCache(t, dir, cache), "same", "3.0.0")
		})
	}
}

func TestCatalogFingerprint_SourceIdentityAndIgnoredFiles(t *testing.T) {
	dir := t.TempDir()
	stamp := time.Unix(1_700_000_100, 0)
	top := filepath.Join(dir, "same.yaml")
	writeCatalogTestTool(t, top, "same", "1.0.0", stamp)
	before := computeFingerprint(dir)
	thirdParty := filepath.Join(dir, "third-party", "same.yaml")
	writeCatalogTestTool(t, thirdParty, "same", "1.0.0", stamp)
	if err := os.Remove(top); err != nil {
		t.Fatal(err)
	}
	after := computeFingerprint(dir)
	if before == after {
		t.Fatal("moving an identical manifest to third-party did not change source identity")
	}
	for _, ignored := range []string{"backup/same.yaml", "third-party/nested/same.yaml", "same.yaml.bak", "third-party/same.yaml.sig", "third-party-repos.json"} {
		writeCatalogTestTool(t, filepath.Join(dir, filepath.FromSlash(ignored)), "ignored", "9.0.0", stamp)
	}
	if got := computeFingerprint(dir); got != after {
		t.Fatal("files excluded by the loader changed its fingerprint")
	}
	other := t.TempDir()
	writeCatalogTestTool(t, filepath.Join(other, "third-party", "same.yaml"), "same", "1.0.0", stamp)
	if computeFingerprint(other) == after {
		t.Fatal("different source roots with identical metadata share a fingerprint")
	}
}
