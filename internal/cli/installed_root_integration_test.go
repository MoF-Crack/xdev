package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xmdemt/xdev/internal/config"
	"github.com/xmdemt/xdev/internal/manifest"
	"github.com/xmdemt/xdev/internal/plugin"
	"github.com/xmdemt/xdev/internal/util"
)

// Exercise the actual Cobra commands, with the shipped NDK manifest and a
// persisted install_dir override. All links/files are private test fixtures.
func TestD18_InstalledRootCLIEntries(t *testing.T) {
	for _, nestedRoot := range []string{"android-sdk", "vendor/mobile/sdk"} {
		t.Run(nestedRoot, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("XMDEV_HOME", home)
			t.Cleanup(config.ResetHomeDirForTest)
			cfg, err := config.LoadReadOnly(home)
			if err != nil {
				t.Fatal(err)
			}
			cfg.Core.InstallDir = filepath.Join(home, "custom-tool-store")
			if err := cfg.Save(); err != nil {
				t.Fatal(err)
			}
			cfg, err = config.LoadReadOnly(home)
			if err != nil {
				t.Fatal(err)
			}
			originalConfig, err := os.ReadFile(filepath.Join(home, "config.yaml"))
			if err != nil {
				t.Fatal(err)
			}
			old := globalCtx
			globalCtx = runCtx{cfg: cfg, initialized: true, noPathFix: true}
			t.Cleanup(func() { globalCtx = old })
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
			cat, err := manifest.LoadCatalog(cfg.Core.ManifestDir)
			if err != nil {
				t.Fatal(err)
			}
			m, ok := cat.Tool("android-ndk")
			if !ok || m.InstallRoot != nestedRoot {
				t.Fatalf("fixture manifest: %+v", m)
			}
			p := plugin.NewGeneric(m)
			dirs := map[string]string{}
			for _, v := range []string{"r29", "27.2.12479018"} {
				dir, err := p.InstallDir(cfg.Core.InstallDir, v)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.MkdirAll(dir, 0755); err != nil {
					t.Fatal(err)
				}
				dirs[v] = dir
			}
			// Misleading flat/old roots, auxiliary directories and arbitrary trees
			// must not become installed versions or synthetic tools in the table.
			for _, dir := range []string{
				filepath.Join(cfg.Core.InstallDir, "android-ndk", "r99"),
				filepath.Join(home, "install", nestedRoot, "android-ndk", "r98"),
				filepath.Join(filepath.Dir(dirs["r29"]), "gopath"),
				filepath.Join(cfg.Core.InstallDir, "unregistered", "1.2.3"),
				filepath.Join(home, "current"),
				filepath.Join(home, "project", "src"),
			} {
				if err := os.MkdirAll(dir, 0755); err != nil {
					t.Fatal(err)
				}
			}
			if err := util.CreateJunction(filepath.Join(home, "current", "android-ndk"), dirs["27.2.12479018"]); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(home, "project", ".xdev.yaml"), []byte("tools:\n  android-ndk: r29\n"), 0600); err != nil {
				t.Fatal(err)
			}
			t.Chdir(filepath.Join(home, "project", "src"))
			hook := newHookCmd()
			hook.SetArgs([]string{"--shell", "bash"})
			capture := captureStdout(t)
			err = hook.Execute()
			out := capture()
			if err != nil || !strings.Contains(out, dirs["r29"]) || !strings.Contains(out, "ANDROID_NDK_HOME") || !strings.Contains(out, filepath.Clean(filepath.Join(dirs["r29"], "..", ".."))) {
				t.Errorf("hook did not select installed project NDK: %q, %v; want %s", out, err, dirs["r29"])
			}
			for _, args := range [][]string{{"android-ndk"}, {"android-ndk", "--remote"}, {}} {
				cmd := newListCmd()
				cmd.SetArgs(args)
				capture = captureStdout(t)
				err = cmd.Execute()
				out = capture()
				if err != nil || !strings.Contains(out, "r29") || !strings.Contains(out, "27.2.12479018") || !strings.Contains(out, "★") {
					t.Errorf("list %v: %q / %v", args, out, err)
				}
				if len(args) == 2 && !strings.Contains(out, "installed") {
					t.Errorf("remote list lacks installed NDK: %q", out)
				}
				for _, forbidden := range []string{"r99", "r98", "gopath"} {
					if strings.Contains(out, forbidden) {
						t.Errorf("list %v leaked %q: %s", args, forbidden, out)
					}
				}
				if len(args) == 0 && (!strings.Contains(out, "android-ndk") || !strings.Contains(out, "unregistered")) {
					t.Errorf("total list omits canonical or legacy flat tool: %s", out)
				}
			}
			afterConfig, err := os.ReadFile(filepath.Join(home, "config.yaml"))
			if err != nil || string(afterConfig) != string(originalConfig) {
				t.Fatalf("read-only entries changed configuration: %v", err)
			}
		})
	}
}
