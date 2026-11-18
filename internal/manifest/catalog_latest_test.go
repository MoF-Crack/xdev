package manifest

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"testing/fstest"
	"time"
)

func TestCatalogLatest_EmbeddedFirstPromotion(t *testing.T) {
	archive := Archive{OS: "any", Arch: "any", URL: "https://trusted.test/tool.zip", Format: "zip", SHA256: "trusted-hash", Size: 42}
	version := func(v string) Version {
		return Version{Version: v, Stable: !IsPrerelease(v), Archives: []Archive{archive}}
	}
	for _, tc := range []struct {
		name, current, candidate                       string
		missingCurrent, placeholder, shadowPlaceholder bool
		want                                           string
	}{
		{name: "new stable", current: "1.0.0", candidate: "2.0.0", want: "2.0.0"},
		{name: "downgrade", current: "2.0.0", candidate: "1.0.0", want: "2.0.0"},
		{name: "same trusted version", current: "1.0.0", candidate: "1.0.0", want: "1.0.0"},
		{name: "missing candidate", current: "1.0.0", candidate: "5.0.0", want: "1.0.0"},
		{name: "placeholder", current: "1.0.0", candidate: "2.0.0", placeholder: true, want: "1.0.0"},
		{name: "embedded placeholder cannot be filled by disk", current: "1.0.0", candidate: "2.0.0", shadowPlaceholder: true, want: "1.0.0"},
		{name: "stable rejects prerelease", current: "1.0.0", candidate: "2.0.0-rc.1", want: "1.0.0"},
		{name: "same prerelease channel", current: "2.0.0-rc.1", candidate: "2.0.0-rc.2", want: "2.0.0-rc.2"},
		{name: "different prerelease channel", current: "2.0.0-rc.1", candidate: "3.0.0-alpha.1", want: "2.0.0-rc.1"},
		{name: "prerelease graduates", current: "2.0.0-rc.1", candidate: "2.0.0", want: "2.0.0"},
		{name: "four segment release", current: "2026.2", candidate: "2026.2.0.1", want: "2026.2.0.1"},
		{name: "numeric release", current: "21", candidate: "22", want: "22"},
		{name: "opaque current", current: "r29", candidate: "1", want: "r29"},
		{name: "opaque candidate", current: "1", candidate: "999garbage", want: "1"},
		{name: "missing current rejects downgrade", current: "3.0.0", candidate: "2.0.0", missingCurrent: true, want: "3.0.0"},
		{name: "missing current permits upgrade", current: "3.0.0", candidate: "4.0.0", missingCurrent: true, want: "4.0.0"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			base := &Tool{Name: "demo", DisplayName: "Trusted", Latest: tc.current, VersionList: []Version{version(tc.current)}}
			if tc.missingCurrent {
				base.VersionList = nil
			}
			if tc.shadowPlaceholder {
				base.VersionList = append(base.VersionList, Version{Version: tc.candidate})
			}
			data, err := MarshalTool(base)
			if err != nil {
				t.Fatal(err)
			}
			setCatalogTestEmbedded(t, fstest.MapFS{"demo.yaml": &fstest.MapFile{Data: data}})
			trusted, err := loadFromEmbedded()
			if err != nil {
				t.Fatal(err)
			}
			trustedTool, _ := trusted.Tool("demo")
			disk := &Tool{Name: "demo", DisplayName: "Untrusted", Latest: tc.candidate, VersionList: []Version{version(tc.candidate)}}
			if tc.name == "missing candidate" {
				disk.VersionList = nil
			}
			if tc.placeholder {
				disk.VersionList[0].Archives = nil
			}
			// Attempt to replace all trusted fields of the existing release.
			disk.VersionList = append(disk.VersionList, Version{Version: tc.current, Unverified: true, Archives: []Archive{{OS: "any", Arch: "any", URL: "https://evil.test/override.zip", Format: "zip", SHA256: "evil"}}})
			data, err = MarshalTool(disk)
			if err != nil {
				t.Fatal(err)
			}
			dir, cache := t.TempDir(), t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "demo.yaml"), data, 0o644); err != nil {
				t.Fatal(err)
			}
			got, _ := checkCatalogCache(t, dir, cache).Tool("demo")
			if got.Latest != tc.want || got.DefaultVersion() != tc.want {
				t.Fatalf("latest/default = %q/%q, want %q", got.Latest, got.DefaultVersion(), tc.want)
			}
			if got.DisplayName != "Trusted" {
				t.Fatal("disk overwrote embedded metadata")
			}
			if !tc.missingCurrent {
				v, _ := got.FindVersion(tc.current)
				want, _ := trustedTool.FindVersion(tc.current)
				if !reflect.DeepEqual(v, want) {
					t.Fatalf("embedded version replaced: %+v", v)
				}
			}
		})
	}
}

