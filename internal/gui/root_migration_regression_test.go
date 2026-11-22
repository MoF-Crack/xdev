package gui

import (
	"github.com/xmdemt/xdev/internal/config"
	"github.com/xmdemt/xdev/internal/env"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestRootMigrationLegacyCopyAndSwitch(t *testing.T) {
	for _, blocked := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "collision"}[blocked], func(t *testing.T) {
			base := t.TempDir()
			old := filepath.Join(base, "XimuLocalDev", "xmdevenvmt")
			dest := filepath.Join(base, "XDev")
			a := newTestAppWithDirs(t, old)
			if err := a.cfg.EnsureDirs(); err != nil {
				t.Fatal(err)
			}
			if err := a.cfg.Save(); err != nil {
				t.Fatal(err)
			}
			t.Setenv("XMDEV_HOME", old)
			config.ResetHomeDirForTest()
			t.Cleanup(config.ResetHomeDirForTest)
			_, restore := env.UseInMemoryUserPathForTesting("")
			t.Cleanup(restore)
			sentinel := filepath.Join(old, "install", "tool-data")
			if err := os.WriteFile(sentinel, []byte("keep"), 0600); err != nil {
				t.Fatal(err)
			}
			before, _ := os.ReadFile(filepath.Join(old, "config.yaml"))
			if blocked {
				if err := os.MkdirAll(dest, 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dest, "occupied"), []byte("keep"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			err := a.MigrateLegacyDir(old)
			if blocked {
				if err == nil {
					t.Fatal("collision accepted")
				}
				if a.getCfg().Core.XMDevDir != old {
					t.Fatal("failure changed root")
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				loaded, e := config.Load(old)
				if e != nil {
					t.Fatal(e)
				}
				for _, c := range []*config.Config{a.getCfg(), loaded} {
					if c.Core.XMDevDir != dest || c.Core.InstallDir != filepath.Join(dest, "install") || c.Core.ShimDir != filepath.Join(dest, "shims") || c.Core.ManifestDir != filepath.Join(dest, "manifests") || c.Core.CacheDir != filepath.Join(dest, "cache") || c.Core.EnvDir != filepath.Join(dest, "env") || c.Core.DownloadDir != filepath.Join(dest, "downloads") {
						t.Fatal("root or derived paths not switched", c.Core)
					}
				}
				if b, e := os.ReadFile(filepath.Join(dest, "install", "tool-data")); e != nil || string(b) != "keep" {
					t.Fatal("copy missing", e)
				}
			}
			if b, e := os.ReadFile(sentinel); e != nil || string(b) != "keep" {
				t.Fatal("source lost", e)
			}
			after, _ := os.ReadFile(filepath.Join(old, "config.yaml"))
			if string(before) != string(after) {
				t.Fatal("source config changed")
			}
		})
	}
}

func TestRootMigrationInitFailureKeepsConfig(t *testing.T) {
	for _, phase := range []string{"dirs", "save"} {
		t.Run(phase, func(t *testing.T) {
			a := newTestAppWithDirs(t, t.TempDir())
			before := a.getCfg().Clone()
			dest := t.TempDir()
			if phase == "dirs" {
				if err := os.WriteFile(filepath.Join(dest, "install"), []byte("blocked"), 0600); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := os.Mkdir(filepath.Join(dest, "config.yaml"), 0700); err != nil {
					t.Fatal(err)
				}
			}
			if err := a.InitConfig(dest); err == nil {
				t.Fatal("expected initialization error")
			}
			after := a.getCfg()
			if !reflect.DeepEqual(before.Core, after.Core) || before.Integrity != after.Integrity {
				t.Fatal("failed initialization published partial config")
			}
		})
	}
}
