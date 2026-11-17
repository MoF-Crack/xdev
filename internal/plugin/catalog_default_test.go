package plugin

import (
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/xmdemt/xdev/internal/manifest"
)

// Exercise the actual default-install chain, including a warm catalog read.
func TestCatalogDefault_ResolveAndArchive(t *testing.T) {
	manifest.InitEmbedded(fstest.MapFS{"demo.yaml": &fstest.MapFile{Data: []byte(`name: demo
latest: 1.0.0
versions:
  - version: 1.0.0
    archives:
      - os: any
        arch: any
        url: https://trusted.test/1.zip
        format: zip
        sha256: trusted
`)}})
	// No other plugin tests initialize embedded manifests. An empty FS clears
	// these fixture entries without passing nil to fs.ReadDir.
	t.Cleanup(func() { manifest.InitEmbedded(fstest.MapFS{}) })
	dir, cache := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "demo.yaml"), []byte(`name: demo
latest: 2.0.0
versions:
  - version: 1.0.0
    archives:
      - os: any
        arch: any
        url: https://untrusted.test/1.zip
        format: zip
        sha256: overwritten
  - version: 2.0.0
    archives:
      - os: any
        arch: any
        url: https://discovered.test/2.zip
        format: zip
        sha256: discovered
`), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"uncached", "cold", "warm"} {
		t.Run(mode, func(t *testing.T) {
			cacheDir := cache
			if mode == "uncached" {
				cacheDir = ""
			}
			cat, err := manifest.LoadCatalogCached(dir, cacheDir)
			if err != nil {
				t.Fatal(err)
			}
			tool, ok := cat.Tool("demo")
			if !ok {
				t.Fatal("missing demo")
			}
			p := NewGeneric(tool)
			if got := p.DefaultVersion(); got != "2.0.0" {
				t.Fatalf("default = %q", got)
			}
			for _, spec := range []string{p.DefaultVersion(), "latest"} {
				got, err := p.ResolveVersion(spec)
				if err != nil || got != "2.0.0" {
					t.Fatalf("resolve %q = %q, %v", spec, got, err)
				}
				a, err := p.ArchiveFor(got)
				if err != nil || a.URL != "https://discovered.test/2.zip" || a.SHA256 != "discovered" {
					t.Fatalf("default archive = %+v, %v", a, err)
				}
			}
			a, err := p.ArchiveFor("1.0.0")
			if err != nil || a.URL != "https://trusted.test/1.zip" || a.SHA256 != "trusted" {
				t.Fatalf("trusted archive = %+v, %v", a, err)
			}
		})
	}
}
