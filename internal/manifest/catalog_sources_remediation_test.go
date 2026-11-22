package manifest

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
)

func remediationEmbedded(t *testing.T, mode string) {
	t.Helper()
	old := embeddedFS
	t.Cleanup(func() { embeddedFS = old })
	embeddedFS = nil
	var efs fs.FS
	switch mode {
	case "root":
		efs = fstest.MapFS{"base.yaml": &fstest.MapFile{Data: []byte("name: base\nlatest: '1'\n")}}
	case "nested":
		efs = fstest.MapFS{"manifests/base.yaml": &fstest.MapFile{Data: []byte("name: base\nlatest: '1'\n")}}
	}
	if efs != nil {
		InitEmbedded(efs)
	}
}

func remediationWrite(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestD21_CachedCatalogTracksThirdPartyContent(t *testing.T) {
	for _, mode := range []string{"none", "root", "nested"} {
		t.Run(mode, func(t *testing.T) {
			remediationEmbedded(t, mode)
			dir, cache := t.TempDir(), t.TempDir()
			remediationWrite(t, filepath.Join(dir, "anchor.yaml"), "name: anchor\nlatest: '1'\n")
			load := func() *Catalog {
				t.Helper()
				c, e := LoadCatalogCached(dir, cache)
				if e != nil {
					t.Fatal(e)
				}
				return c
			}
			load()
			tp := filepath.Join(dir, "third-party", "custom.yaml")
			remediationWrite(t, tp, "name: custom\ndisplay_name: Alpha\n")
			if _, ok := load().Tool("custom"); !ok {
				t.Error("new third-party manifest missing from cached catalog")
			}
			info, err := os.Stat(tp)
			if err != nil {
				t.Fatal(err)
			}
			remediationWrite(t, tp, "name: custom\ndisplay_name: Bravo\n")
			if err := os.Chtimes(tp, info.ModTime(), info.ModTime()); err != nil {
				t.Fatal(err)
			}
			c := load()
			if tool, ok := c.Tool("custom"); !ok || tool.DisplayName != "Bravo" {
				t.Errorf("same-size/same-mtime content replacement not visible: %v", tool)
			}
			if err := os.Remove(tp); err != nil {
				t.Fatal(err)
			}
			if _, ok := load().Tool("custom"); ok {
				t.Error("deleted third-party manifest remains cached")
			}
		})
	}
}

func TestD22_OnlyThirdPartyIsNotEmpty(t *testing.T) {
	for _, mode := range []string{"none", "root", "nested"} {
		t.Run(mode, func(t *testing.T) {
			remediationEmbedded(t, mode)
			dir := t.TempDir()
			path := filepath.Join(dir, "third-party", "custom.yaml")
			body := "name: only-custom\nlatest: '7'\n"
			remediationWrite(t, path, body)
			for _, cache := range []string{"", t.TempDir()} {
				cat, err := LoadCatalogCached(dir, cache)
				if err != nil {
					t.Fatal(err)
				}
				if _, ok := cat.Tool("only-custom"); !ok {
					t.Error("only-third-party source was treated as empty")
				}
			}
			data, err := os.ReadFile(path)
			if err != nil || string(data) != body {
				t.Errorf("source changed: %q, %v", data, err)
			}
		})
	}
}

func TestD21_SourceBoundaryAndPrecedence(t *testing.T) {
	remediationEmbedded(t, "none")
	dir, cache := t.TempDir(), t.TempDir()
	remediationWrite(t, filepath.Join(dir, "tool.yaml"), "name: tool\ndisplay_name: Top\n")
	remediationWrite(t, filepath.Join(dir, "third-party", "tool.yaml"), "name: tool\ndisplay_name: Third Party\n")
	remediationWrite(t, filepath.Join(dir, "backup", "stray.yaml"), "name: stray\n")
	remediationWrite(t, filepath.Join(dir, "third-party", "backup", "nested.yaml"), "name: nested\n")
	cat, err := LoadCatalogCached(dir, cache)
	if err != nil {
		t.Fatal(err)
	}
	if tool, ok := cat.Tool("tool"); !ok || tool.DisplayName != "Third Party" {
		t.Fatalf("third-party must win disk duplicate: %v", tool)
	}
	if len(cat.Tools()) != 1 {
		t.Fatalf("backup tree was loaded: %v", toolNames(cat))
	}
	before, err := os.ReadFile(filepath.Join(cache, "manifest-cache.dat"))
	if err != nil {
		t.Fatal(err)
	}
	remediationWrite(t, filepath.Join(dir, "backup", "stray.yaml"), "name: different\n")
	if _, err := LoadCatalogCached(dir, cache); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(filepath.Join(cache, "manifest-cache.dat"))
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Error("unloaded backup must not change catalog fingerprint")
	}
}

