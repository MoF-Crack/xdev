package manifest

import "testing"

func TestD23_ArchiveSelectionSkipsPlaceholdersEveryPass(t *testing.T) {
	for _, form := range []string{"", "cli"} {
		passes := []Archive{
			{OS: "linux", Arch: "amd64", Form: form},
			{OS: "linux", Arch: "amd64"},
			{OS: "linux", Arch: "any", Form: form},
			{OS: "linux", Arch: "any"},
			{OS: "any", Arch: "any", Form: form},
			{OS: "any", Arch: "any"},
		}
		for i, base := range passes {
			t.Run(form+string(rune('1'+i)), func(t *testing.T) {
				good := base
				good.URL = "https://example.test/good"
				good.Format = "zip"
				for _, items := range [][]Archive{{base, good}, {good, base}} {
					v := Version{Archives: items}
					if !v.Installable("linux", "amd64") {
						t.Fatal("fixture should be installable")
					}
					a, found := v.ArchiveForCurrentWithForm("linux", "amd64", form)
					if !found || a == nil || !a.Installable() {
						t.Errorf("selected incomplete archive: %+v, %v", a, found)
					}
				}
			})
		}
	}
}

func TestD23_ArchiveSelectionRetainsFormAndPlatformPriority(t *testing.T) {
	items := []Archive{
		{OS: "any", Arch: "any", URL: "any-empty", Format: "zip"},
		{OS: "any", Arch: "any", Form: "cli", URL: "any-cli", Format: "zip"},
		{OS: "linux", Arch: "any", URL: "linux-empty", Format: "zip"},
		{OS: "linux", Arch: "any", Form: "cli", URL: "linux-cli", Format: "zip"},
		{OS: "linux", Arch: "x86_64", URL: "exact-empty", Format: "zip"},
		{OS: "linux", Arch: "x86_64", Form: "cli", URL: "exact-cli", Format: "zip"},
	}
	wants := []string{"exact-cli", "exact-empty", "linux-cli", "linux-empty", "any-cli", "any-empty"}
	for _, want := range wants {
		v := Version{Archives: items}
		a, ok := v.ArchiveForCurrentWithForm("linux", "amd64", "cli")
		if !ok || a.URL != want {
			t.Fatalf("want %s, got %+v (ok=%v)", want, a, ok)
		}
		for i := range items {
			if items[i].URL == want {
				items[i].URL = ""
			}
		}
	}
	v := Version{Archives: items}
	if a, ok := v.ArchiveForCurrent("linux", "amd64"); ok || a != nil {
		t.Fatalf("all placeholders should be unavailable, got %+v", a)
	}
	wrong := Version{Archives: []Archive{{OS: "any", Arch: "arm64", URL: "wrong-arch", Format: "zip"}}}
	if a, ok := wrong.ArchiveForCurrent("linux", "amd64"); ok {
		t.Fatalf("any OS must still match arch, got %+v", a)
	}
	npm := Version{Archives: []Archive{{OS: "any", Arch: "any", InstallKind: InstallKindNPM, Form: "cli"}}}
	if a, ok := npm.ArchiveForCurrentWithForm("windows", "amd64", "cli"); !ok || a.EffectiveKind() != InstallKindNPM {
		t.Fatalf("npm metadata exception lost: %+v", a)
	}
}

func TestD23_ShippedInstallableVersionsAlwaysSelectInstallableArchive(t *testing.T) {
	dir, ok := repoManifestDir()
	if !ok {
		t.Fatal("shipped corpus unavailable")
	}
	cat, err := loadFromDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	checked := 0
	for _, tool := range cat.Tools() {
		for _, v := range tool.VersionList {
			for _, osName := range []string{"windows", "linux", "darwin"} {
				for _, arch := range []string{"amd64", "x86_64", "arm64", "386"} {
					for _, form := range []string{"", "cli", "gui", "tui"} {
						exists := false
						for _, candidate := range v.Archives {
							platform := (candidate.OS == osName || candidate.OS == "any") && (candidate.Arch == "any" || CanonicalArch(osName, candidate.Arch) == CanonicalArch(osName, arch))
							if platform && candidate.Installable() && (form == "" || candidate.Form == "" || candidate.Form == form) {
								exists = true
								break
							}
						}
						a, found := v.ArchiveForCurrentWithForm(osName, arch, form)
						if found != exists || (found && (a == nil || !a.Installable())) {
							t.Errorf("%s@%s %s/%s form=%q: available=%v; selected=%+v, %v", tool.Name, v.Version, osName, arch, form, exists, a, found)
						}
						if form == "" {
							if v.Installable(osName, arch) != exists {
								t.Errorf("%s@%s %s/%s installability differs from corpus", tool.Name, v.Version, osName, arch)
							}
							if exists {
								checked++
							}
						}
					}
				}
			}
		}
	}
	if len(cat.Tools()) < 50 || checked < 100 {
		t.Fatalf("corpus guard underfilled: tools=%d platform versions=%d", len(cat.Tools()), checked)
	}
	t.Logf("checked %d tools and %d installable platform/version combinations", len(cat.Tools()), checked)
}
