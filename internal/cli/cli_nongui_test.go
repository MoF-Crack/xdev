package cli

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestCLISourcesDoNotImportGUI(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	dir := filepath.Dir(thisFile)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), `github.com/xmdemt/xdev/internal/gui`) {
			t.Errorf("%s imports internal/gui — CLI/shim must not link Wails (Spec 030)", name)
		}
	}
}

func TestLocateGUIBinary_Missing(t *testing.T) {
	dir := t.TempDir()
	_, err := locateGUIBinary(dir, filepath.Join(dir, "xdev"+exeSuffix()))
	if err == nil {
		t.Fatal("expected error when xdev-gui is absent")
	}
	if !strings.Contains(err.Error(), "xdev-gui") {
		t.Errorf("error %q should mention xdev-gui", err)
	}
}

func TestLocateGUIBinary_FindsSibling(t *testing.T) {
	dir := t.TempDir()
	want := filepath.Join(dir, "xdev-gui"+exeSuffix())
	if err := os.WriteFile(want, []byte("gui"), 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := locateGUIBinary(dir, filepath.Join(dir, "xdev"+exeSuffix()))
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Errorf("got %q want %q", got, want)
	}
}
