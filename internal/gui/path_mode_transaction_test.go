package gui

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/xmdemt/xdev/internal/config"
	"github.com/xmdemt/xdev/internal/env"
	"github.com/xmdemt/xdev/internal/remote"
)

func TestGUISetterSaveFailureHasNoSideEffects(t *testing.T) {
	for _, kind := range []string{"proxy", "path"} {
		t.Run(kind, func(t *testing.T) {
			a := newTestAppWithDirs(t, t.TempDir())
			if err := a.cfg.EnsureDirs(); err != nil {
				t.Fatal(err)
			}
			if err := a.cfg.Save(); err != nil {
				t.Fatal(err)
			}
			originalPath := "original-path" + string(os.PathListSeparator) + a.cfg.Core.ShimDir
			_, restore := env.UseInMemoryUserPathForTesting(originalPath)
			t.Cleanup(restore)
			snapshot := a.getCfg()
			cfgFile := filepath.Join(snapshot.Core.XMDevDir, "config.yaml")
			before, _ := os.ReadFile(cfgFile)
			backup := cfgFile + ".test-before"
			if err := os.Rename(cfgFile, backup); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(cfgFile, 0700); err != nil {
				t.Fatal(err)
			}
			shimFile := filepath.Join(snapshot.Core.ShimDir, "keep")
			if err := os.WriteFile(shimFile, []byte("original"), 0600); err != nil {
				t.Fatal(err)
			}
			var err error
			if kind == "proxy" {
				err = a.SetProxyConfig("http://127.0.0.1:1234", "", "", true)
			} else {
				err = a.SetPathMode("direct")
			}
			if err == nil {
				t.Error("GUI setter hid save failure")
			}
			after := a.getCfg()
			if !reflect.DeepEqual(snapshot, after) {
				t.Error("failed setter changed memory")
			}
			if b, e := os.ReadFile(backup); e != nil || string(b) != string(before) {
				t.Error("old config bytes changed", e)
			}
			if got, _ := env.ReadUserPath(); got != originalPath {
				t.Error("failed setter changed PATH", got)
			}
			if b, e := os.ReadFile(shimFile); e != nil || string(b) != "original" {
				t.Error("failed setter changed shim", e)
			}
		})
	}
}

func TestGUISetterIntegrityRoundTrip(t *testing.T) {
	a := newTestAppWithDirs(t, t.TempDir())
	if err := a.cfg.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	if err := a.cfg.Save(); err != nil {
		t.Fatal(err)
	}
	_, restore := env.UseInMemoryUserPathForTesting("original-path")
	t.Cleanup(restore)
	t.Cleanup(func() { remote.ConfigureHTTPClient("") })
	if err := a.SetProxyConfig("http://127.0.0.1:1234", "socks5://127.0.0.1:1235", "localhost", true); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"direct", "shim", "auto"} {
		if err := a.SetPathMode(mode); err != nil {
			t.Fatal(err)
		}
		loaded, err := config.LoadReadOnly(a.getCfg().Core.XMDevDir)
		if err != nil {
			t.Fatal(err)
		}
		if loaded.Path.Mode != mode || !loaded.Proxy.Enabled || loaded.Proxy.HTTP != "http://127.0.0.1:1234" || loaded.Integrity == "" {
			t.Fatalf("GUI setter/load mismatch: %#v %#v", loaded.Path, loaded.Proxy)
		}
	}
}
