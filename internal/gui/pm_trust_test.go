package gui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xmdemt/xdev/internal/pm"
	"github.com/xmdemt/xdev/internal/pm/pmtest"
)

func TestGUI_PMTrustBeforeExecute(t *testing.T) {
	a := newTestApp(t)
	dir := a.getCfg().Core.ManifestDir
	if err := os.MkdirAll(filepath.Join(dir, "local"), 0700); err != nil {
		t.Fatal(err)
	}
	calls := pmtest.Commands(t, "bash", "probe-detector", "fixture-method", "fixture-enable")
	path := filepath.Join(dir, "local", pm.FileName)
	text := `package_managers:
  - name: scoop
    display_name: Local fixture
    install_command: inert command
    execution_env: bash
    detect_cmd: probe-detector
    parent_tool: nothing
    enable_command: fixture-enable activate
    install_methods:
      - name: first
        command: fixture-method install
        detect_cmd: fixture-method
`
	write := func(path, body string) {
		if err := os.WriteFile(path, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write(path, text)
	name := "local/scoop"
	p, err := a.PlanPackageManager(name)
	if err != nil || p.Source != "local" || !strings.Contains(p.Summary, "fixture-method install") {
		t.Fatal(p, err)
	}
	rows := a.GetPackageManagers()
	found := false
	for _, r := range rows {
		if r.Name == name {
			found = r.CanInstall && !r.Installed
		}
	}
	if !found {
		t.Fatal("untrusted local should be listed without probing")
	}
	for _, op := range []func() error{func() error { return a.InstallPackageManager(name) }, func() error { return a.InstallSubTool(name, "") }, func() error { return a.InstallSubTool(name, "first") }, func() error { return a.MigratePackageManager(name) }, func() error { return a.UpdatePackageManager(name) }} {
		if err := op(); err == nil {
			t.Fatal("missing approval accepted")
		}
	}
	if calls() != "" {
		t.Fatal(calls())
	}
	if err := a.InstallPackageManagerTrusted(name, p.Digest); err != nil {
		t.Fatal(err)
	}
	if err := a.InstallSubToolTrusted(name, "first", p.Digest); err != nil {
		t.Fatal(err)
	}
	if err := a.InstallSubToolTrusted(name, "", p.Digest); err != nil {
		t.Fatal(err)
	}
	journal := calls()
	for _, key := range []string{"inert command", "fixture-method", "fixture-enable"} {
		if !strings.Contains(journal, key) {
			t.Fatal(journal)
		}
	}
	if strings.Contains(journal, "probe-detector") {
		t.Fatal(journal)
	}
	write(path, strings.Replace(text, "inert command", "changed fixture", 1))
	if err := a.InstallPackageManagerTrusted(name, p.Digest); err == nil {
		t.Fatal("stale approval accepted")
	}
	write(filepath.Join(dir, pm.FileName), text)
	write(filepath.Join(dir, pm.FileName+".sig"), "invalid")
	if err := a.InstallPackageManager("scoop"); err == nil {
		t.Fatal("bad official catalog accepted")
	}
	if calls() != journal {
		t.Fatal(calls())
	}
}