func TestD21_OldCacheSchemaRejected(t *testing.T) {
	remediationEmbedded(t, "none")
	dir, cache := t.TempDir(), t.TempDir()
	remediationWrite(t, filepath.Join(dir, "tool.yaml"), "name: tool\ndisplay_name: Correct\n")
	if _, err := LoadCatalogCached(dir, cache); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(cache, "manifest-cache.dat")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var entry map[string]any
	if err := json.Unmarshal(data, &entry); err != nil {
		t.Fatal(err)
	}
	delete(entry, "schema")
	entry["catalog"] = map[string]any{"tools": []any{map[string]any{"Name": "poisoned-old-schema"}}}
	data, err = json.Marshal(entry)
	if err != nil {
		t.Fatal(err)
	}
	remediationWrite(t, path, string(data))
	cat, err := LoadCatalogCached(dir, cache)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := cat.Tool("tool"); !ok {
		t.Error("cache without current schema was accepted")
	}
	if _, ok := cat.Tool("poisoned-old-schema"); ok {
		t.Error("old schema cache must be invalidated")
	}
}

func TestD22_UnreadableCollectionIsNotEmpty(t *testing.T) {
	for _, mode := range []string{"none", "root", "nested"} {
		t.Run(mode, func(t *testing.T) {
			remediationEmbedded(t, mode)
			dir := t.TempDir()
			remediationWrite(t, filepath.Join(dir, "third-party"), "not a directory")
			for _, cache := range []string{"", t.TempDir()} {
				if _, err := LoadCatalogCached(dir, cache); err == nil {
					t.Error("third-party enumeration failure silently treated as empty")
				}
			}
		})
	}
}

func TestD22_BadManifestDoesNotTriggerRepoFallback(t *testing.T) {
	for _, mode := range []string{"none", "root", "nested"} {
		t.Run(mode, func(t *testing.T) {
			remediationEmbedded(t, mode)
			dir, cache := t.TempDir(), t.TempDir()
			remediationWrite(t, filepath.Join(dir, "third-party", "broken.yaml"), "versions: [broken")
			cat, err := LoadCatalogCached(dir, cache)
			if mode == "none" {
				if err == nil {
					t.Error("unparseable third-party must produce a load error without embedded baseline")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(cat.Tools()) != 1 {
				t.Errorf("parse failure switched to unrelated repo sources: %v", toolNames(cat))
			}
		})
	}
}

func TestD18_MergedNDKUsesToolVersionScheme(t *testing.T) {
	old := embeddedFS
	t.Cleanup(func() { embeddedFS = old })
	InitEmbedded(fstest.MapFS{"android-ndk.yaml": &fstest.MapFile{Data: []byte("name: android-ndk\nversions:\n  - version: r29\n")}})
	dir := t.TempDir()
	remediationWrite(t, filepath.Join(dir, "android-ndk.yaml"), "name: android-ndk\nversions:\n  - version: 27.2.12479018\n  - version: r26\n")
	cat, err := LoadCatalog(dir)
	if err != nil {
		t.Fatal(err)
	}
	tool, _ := cat.Tool("android-ndk")
	if tool.VersionList[0].Version != "r29" {
		t.Errorf("NDK sorted top=%q; want r29", tool.VersionList[0].Version)
	}
}

func TestD18_UnknownVersionsCannotPromoteLatest(t *testing.T) {
	versions := []Version{{Version: "garbage-z", Archives: []Archive{{URL: "https://example.test/z", Format: "zip"}}}, {Version: "garbage-a"}}
	if CanPromoteLatest(versions, "garbage-z", "garbage-a", false) {
		t.Error("unknown version lexical order must not authorize a promotion")
	}
}
