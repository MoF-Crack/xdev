package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/xmdemt/xdev/internal/config"
)

func TestConfigCommandRootReanchorsAndReloads(t *testing.T) {
	root := t.TempDir()
	next := filepath.Join(t.TempDir(), "new-home")
	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Core.InstallDir = filepath.Join(root, "explicit-install")
	if err = cfg.Save(); err != nil {
		t.Fatal(err)
	}
	old := globalCtx
	globalCtx = runCtx{cfg: cfg, initialized: true}
	t.Cleanup(func() { globalCtx = old; config.ResetHomeDirForTest() })
	t.Setenv("XMDEV_HOME", root)
	cmd := newConfigSetCmd()
	cmd.SetArgs([]string{"core.xmdevenv_dir", next})
	if err = cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	for _, loaded := range []*config.Config{Cfg(), mustLoadRoot(t, next), mustLoadRoot(t, "")} {
		want := map[string]string{"download": filepath.Join(next, "downloads"), "install": filepath.Join(next, "install"), "shim": filepath.Join(next, "shims"), "cache": filepath.Join(next, "cache"), "env": filepath.Join(next, "env"), "manifest": filepath.Join(next, "manifests")}
		got := map[string]string{"download": loaded.Core.DownloadDir, "install": loaded.Core.InstallDir, "shim": loaded.Core.ShimDir, "cache": loaded.Core.CacheDir, "env": loaded.Core.EnvDir, "manifest": loaded.Core.ManifestDir}
		for field, value := range want {
			if got[field] != value {
				t.Errorf("%s=%q want %q", field, got[field], value)
			}
		}
	}
	// Switching roots does not claim or delete the existing installation tree.
	if _, err = os.Stat(filepath.Join(root, "config.yaml")); err != nil {
		t.Fatal(err)
	}
	if got := config.ResolveHomeDir(); got != next {
		t.Fatalf("shim home=%q want %q", got, next)
	}
}

func TestConfigCommandSaveFailureKeepsSnapshot(t *testing.T) {
	root := t.TempDir()
	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if err = cfg.Save(); err != nil {
		t.Fatal(err)
	}
	filename := filepath.Join(root, "config.yaml")
	if err = os.Rename(filename, filename+".bak"); err != nil {
		t.Fatal(err)
	}
	if err = os.Mkdir(filename, 0700); err != nil {
		t.Fatal(err)
	}
	old := globalCtx
	globalCtx = runCtx{cfg: cfg, initialized: true}
	t.Cleanup(func() { globalCtx = old })
	before := cfg.Core.LogLevel
	cmd := newConfigSetCmd()
	cmd.SetArgs([]string{"core.log_level", "debug"})
	if err = cmd.Execute(); err == nil {
		t.Fatal("expected save error")
	}
	if cfg.Core.LogLevel != before {
		t.Fatal("failed CLI edit published memory state")
	}
}

func mustLoadRoot(t *testing.T, root string) *config.Config {
	t.Helper()
	c, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	return c
}
