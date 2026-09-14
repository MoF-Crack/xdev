package util

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func writeIgnoredFile(t *testing.T, dir string, items []map[string]string) {
	t.Helper()
	store := map[string]interface{}{"items": items}
	data, _ := json.Marshal(store)
	if err := os.WriteFile(filepath.Join(dir, ignoredFileName), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLoadIgnoredUpdateNames_FiltersKind(t *testing.T) {
	dir := t.TempDir()
	writeIgnoredFile(t, dir, []map[string]string{
		{"name": "gradle", "kind": "update"},
		{"name": "node", "kind": "local-scan"},
		{"name": "go", "kind": "update"},
	})
	got := LoadIgnoredUpdateNames(dir)
	if !got["gradle"] {
		t.Error("gradle (kind=update) should be ignored")
	}
	if !got["go"] {
		t.Error("go (kind=update) should be ignored")
	}
	if got["node"] {
		t.Error("node (kind=local-scan) must NOT be treated as update-ignored")
	}
}

func TestLoadIgnoredUpdateNames_MissingFile(t *testing.T) {
	dir := t.TempDir()
	got := LoadIgnoredUpdateNames(dir)
	if len(got) != 0 {
		t.Errorf("missing file should yield empty set, got %v", got)
	}
}

func TestLoadIgnoredUpdateNames_CorruptFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ignoredFileName), []byte("{broken"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := LoadIgnoredUpdateNames(dir)
	if len(got) != 0 {
		t.Errorf("corrupt file should yield empty set, got %v", got)
	}
}

func TestLoadIgnoredUpdateNames_EmptyCacheDir(t *testing.T) {
	if got := LoadIgnoredUpdateNames(""); len(got) != 0 {
		t.Errorf("empty cacheDir should yield empty set, got %v", got)
	}
}
