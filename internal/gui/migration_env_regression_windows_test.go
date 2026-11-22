//go:build windows

package gui

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xmdemt/xdev/internal/env"
	"github.com/xmdemt/xdev/internal/localmigrate"
)

type migrationEnvPlugin struct {
	guiFakePlugin
	vars map[string]string
}

func (p *migrationEnvPlugin) EnvVars(string) map[string]string { return p.vars }

func TestRootMigrationPostSetupAutoAndEnvRollback(t *testing.T) {
	a := newTestAppWithDirs(t, t.TempDir())
	c := a.cfg
	c.Path.Mode = "auto"
	c.Core.ShimDir = filepath.Join(c.Core.XMDevDir, "custom-shims")
	if err := c.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(c.Core.XMDevDir, "xdev.exe"), []byte("fixture"), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XMDEV_HOME", c.Core.XMDevDir)
	_, restorePath := env.UseInMemoryUserPathForTesting("unchanged")
	t.Cleanup(restorePath)
	original := env.UserEnvSnapshot{Exists: true, Name: "GoRoot", Raw: "%ORIGINAL%", Type: env.UserEnvString}
	values := map[string]env.UserEnvSnapshot{"GOROOT": original}
	writes := 0
	reads := 0
	restore := env.UseUserEnvBackendForTesting(env.UserEnvBackend{
		Read: func(name string) (env.UserEnvSnapshot, error) { reads++; return values[strings.ToUpper(name)], nil },
		Write: func(name, value string, typ uint32) error {
			writes++
			if strings.EqualFold(name, "JAVA_HOME") {
				return errors.New("injected second write failure")
			}
			values[strings.ToUpper(name)] = env.UserEnvSnapshot{Exists: true, Name: name, Raw: value, Type: typ}
			return nil
		},
		Delete: func(name string) error { delete(values, strings.ToUpper(name)); return nil },
	})
	t.Cleanup(restore)
	p := &migrationEnvPlugin{guiFakePlugin: guiFakePlugin{name: "testtool", shims: []string{"testtool"}}, vars: map[string]string{"GOROOT": c.Core.InstallDir, "JAVA_HOME": c.Core.InstallDir, "Path": "forbidden"}}
	a.registry.Register(p)
	a.postMigrateSetup(localmigrate.LocalTool{Name: "testtool", Version: "1"}, c)
	if _, err := os.Stat(filepath.Join(c.Core.ShimDir, "testtool.exe")); err != nil {
		t.Error("auto/custom shim not installed", err)
	}
	if values["GOROOT"] != original || len(values) != 1 {
		t.Fatalf("environment not restored: %#v", values)
	}
	if reads != 2 || writes != 3 {
		t.Fatalf("expected two snapshots, write/failure/restore; reads=%d writes=%d", reads, writes)
	}
}
