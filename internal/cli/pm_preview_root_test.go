package cli

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
)

// Execute the real root command in a fresh process: Cobra's initialization and
// config's legacy-migration once gate must not be bypassed by injected globalCtx.
func TestPMPreviewRootReadOnly(t *testing.T) {
	if os.Getenv("XDEV_PM_ROOT_PREVIEW_CHILD") == "1" {
		os.Args[0] = "xdev"
		cmd := NewRootCommand("xdev", "preview fixture")
		name := "brew"
		if runtime.GOOS == "windows" {
			name = "choco"
		}
		args := []string{"pm", "install", name, "--dry-run"}
		if dir := os.Getenv("XDEV_PM_ROOT_PREVIEW_DIR"); dir != "" {
			args = append(args, "--xmdevenv-dir", dir)
		}
		cmd.SetArgs(args)
		if err := cmd.Execute(); err != nil {
			t.Fatal(err)
		}
		if sniffScheduler != nil {
			t.Fatal("read-only preview constructed scheduler")
		}
		return
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("LOCALAPPDATA", filepath.Join(home, "local"))
	t.Setenv("APPDATA", filepath.Join(home, "roaming"))
	t.Setenv("XMDEV_HOME", "")
	t.Setenv("XMDEV_FORCE_HOME", "")
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, "xdg"))
	t.Setenv("PATH", t.TempDir())
	t.Setenv("XDEV_PM_ROOT_PREVIEW_CHILD", "1")
	dir := filepath.Join(home, "explicit")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS == "linux" {
		dir = ""
		legacy := filepath.Join(home, ".xdev")
		if err := os.MkdirAll(legacy, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Join(home, "xdg"), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(legacy, "config.yaml"), []byte("schema_version: 1\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("XDEV_PM_ROOT_PREVIEW_DIR", dir)
	snapshot := func() map[string]string {
		t.Helper()
		result := map[string]string{}
		err := filepath.Walk(home, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			relative, _ := filepath.Rel(home, path)
			if info.IsDir() {
				result[relative] = "directory"
				return nil
			}
			data, err := os.ReadFile(path)
			result[relative] = string(data)
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	before := snapshot()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	child := exec.Command(exe, "-test.run=^TestPMPreviewRootReadOnly$")
	out, err := child.CombinedOutput()
	if err != nil {
		t.Errorf("root preview: %v\n%s", err, out)
	}
	if !bytes.Contains(out, []byte("[dry-run]")) {
		t.Errorf("no plan: %s", out)
	}
	if after := snapshot(); !reflect.DeepEqual(before, after) {
		t.Errorf("preview changed filesystem\nbefore=%v\nafter=%v", before, after)
	}
}
