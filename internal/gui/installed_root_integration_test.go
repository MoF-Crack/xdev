package gui

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/xmdemt/xdev/internal/config"
	"github.com/xmdemt/xdev/internal/plugin"
)

func TestD18_InstalledRootWailsVersions(t *testing.T) {
	for _, nestedRoot := range []string{"android-sdk", "vendor/mobile/sdk"} {
		t.Run(nestedRoot, func(t *testing.T) {
			app := newB5App(t, t.TempDir())
			if err := app.mutateCfg(func(cfg *config.Config) error {
				cfg.Core.InstallDir = filepath.Join(cfg.Core.XMDevDir, "configured-store")
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			cfg := app.getCfg()
			body, err := os.ReadFile(filepath.Join("..", "..", "manifests", "android-ndk.yaml"))
			if err != nil {
				t.Fatal(err)
			}
			body = []byte(strings.ReplaceAll(string(body), "install_root: android-sdk", "install_root: "+nestedRoot))
			if err := os.MkdirAll(cfg.Core.ManifestDir, 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(cfg.Core.ManifestDir, "android-ndk.yaml"), body, 0600); err != nil {
				t.Fatal(err)
			}
			if err := app.reloadCatalog(); err != nil {
				t.Fatal(err)
			}
			app.registry = plugin.BuildRegistry(app.getCat(), "")
			p, err := app.mustGetPlugin("android-ndk")
			if err != nil {
				t.Fatal(err)
			}
			for _, version := range []string{"r29", "27.2.12479018"} {
				dir, err := p.InstallDir(cfg.Core.InstallDir, version)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.MkdirAll(dir, 0755); err != nil {
					t.Fatal(err)
				}
				for _, alias := range p.Shims() {
					if runtime.GOOS == "windows" {
						alias += ".cmd"
					}
					if err := os.WriteFile(filepath.Join(dir, alias), []byte("fixture executable\n"), 0755); err != nil {
						t.Fatal(err)
					}
				}
			}
			linkCurrent(t, cfg.Core.XMDevDir, filepath.Join(cfg.Core.InstallDir, nestedRoot), "android-ndk", "27.2.12479018")
			// This flat-root version is present on disk but is outside this plugin's layout.
			if err := os.MkdirAll(filepath.Join(cfg.Core.InstallDir, "android-ndk", "26.3.11579264"), 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(filepath.Join(cfg.Core.InstallDir, nestedRoot, "android-ndk", "gopath"), 0755); err != nil {
				t.Fatal(err)
			}
			available := app.GetAvailableVersions("android-ndk")
			seen := map[string]bool{}
			for _, vi := range available {
				seen[vi.Version] = true
				wantInstalled := vi.Version == "r29" || vi.Version == "27.2.12479018"
				wantActive := vi.Version == "27.2.12479018"
				if vi.Installed != wantInstalled || vi.Active != wantActive {
					t.Errorf("Wails available %+v want installed=%v active=%v", vi, wantInstalled, wantActive)
				}
			}
			if !seen["r29"] || !seen["27.2.12479018"] {
				t.Fatalf("missing shipped versions: %+v", available)
			}
			installed := app.GetInstalledVersions("android-ndk")
			if len(installed) != 2 {
				t.Fatalf("Wails installed versions: %+v", installed)
			}
			for _, vi := range installed {
				if !vi.Installed || !vi.Intact || vi.Active != (vi.Version == "27.2.12479018") {
					t.Errorf("Wails installed flags: %+v", vi)
				}
				if vi.Version != "r29" && vi.Version != "27.2.12479018" {
					t.Errorf("unrelated directory leaked: %+v", vi)
				}
			}
		})
	}
}
