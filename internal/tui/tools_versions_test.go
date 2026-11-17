package tui

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestViewTools_PerToolActiveVersions is the regression for the field report:
// every installed row showed the selected tool's version (go/python rows showed
// node's 26.5.0 simply because node was selected).
func TestViewTools_PerToolActiveVersions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation requires elevated privileges on Windows")
	}
	m := newTUITestModelWithTools(t, "go", "node", "python")

	install := m.config.Core.InstallDir
	want := map[string]string{"go": "1.27.3", "node": "26.5.0", "python": "3.14.6"}
	if err := os.MkdirAll(filepath.Join(m.config.Core.XMDevDir, "current"), 0o755); err != nil {
		t.Fatal(err)
	}
	for tool, ver := range want {
		target := filepath.Join(install, tool, ver)
		if err := os.MkdirAll(target, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, filepath.Join(m.config.Core.XMDevDir, "current", tool)); err != nil {
			t.Fatal(err)
		}
	}

	m.toolIdx = 1 // node selected — this used to leak into every row
	m.activeVersion = "26.5.0"

	out := m.viewTools(70, 20)
	for tool, ver := range want {
		line := ""
		for _, l := range strings.Split(out, "\n") {
			if strings.Contains(l, " "+tool+" ") {
				line = l
				break
			}
		}
		if line == "" {
			t.Fatalf("row for %q not found in:\n%s", tool, out)
		}
		if !strings.Contains(line, ver) {
			t.Errorf("%s row = %q, want version %q", tool, line, ver)
		}
		for otherTool, otherVer := range want {
			if otherTool == tool {
				continue
			}
			if otherVer != ver && strings.Contains(line, otherVer) {
				t.Errorf("%s row %q leaked %s's version %q", tool, line, otherTool, otherVer)
			}
		}
	}
}
