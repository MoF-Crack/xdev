package shim

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/xmdemt/xdev/internal/config"
	"github.com/xmdemt/xdev/internal/manifest"
	"github.com/xmdemt/xdev/internal/plugin"
	"github.com/xmdemt/xdev/internal/util"
)

func TestD18_InstalledRootProjectShim(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	binary, err := os.ReadFile(self)
	if err != nil {
		t.Fatal(err)
	}
	for _, nestedRoot := range []string{"android-sdk", "vendor/mobile/sdk"} {
		t.Run(nestedRoot, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("XMDEV_HOME", home)
			t.Cleanup(config.ResetHomeDirForTest)
			cfg, err := config.LoadReadOnly(home)
			if err != nil {
				t.Fatal(err)
			}
			cfg.Core.InstallDir = filepath.Join(home, "configured-store")
			cfg.Core.ManifestDir = filepath.Join(home, "custom-manifests")
			if err := cfg.Save(); err != nil {
				t.Fatal(err)
			}
			original, err := os.ReadFile(filepath.Join(home, "config.yaml"))
			if err != nil {
				t.Fatal(err)
			}
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
			if err := os.WriteFile(filepath.Join(cfg.Core.ManifestDir, "node.yaml"), []byte("name: node\nlatest: 22.0.0\nshims: [node, npm]\nversions:\n  - version: 22.0.0\n"), 0600); err != nil {
				t.Fatal(err)
			}
			cat, err := manifest.LoadCatalog(cfg.Core.ManifestDir)
			if err != nil {
				t.Fatal(err)
			}
			reg := plugin.BuildRegistry(cat, "")
			makeBinary := func(tool, ver, alias string) string {
				t.Helper()
				p, ok := reg.Get(tool)
				if !ok {
					t.Fatalf("missing fixture plugin %s", tool)
				}
				dir, err := p.InstallDir(cfg.Core.InstallDir, ver)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.MkdirAll(dir, 0755); err != nil {
					t.Fatal(err)
				}
				name := alias
				if runtime.GOOS == "windows" {
					name += ".exe"
				}
				target := filepath.Join(dir, name)
				// A real executable file, not just an empty version directory.
				if err := os.WriteFile(target, binary, 0755); err != nil {
					t.Fatal(err)
				}
				return target
			}
			ndk := makeBinary("android-ndk", "r29", "ndk-build")
			globalNDK := makeBinary("android-ndk", "27.2.12479018", "ndk-build")
			node := makeBinary("node", "22.0.0", "node")
			npm := makeBinary("node", "22.0.0", "npm")
			if err := os.MkdirAll(filepath.Join(home, "current"), 0755); err != nil {
				t.Fatal(err)
			}
			if err := util.CreateJunction(filepath.Join(home, "current", "android-ndk"), filepath.Dir(globalNDK)); err != nil {
				t.Fatal(err)
			}
			projectDir := filepath.Join(home, "project")
			if err := os.MkdirAll(filepath.Join(projectDir, "src"), 0755); err != nil {
				t.Fatal(err)
			}
			projectFile := filepath.Join(projectDir, ".xdev.yaml")
			if err := os.WriteFile(projectFile, []byte("tools:\n  android-ndk: r29\n  node: 22.0.0\n"), 0600); err != nil {
				t.Fatal(err)
			}
			t.Chdir(filepath.Join(projectDir, "src"))
			for _, tc := range []struct{ alias, tool, target string }{{"ndk-build", "android-ndk", ndk}, {"node", "node", node}, {"npm", "node", npm}} {
				got, tool, ok := lookupProjectVersion(home, tc.alias, nil)
				if !ok || got != tc.target || tool != tc.tool {
					t.Errorf("project alias %s = %q,%q,%v want %q,%q", tc.alias, got, tool, ok, tc.target, tc.tool)
				}
			}
			InvalidateShimIndex()
			t.Cleanup(InvalidateShimIndex)
			got, tool, args, err := Lookup("ndk-build", []string{"--version"})
			if err != nil || got != ndk || tool != "android-ndk" || len(args) != 1 || args[0] != "--version" {
				t.Errorf("Lookup project: %q %q %v %v", got, tool, args, err)
			}
			if err := os.WriteFile(projectFile, []byte("tools:\n  android-ndk: r28\n"), 0600); err != nil {
				t.Fatal(err)
			}
			if got, _, ok := lookupProjectVersion(home, "ndk-build", nil); ok {
				t.Errorf("missing project version matched %q", got)
			}
			got, tool, _, err = Lookup("ndk-build", nil)
			if err != nil || got == "" || tool != "android-ndk" {
				t.Fatalf("missing version did not fall back to current: %q %q %v", got, tool, err)
			}
			selected, err := os.Stat(got)
			if err != nil {
				t.Fatal(err)
			}
			expected, err := os.Stat(globalNDK)
			if err != nil {
				t.Fatal(err)
			}
			if !os.SameFile(selected, expected) {
				t.Errorf("global fallback = %q, want file %q", got, globalNDK)
			}
			after, err := os.ReadFile(filepath.Join(home, "config.yaml"))
			if err != nil || string(after) != string(original) {
				t.Fatalf("shim rewrote config override: %v", err)
			}
		})
	}
}
