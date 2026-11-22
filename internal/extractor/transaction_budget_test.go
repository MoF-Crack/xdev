package extractor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExtractRejectsRawControlNamesBeforePublishing(t *testing.T) {
	for _, name := range []string{"line\nname.txt", "line\n\nPath = second.txt", "line\rname.txt"} {
		t.Run(strings.ReplaceAll(name, "\n", "_"), func(t *testing.T) {
			dir := t.TempDir()
			src := filepath.Join(dir, "a.zip")
			dst := filepath.Join(dir, "out")
			createTestZip(t, src, []string{"good.txt|ok", name + "|bad"})
			if err := Extract(src, dst, "zip", 0); err == nil {
				t.Error("raw control character accepted")
			}
			if _, err := os.Stat(dst); !os.IsNotExist(err) {
				t.Errorf("failed extraction published output: %v", err)
			}
		})
	}
}
func TestExtractNeverMergesWithExistingTree(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "a.zip")
	dst := filepath.Join(dir, "out")
	os.Mkdir(dst, 0755)
	os.WriteFile(filepath.Join(dst, "keep"), []byte("old"), 0600)
	createTestZip(t, src, []string{"new.txt|new"})
	if err := Extract(src, dst, "zip", 0); err == nil {
		t.Error("unexpected merge into occupied destination")
	}
	if fileExists(filepath.Join(dst, "new.txt")) || readFile(t, filepath.Join(dst, "keep")) != "old" {
		t.Error("existing tree changed")
	}
}
