package manifest

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWritableManifestUsesCatalogPrecedence(t *testing.T) {
	root := t.TempDir()
	write := func(name, body string) {
		t.Helper()
		p := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write("addon.yaml", "name: addon\nlatest: 1.0.0\n")
	write("third-party/renamed.yaml", "name: addon\nlatest: 2.0.0\n")
	write("backup/addon.yaml", "name: addon\nlatest: 9.0.0\n")
	p, err := ResolveWritableToolPath(root, "addon")
	if err != nil {
		t.Fatal(err)
	}
	if p != filepath.Join(root, "third-party", "renamed.yaml") {
		t.Fatalf("effective write path = %q", p)
	}
	c, err := loadFromDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if tool, _ := c.Tool("addon"); tool.Latest != "2.0.0" {
		t.Fatal("write and read precedence differ")
	}
	p, err = ResolveWritableToolPath(root, "embedded-only")
	if err != nil || p != filepath.Join(root, "embedded-only.yaml") {
		t.Fatalf("new overlay = %q %v", p, err)
	}
	for _, name := range []string{"../addon", ""} {
		if _, err := ResolveWritableToolPath(root, name); err == nil {
			t.Fatalf("unsafe name %q accepted", name)
		}
	}
}
