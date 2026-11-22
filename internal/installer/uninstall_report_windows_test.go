//go:build windows

package installer

import (
	"errors"
	"github.com/xmdemt/xdev/internal/env"
	"testing"
)

type uninstallEnvPlugin struct{ *fakePlugin }

func (p *uninstallEnvPlugin) EnvVars(string) map[string]string {
	return map[string]string{"JAVA_HOME": "unused", "DENO_DIR": "unused"}
}

func TestReportD04LastVersionEnvironmentCleanupError(t *testing.T) {
	ins, cfg, _ := newTestInstaller(t)
	ins.SetNoEnvWrite(true)
	p := &uninstallEnvPlugin{&fakePlugin{name: "fakego", shims: []string{"fakego"}}}
	a := makeFakeInstalledVersion(t, cfg.Core.InstallDir, "fakego", "1.0.0")
	if err := ins.Activate(p, a, "1.0.0"); err != nil {
		t.Fatal(err)
	}
	cause := errors.New("environment deletion sentinel")
	calls := map[string]int{}
	t.Cleanup(env.UseUserEnvBackendForTesting(env.UserEnvBackend{
		Read: func(string) (env.UserEnvSnapshot, error) {
			t.Fatal("cleanup read user env")
			return env.UserEnvSnapshot{}, nil
		},
		Write: func(string, string, uint32) error { t.Fatal("cleanup wrote user env"); return nil },
		Delete: func(name string) error {
			calls[name]++
			if name == "JAVA_HOME" {
				return cause
			}
			return nil
		},
	}))
	ins.SetNoEnvWrite(false)
	err := ins.Uninstall(p, "1.0.0")
	if !errors.Is(err, cause) {
		t.Fatalf("cleanup cause hidden: %v", err)
	}
	if calls["JAVA_HOME"] != 1 || calls["DENO_DIR"] != 1 {
		t.Fatalf("cleanup did not attempt all vars: %v", calls)
	}
}
