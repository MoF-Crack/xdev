//go:build windows

package gui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func init() {
	if os.Getenv("XDEV_PM_IDENTITY_FIXTURE") != "1" {
		return
	}
	name := strings.TrimSuffix(filepath.Base(os.Args[0]), ".exe")
	if name != "pm-intended" && name != "corepack" {
		return
	}
	f, err := os.OpenFile(os.Getenv("XDEV_PM_IDENTITY_LOG"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		os.Exit(91)
	}
	_, err = fmt.Fprintln(f, name, strings.Join(os.Args[1:], " "))
	if err != nil {
		os.Exit(92)
	}
	if err = f.Close(); err != nil {
		os.Exit(93)
	}
	os.Exit(0)
}

func TestPMApprovedCommandIdentitySurvivesManagedNode(t *testing.T) {
	a := newTestApp(t)
	home := a.getCfg().Core.XMDevDir
	sourceDir := filepath.Join(a.getCfg().Core.ManifestDir, "local")
	if err := os.MkdirAll(sourceDir, 0700); err != nil {
		t.Fatal(err)
	}
	definition := `package_managers:
 - name: probe
   parent_tool: node
   enable_command: pm-intended activate
   install_methods:
    - name: custom
      command: pm-intended install
      detect_cmd: corepack
`
	if err := os.WriteFile(filepath.Join(sourceDir, "package-managers.yaml"), []byte(definition), 0600); err != nil {
		t.Fatal(err)
	}
	binDir := filepath.Join(home, "fixture-bin")
	nodeDir := filepath.Join(home, "install", "node", "1.0.0")
	for _, dir := range []string{binDir, nodeDir, filepath.Join(home, "current")} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{filepath.Join(binDir, "pm-intended.exe"), filepath.Join(nodeDir, "corepack.exe")} {
		if err := os.Link(exe, path); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", binDir)
	t.Setenv("XDEV_PM_IDENTITY_FIXTURE", "1")
	journal := filepath.Join(home, "argv.log")
	t.Setenv("XDEV_PM_IDENTITY_LOG", journal)
	plan, err := a.PlanPackageManager("local/probe")
	if err != nil {
		t.Fatal(err)
	}
	if err := a.InstallSubToolTrusted("local/probe", "", plan.Digest); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(nodeDir, filepath.Join(home, "current", "node")); err != nil {
		t.Fatal(err)
	}
	plan2, err := a.PlanPackageManager("local/probe")
	if err != nil {
		t.Fatal(err)
	}
	if plan != plan2 {
		t.Fatalf("definition unexpectedly changed")
	}
	for _, method := range []string{"", "custom"} {
		if err := a.InstallSubToolTrusted("local/probe", method, plan.Digest); err != nil {
			t.Fatal(err)
		}
	}
	actual, err := os.ReadFile(journal)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(actual), "corepack") {
		t.Fatalf("approved command was replaced by detector/parent executable: %s", actual)
	}
	if strings.Count(string(actual), "pm-intended activate") != 2 || !strings.Contains(string(actual), "pm-intended install") {
		t.Fatalf("unexpected execution: %s", actual)
	}
}
