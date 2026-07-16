package manifest

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
)

func TestLoadCatalogFallsBackToRepoWhenEmpty(t *testing.T) {
	tmp := t.TempDir()
	// tmp/manifests exists but is empty.
	empty := filepath.Join(tmp, "manifests")
	if err := os.MkdirAll(empty, 0o755); err != nil {
		t.Fatal(err)
	}
	cat, err := LoadCatalog(empty)
	if err != nil {
		t.Fatalf("LoadCatalog: %v", err)
	}
	// The fallback path is the in-repo manifests/ directory; in
	// this dev checkout it must contain at least python, node, go,
	// java, rust, flutter.
	want := []string{"flutter", "go", "java", "node", "python", "rust"}
	for _, name := range want {
		if _, ok := cat.Tool(name); !ok {
			t.Errorf("missing tool %q in fallback catalog: %v", name, toolNames(cat))
		}
	}
}

func TestLoadCatalogFallsBackWhenDirMissing(t *testing.T) {
	// dir does not exist at all - same fallback.
	cat, err := LoadCatalog(filepath.Join(t.TempDir(), "nope"))
	if err != nil {
		t.Fatalf("LoadCatalog: %v", err)
	}
	if _, ok := cat.Tool("python"); !ok {
		t.Errorf("expected fallback to repo manifests, got %v", toolNames(cat))
	}
}

func TestLoadCatalogFromExplicitDir(t *testing.T) {
	// User supplies a directory with a single hand-written manifest.
	dir := t.TempDir()
	yaml := `name: mytool
display_name: My Tool
description: For testing
homepage: https://example.com
category: language
shims:
  - mytool
versions:
  - version: "1.0.0"
    stable: true
    archives:
      - os: windows
        arch: amd64
        format: zip
        url: https://example.com/mytool-1.0.0.zip
        sha256: 0000000000000000000000000000000000000000000000000000000000000000
        size: 1000
        strip: 0
`
	if err := os.WriteFile(filepath.Join(dir, "mytool.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	cat, err := LoadCatalog(dir)
	if err != nil {
		t.Fatalf("LoadCatalog: %v", err)
	}
	t1, ok := cat.Tool("mytool")
	if !ok {
		t.Fatalf("expected mytool in catalog, got %v", toolNames(cat))
	}
	if t1.DisplayName != "My Tool" {
		t.Errorf("DisplayName = %q, want %q", t1.DisplayName, "My Tool")
	}
}

func toolNames(c *Catalog) []string {
	tools := c.Tools()
	out := make([]string, 0, len(tools))
	for _, t := range tools {
		out = append(out, t.Name)
	}
	sort.Strings(out)
	return out
}

func TestArchiveForCurrentPicksExactOS(t *testing.T) {
	v := &Version{
		Version: "1.0.0",
		Archives: []Archive{
			{OS: "linux", Arch: "x86_64", URL: "linux-x64", Format: "tar.gz"},
			{OS: "windows", Arch: "amd64", URL: "win-amd64", Format: "zip"},
			{OS: "darwin", Arch: "arm64", URL: "mac-arm64", Format: "tar.gz"},
		},
	}
	a, ok := v.ArchiveForCurrent("windows", "amd64")
	if !ok {
		t.Fatal("expected to find windows/amd64 archive")
	}
	if a.URL != "win-amd64" {
		t.Errorf("URL = %q, want win-amd64", a.URL)
	}
	a, ok = v.ArchiveForCurrent("darwin", "arm64")
	if !ok {
		t.Fatal("expected to find darwin/arm64 archive")
	}
	if a.URL != "mac-arm64" {
		t.Errorf("URL = %q, want mac-arm64", a.URL)
	}
}

func TestArchiveForCurrentFallsBackToAnyOS(t *testing.T) {
	v := &Version{
		Version: "1.0.0",
		Archives: []Archive{
			{OS: "any", Arch: "any", URL: "universal", Format: "tar.gz"},
		},
	}
	for _, plat := range [][2]string{{"windows", "amd64"}, {"linux", "x86_64"}, {"darwin", "arm64"}} {
		a, ok := v.ArchiveForCurrent(plat[0], plat[1])
		if !ok {
			t.Errorf("expected to find 'any' archive for %s/%s", plat[0], plat[1])
			continue
		}
		if a.URL != "universal" {
			t.Errorf("%s/%s picked %q", plat[0], plat[1], a.URL)
		}
	}
}

func TestDefaultVersionPicksLatest(t *testing.T) {
	t1 := &Tool{
		Latest: "2.0.0",
		LTS:    "1.0.0",
		VersionList: []Version{
			{Version: "1.0.0"},
			{Version: "2.0.0"},
		},
	}
	if got := t1.DefaultVersion(); got != "2.0.0" {
		t.Errorf("DefaultVersion = %q, want 2.0.0", got)
	}
	t2 := &Tool{
		Latest: "",
		LTS:    "1.0.0",
		VersionList: []Version{
			{Version: "1.0.0"},
		},
	}
	if got := t2.DefaultVersion(); got != "1.0.0" {
		t.Errorf("DefaultVersion = %q, want 1.0.0", got)
	}
}

// --- EffectiveKind ---

// TestEffectiveKind_DefaultIsArchive asserts that an Archive with no
// explicit InstallKind is treated as a plain extractable archive.
func TestEffectiveKind_DefaultIsArchive(t *testing.T) {
	a := Archive{}
	if got := a.EffectiveKind(); got != InstallKindArchive {
		t.Errorf("empty InstallKind: EffectiveKind() = %q, want %q", got, InstallKindArchive)
	}
}

func TestEffectiveKind_ExplicitArchive(t *testing.T) {
	a := Archive{InstallKind: InstallKindArchive}
	if got := a.EffectiveKind(); got != InstallKindArchive {
		t.Errorf("explicit archive: EffectiveKind() = %q, want %q", got, InstallKindArchive)
	}
}

func TestEffectiveKind_Executable(t *testing.T) {
	a := Archive{InstallKind: InstallKindExecutable}
	if got := a.EffectiveKind(); got != InstallKindExecutable {
		t.Errorf("executable: EffectiveKind() = %q, want %q", got, InstallKindExecutable)
	}
}

func TestEffectiveKind_Script(t *testing.T) {
	a := Archive{InstallKind: InstallKindScript}
	if got := a.EffectiveKind(); got != InstallKindScript {
		t.Errorf("script: EffectiveKind() = %q, want %q", got, InstallKindScript)
	}
}

// TestEffectiveKind_UnknownFallsBackToArchive enforces the trust model:
// an unrecognised kind must never silently become executable/script
// (those run untrusted code). The safe default is always "archive"
// which only extracts, never executes.
func TestEffectiveKind_UnknownFallsBackToArchive(t *testing.T) {
	a := Archive{InstallKind: "bogus"}
	if got := a.EffectiveKind(); got != InstallKindArchive {
		t.Errorf("unknown kind %q: EffectiveKind() = %q, want %q (safe default)", a.InstallKind, got, InstallKindArchive)
	}
}
