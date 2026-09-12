package installer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xmdemt/xdev/internal/config"
	"github.com/xmdemt/xdev/internal/manifest"
	"github.com/xmdemt/xdev/internal/plugin"
)

// Ticket 13 (audit-fixes / P0-02 + N-03 + N-04) — installer-side guards:
//   G7  the UnsignedVersion derivation (three states, no hash condition);
//   G8  same-name third-party downgrade guard;
//   G10 the TOFU write-back lands in the CONFIGURED manifest dir.

// G7: (v.Discovered || v.Unverified), version-miss and nil-manifest both
// count as unsigned; a pinned built-in version must stay signed.
func TestUnsignedVersion_Derivation(t *testing.T) {
	tool := &manifest.Tool{
		Name: "demo",
		VersionList: []manifest.Version{
			{Version: "1.0.0"},
			{Version: "2.0.0", Discovered: true},
			{Version: "2.1.0", Unverified: true},
			{Version: "2.2.0", Discovered: true, Unverified: true},
		},
	}

	cases := []struct {
		name    string
		tool    *manifest.Tool
		version string
		want    bool
	}{
		{"pinned built-in version", tool, "1.0.0", false},
		{"discovered version", tool, "2.0.0", true},
		{"unverified version", tool, "2.1.0", true},
		{"discovered+unverified", tool, "2.2.0", true},
		{"version not in manifest (custom tool)", tool, "9.9.9", true},
		{"nil manifest (lua/vfox plugin)", nil, "1.0.0", true},
	}
	for _, tc := range cases {
		if got := DeriveUnsignedVersion(tc.tool, tc.version); got != tc.want {
			t.Errorf("%s: DeriveUnsignedVersion = %v, want %v", tc.name, got, tc.want)
		}
	}

	// The derivation must NOT depend on the hash being empty: after a TOFU
	// write-back the hash is real and the verdict must not flip (G6).
	tool.VersionList[1].Archives = []manifest.Archive{{OS: "windows", Arch: "amd64", SHA256: strings.Repeat("a", 64)}}
	if !DeriveUnsignedVersion(tool, "2.0.0") {
		t.Error("a discovered version with a written-back hash must stay unsigned (no hash condition in the derivation)")
	}
}

// tofuApp builds an Installer whose manifest dir is a temp dir and whose
// registry knows the given tool as a built-in plugin.
func tofuApp(t *testing.T, tool string) (*Installer, *config.Config) {
	t.Helper()
	tmp := t.TempDir()
	cfg := &config.Config{
		Core: config.CoreConfig{
			XMDevDir: tmp,
			InstallDir: filepath.Join(tmp, "install"),
			CacheDir:   filepath.Join(tmp, "cache"),
			ShimDir:    filepath.Join(tmp, "shims"),
			// Deliberately NOT <home>/manifests: the N-04 guard needs a
			// configured dir that differs from the hard-coded legacy path.
			ManifestDir: filepath.Join(tmp, "custom-manifests"),
		},
		Path: config.PathConfig{Mode: "shim"},
	}
	if err := os.MkdirAll(cfg.Core.ManifestDir, 0o755); err != nil {
		t.Fatal(err)
	}
	reg := plugin.NewRegistry()
	reg.Register(&fakePlugin{name: tool})
	return New(cfg, reg), cfg
}

// G8: a disk manifest under third-party/ must not downgrade a built-in
// tool to the TOFU trust model. A genuinely third-party tool still may.
func TestLoadManifestSignature_ThirdPartySameNameRejected(t *testing.T) {
	ins, cfg := tofuApp(t, "go")

	// Built-in "go" exists at the top level of the manifest dir (the
	// curated set), and a same-named third-party manifest is planted.
	if err := os.WriteFile(filepath.Join(cfg.Core.ManifestDir, "go.yaml"), []byte("name: go\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	thirdPartyDir := filepath.Join(cfg.Core.ManifestDir, "third-party")
	if err := os.MkdirAll(thirdPartyDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(thirdPartyDir, "go.yaml"), []byte("name: go\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, isThirdParty := ins.LoadManifestSignature("go")
	if isThirdParty {
		t.Error("a same-name third-party manifest must NOT downgrade a built-in tool to TOFU (N-03)")
	}

	// A tool that exists ONLY under third-party/ keeps the TOFU fallback.
	if err := os.WriteFile(filepath.Join(thirdPartyDir, "custom.yaml"), []byte("name: custom\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, customThirdParty := ins.LoadManifestSignature("custom")
	if !customThirdParty {
		t.Error("a genuinely third-party tool must keep the TOFU fallback")
	}
}

// G10: the TOFU write-back must land in cfg.Core.ManifestDir — the pre-fix
// code hard-coded <home>/manifests and silently pinned the hash elsewhere.
func TestTOFUWriteback_UsesManifestDir(t *testing.T) {
	ins, cfg := tofuApp(t, "demo")

	// A minimal on-disk manifest with an empty SHA-256 for the version.
	yaml := `name: demo
display_name: Demo
category: system
versions:
  - version: 1.0.0
    stable: true
    archives:
      - os: windows
        arch: amd64
        format: zip
        url: https://example.test/demo-1.0.0.zip
        sha256: ""
`
	if err := os.WriteFile(filepath.Join(cfg.Core.ManifestDir, "demo.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}

	// A downloaded archive whose hash TOFU should record.
	dest := filepath.Join(cfg.Core.CacheDir, "demo-1.0.0.zip")
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dest, []byte("archive bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	arch := &manifest.Archive{OS: "windows", Arch: "amd64", Format: "zip", URL: "https://example.test/demo-1.0.0.zip"}

	if err := ins.TOFUArchiveIfNeeded("demo", "1.0.0", arch, dest); err != nil {
		t.Fatalf("TOFUArchiveIfNeeded: %v", err)
	}

	// The configured manifest dir was updated …
	updated, err := os.ReadFile(filepath.Join(cfg.Core.ManifestDir, "demo.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(updated), "sha256: ") || strings.Contains(string(updated), `sha256: ""`) {
		t.Errorf("TOFU hash not written into the configured manifest dir:\n%s", updated)
	}
	// … and the hard-coded legacy location was NOT created.
	legacy := filepath.Join(cfg.Core.XMDevDir, "manifests")
	if cfg.Core.ManifestDir == legacy {
		t.Skip("config manifest dir equals the legacy path — nothing to distinguish")
	}
	if _, err := os.Stat(filepath.Join(legacy, "demo.yaml")); err == nil {
		t.Errorf("TOFU wrote to the hard-coded legacy path %s instead of cfg.Core.ManifestDir", legacy)
	}
}