func TestCatalogLatest_DuplicateSourcesRemainEmbeddedFirst(t *testing.T) {
	setCatalogTestEmbedded(t, fstest.MapFS{"demo.yaml": &fstest.MapFile{Data: []byte("name: demo\nlatest: 1.0.0\nversions:\n  - version: 1.0.0\n    archives:\n      - os: any\n        arch: any\n        url: https://trusted.test/1.zip\n        format: zip\n        sha256: trusted\n")}})
	dir, cache := t.TempDir(), t.TempDir()
	stamp := time.Unix(1_700_000_100, 0)
	top := filepath.Join(dir, "demo.yaml")
	thirdParty := filepath.Join(dir, "third-party", "demo.yaml")
	writeCatalogTestTool(t, top, "demo", "2.0.0", stamp)
	writeCatalogTestTool(t, thirdParty, "demo", "3.0.0", stamp)
	requireCatalogVersion(t, checkCatalogCache(t, dir, cache), "demo", "3.0.0")
	writeCatalogTestTool(t, top, "demo", "4.0.0", stamp.Add(time.Second))
	requireCatalogVersion(t, checkCatalogCache(t, dir, cache), "demo", "3.0.0")
	if err := os.Remove(thirdParty); err != nil {
		t.Fatal(err)
	}
	cat := checkCatalogCache(t, dir, cache)
	requireCatalogVersion(t, cat, "demo", "4.0.0")
	tool, _ := cat.Tool("demo")
	trusted, ok := tool.FindVersion("1.0.0")
	if !ok || trusted.Archives[0].SHA256 != "trusted" {
		t.Fatal("lost embedded archive")
	}
}

func TestCatalogLatest_PreservesEffectiveFallbackDefault(t *testing.T) {
	for _, lts := range []string{"", "2.0.0"} {
		t.Run("lts="+lts, func(t *testing.T) {
			base := &Tool{Name: "demo", LTS: lts, VersionList: []Version{{Version: "2.0.0", Archives: []Archive{{OS: "any", Arch: "any", URL: "https://trusted.test/2.zip", Format: "zip"}}}}}
			data, err := MarshalTool(base)
			if err != nil {
				t.Fatal(err)
			}
			setCatalogTestEmbedded(t, fstest.MapFS{"demo.yaml": &fstest.MapFile{Data: data}})
			dir, cache := t.TempDir(), t.TempDir()
			// An unadvertised newer version must not become the implicit first default.
			disk := &Tool{Name: "demo", Latest: "1.0.0", VersionList: []Version{{Version: "9.0.0"}, {Version: "1.0.0", Archives: base.VersionList[0].Archives}}}
			data, err = MarshalTool(disk)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "demo.yaml"), data, 0o644); err != nil {
				t.Fatal(err)
			}
			got, _ := checkCatalogCache(t, dir, cache).Tool("demo")
			if got.DefaultVersion() != "2.0.0" {
				t.Fatalf("default = %q", got.DefaultVersion())
			}
		})
	}
}
