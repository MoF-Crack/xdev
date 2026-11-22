//go:build windows

package gui

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/xmdemt/xdev/internal/config"
	"github.com/xmdemt/xdev/internal/env"
)

func TestGUISetterPathTransactionFaults(t *testing.T) {
	for _, phase := range []string{"save", "shim-publish", "path-write"} {
		t.Run(phase, func(t *testing.T) {
			a := newTestAppWithDirs(t, t.TempDir())
			c := a.cfg
			c.Path.Mode = "direct"
			if err := c.EnsureDirs(); err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(filepath.Join(c.Core.XMDevDir, "current", "demo", "bin"), 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(c.Core.XMDevDir, "xdev.exe"), []byte("fixture dispatcher"), 0755); err != nil {
				t.Fatal(err)
			}
			a.registry.Register(&guiFakePlugin{name: "demo", shims: []string{"demo", "keeper"}})
			keeper := filepath.Join(c.Core.ShimDir, "keeper.exe")
			if err := os.WriteFile(keeper, []byte("prior entry"), 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(c.Core.ShimDir, "keeper"), []byte("prior bare"), 0600); err != nil {
				t.Fatal(err)
			}
			if err := c.Save(); err != nil {
				t.Fatal(err)
			}
			filename := filepath.Join(c.Core.XMDevDir, "config.yaml")
			before, err := os.ReadFile(filename)
			if err != nil {
				t.Fatal(err)
			}
			snapshot := c.Clone()
			original := env.UserEnvSnapshot{Exists: true, Name: "Path", Raw: "%SystemRoot%\\system32;C:\\original", Type: env.UserEnvString}
			state := original
			writes := 0
			restore := env.UseUserPathBackendForTesting(func() (string, error) { return state.Raw, nil }, func(next string) error {
				writes++
				if phase == "path-write" {
					return errors.New("injected final PATH write")
				}
				state.Raw = next
				return nil
			})
			t.Cleanup(restore)
			if phase == "save" {
				old := pathModeSave
				pathModeSave = func(*config.Config, string) error { return errors.New("injected config save") }
				t.Cleanup(func() { pathModeSave = old })
			}
			if phase == "shim-publish" {
				old := pathModeRename
				pathModeRename = func(src, dst string) error {
					if dst == filepath.Join(c.Core.ShimDir, "demo.exe") {
						return errors.New("injected shim publish")
					}
					return os.Rename(src, dst)
				}
				t.Cleanup(func() { pathModeRename = old })
			}
			err = a.SetPathMode("auto")
			if err == nil || !strings.Contains(err.Error(), "injected") {
				t.Fatalf("missing actionable failure: %v", err)
			}
			if !reflect.DeepEqual(snapshot, a.getCfg()) {
				t.Error("failed transaction changed memory")
			}
			after, e := os.ReadFile(filename)
			if e != nil || string(before) != string(after) {
				t.Error("config bytes not restored", e)
			}
			if state != original {
				t.Fatalf("PATH value/name/type changed: %+v", state)
			}
			if phase != "path-write" && writes != 0 {
				t.Fatalf("PATH wrote before prior stage succeeded: %d", writes)
			}
			if phase == "path-write" && writes != 1 {
				t.Fatalf("final write was not exercised: %d", writes)
			}
			for _, name := range []string{"demo", "demo.exe"} {
				if _, e := os.Lstat(filepath.Join(c.Core.ShimDir, name)); !os.IsNotExist(e) {
					t.Error("new shim remains after failure", name, e)
				}
			}
			if b, e := os.ReadFile(keeper); e != nil || string(b) != "prior entry" {
				t.Error("prior shim changed", e)
			}
			entries, e := os.ReadDir(filepath.Join(c.Core.ShimDir, ".xdev-dispatchers"))
			if e != nil && !os.IsNotExist(e) {
				t.Fatal(e)
			}
			if len(entries) != 0 {
				t.Error("new ownership records remain")
			}
		})
	}
}
