package plugin

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/xmdemt/xdev/internal/manifest"
)

func sampleTool() *manifest.Tool {
	return &manifest.Tool{
		Name:        "demo",
		DisplayName: "Demo Tool",
		Description: "Test-only manifest.",
		Homepage:    "https://example.com",
		Category:    "language",
		Shims:       []string{"demo", "democtl"},
		EnvVars: map[string]string{
			"DEMO_HOME": "{{install}}",
		},
		Latest: "1.2.0",
		VersionList: []manifest.Version{
			{
				Version: "1.2.0",
				Archives: []manifest.Archive{
					{OS: "any", Arch: "any", URL: "https://example.com/demo-1.2.0.zip", Format: "zip", Size: 1000},
				},
			},
			{
				Version: "1.1.0",
				Archives: []manifest.Archive{
					{OS: "any", Arch: "any", URL: "https://example.com/demo-1.1.0.zip", Format: "zip", Size: 900},
				},
			},
		},
	}
}

func TestGenericExposesManifestFields(t *testing.T) {
	p := NewGeneric(sampleTool())
	if p.Name() != "demo" {
		t.Errorf("Name = %q, want demo", p.Name())
	}
	if p.DisplayName() != "Demo Tool" {
		t.Errorf("DisplayName = %q", p.DisplayName())
	}
	if p.Category() != "language" {
		t.Errorf("Category = %q", p.Category())
	}
	if got := strings.Join(p.Shims(), ","); got != "demo,democtl" {
		t.Errorf("Shims = %q", got)
	}
	if p.DefaultVersion() != "1.2.0" {
		t.Errorf("DefaultVersion = %q, want 1.2.0", p.DefaultVersion())
	}
}

func TestGenericSetManifestSwapsVersionList(t *testing.T) {
	p := NewGeneric(sampleTool())
	p.SetManifest(&manifest.Tool{
		Name:   "demo",
		Latest: "9.9.9",
		VersionList: []manifest.Version{
			{Version: "9.9.9", Unverified: true},
		},
	})
	got, err := p.ResolveVersion("9.9.9")
	if err != nil {
		t.Fatalf("ResolveVersion after SetManifest: %v", err)
	}
	if got != "9.9.9" {
		t.Errorf("ResolveVersion = %q, want 9.9.9", got)
	}
}

func TestGenericInstallDir(t *testing.T) {
	p := NewGeneric(sampleTool())
	got, err := p.InstallDir("/opt/xdev/install", "1.2.0")
	if err != nil {
		t.Fatalf("InstallDir: %v", err)
	}
	want := filepath.Join("/opt/xdev/install", "demo", "1.2.0")
	if got != want {
		t.Errorf("InstallDir = %q, want %q", got, want)
	}
}

func TestGenericEnvVarsInterpolatesInstall(t *testing.T) {
	p := NewGeneric(sampleTool())
	env := p.EnvVars("/opt/xdev/install/demo/1.2.0")
	if env["DEMO_HOME"] != "/opt/xdev/install/demo/1.2.0" {
		t.Errorf("DEMO_HOME = %q, want interpolated path", env["DEMO_HOME"])
	}
}

func TestGenericArchiveForPicksCurrentOS(t *testing.T) {
	p := NewGeneric(sampleTool())
	a, err := p.ArchiveFor("1.2.0")
	if err != nil {
		t.Fatalf("ArchiveFor: %v", err)
	}
	if a.URL != "https://example.com/demo-1.2.0.zip" {
		t.Errorf("URL = %q", a.URL)
	}
}

func TestGenericArchiveForUnknownVersion(t *testing.T) {
	p := NewGeneric(sampleTool())
	_, err := p.ArchiveFor("9.9.9")
	if err == nil {
		t.Errorf("expected error for unknown version")
	}
}

func TestGenericArchiveForSniffedVersionWithoutArchives(t *testing.T) {
	m := sampleTool()
	m.VersionList = append(m.VersionList, manifest.Version{
		Version:    "9.9.9",
		Unverified: true,
		Discovered: true,
	})
	p := NewGeneric(m)
	_, err := p.ArchiveFor("9.9.9")
	if err == nil {
		t.Fatal("expected error when sniffed version has no archives")
	}
	if !strings.Contains(err.Error(), "could not derive an archive URL") {
		t.Errorf("error = %q, want archive-derivation hint", err)
	}
}

// TestGenericArchiveForPicksFormCarryingArchive (Spec 028 TC-A7)
// 生产链 smoke：Generic.ArchiveFor → Version.ArchiveForCurrent 无死递归；
// legacy 语义下带 Form 字段的 exact os+arch archive 仍被选中。
func TestGenericArchiveForPicksFormCarryingArchive(t *testing.T) {
	m := sampleTool()
	m.VersionList[0].Archives[0].Form = "gui"
	p := NewGeneric(m)
	a, err := p.ArchiveFor("1.2.0")
	if err != nil {
		t.Fatalf("ArchiveFor: %v", err)
	}
	if a.URL != "https://example.com/demo-1.2.0.zip" {
		t.Errorf("URL = %q, want demo archive (Form ignored by legacy path)", a.URL)
	}
}

func TestRegistryRegisterAndGet(t *testing.T) {
	r := NewRegistry()
	p := NewGeneric(sampleTool())
	r.Register(p)
	got, ok := r.Get("demo")
	if !ok {
		t.Fatal("expected demo in registry")
	}
	if got.Name() != "demo" {
		t.Errorf("Name = %q", got.Name())
	}
}

func TestRegistrySearch(t *testing.T) {
	r := NewRegistry()
	r.Register(NewGeneric(sampleTool()))
	matches := r.Search("DEMO") // case insensitive
	if len(matches) != 1 {
		t.Errorf("expected 1 match, got %d", len(matches))
	}
}

func TestRegistryNamesSorted(t *testing.T) {
	r := NewRegistry()
	t1 := &manifest.Tool{Name: "z", Shims: []string{"z"}, VersionList: []manifest.Version{{Version: "1"}}}
	t2 := &manifest.Tool{Name: "a", Shims: []string{"a"}, VersionList: []manifest.Version{{Version: "1"}}}
	t3 := &manifest.Tool{Name: "m", Shims: []string{"m"}, VersionList: []manifest.Version{{Version: "1"}}}
	r.Register(NewGeneric(t1))
	r.Register(NewGeneric(t2))
	r.Register(NewGeneric(t3))
	names := r.Names()
	want := []string{"a", "m", "z"}
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Errorf("Names = %v, want %v", names, want)
	}
}
