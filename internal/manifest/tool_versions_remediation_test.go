package manifest

import "testing"

func TestD18_RealCatalogVersionsAreRecognized(t *testing.T) {
	dir, ok := repoManifestDir()
	if !ok {
		t.Fatal("missing shipped manifest corpus")
	}
	c, err := loadFromDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, tool := range c.Tools() {
		for _, v := range tool.Versions() {
			count++
			if !IsComparableToolVersion(tool.Name, v) {
				t.Errorf("unrecognized shipped release %s@%s", tool.Name, v)
			}
		}
	}
	ndk, ok := c.Tool("android-ndk")
	if !ok {
		t.Fatal("missing shipped Android NDK")
	}
	if latest, ok := NewestToolVersion(ndk.Name, ndk.Versions()); !ok || latest != ndk.Latest {
		t.Errorf("newest shipped NDK %q vs manifest latest %q", latest, ndk.Latest)
	}
	if count < 150 || len(c.Tools()) < 50 {
		t.Fatalf("incomplete shipped corpus: %d versions, %d tools", count, len(c.Tools()))
	}
	t.Logf("checked %d tools, %d releases", len(c.Tools()), count)
}

func TestD18_PromotionDoesNotUseDisplayTies(t *testing.T) {
	archive := []Archive{{URL: "example", Format: "zip"}}
	vs := []Version{{Version: "r27c", Release: "2030-01-01", Archives: archive}, {Version: "27.2.1", Archives: archive}, {Version: "r29", Archives: archive}, {Version: "unknown", Archives: archive}}
	for _, tc := range []struct {
		candidate, current string
		want               bool
	}{
		{"r27c", "27.2.1", false}, {"27.2.1", "r27c", false}, {"r29", "27.2.1", true}, {"r27c", "r29", false}, {"unknown", "", false}, {"r29", "unknown", false}, {"r27c", "r30", false},
	} {
		if got := CanPromoteToolLatest("android-ndk", vs, tc.candidate, tc.current, false); got != tc.want {
			t.Errorf("promote %s over %s: %v; want %v", tc.candidate, tc.current, got, tc.want)
		}
	}
	SortToolVersionsByRelease("android-ndk", vs)
	if vs[0].Version != "r29" || vs[1].Version != "r27c" || vs[3].Version != "unknown" {
		t.Fatalf("display order: %v", vs)
	}
}
