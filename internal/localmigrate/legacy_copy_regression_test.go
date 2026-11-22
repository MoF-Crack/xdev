package localmigrate

import (
	"github.com/xmdemt/xdev/internal/env"
	"os"
	"path/filepath"
	"testing"
)

func TestLegacyCopyPreservesCompleteSource(t *testing.T) {
	_, restore := env.UseInMemoryUserPathForTesting("")
	t.Cleanup(restore)
	src := t.TempDir()
	dst := filepath.Join(t.TempDir(), "copy")
	for _, name := range []string{"install/tool/1/bin/tool", "manifests/custom.yaml", "shims/tool", "unknown/data", "env/tool.json", "current/note"} {
		p := filepath.Join(src, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(name), 0755); err != nil {
			t.Fatal(err)
		}
	}
	if err := MigrateLegacyDir(src, dst); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"install/tool/1/bin/tool", "manifests/custom.yaml", "shims/tool", "unknown/data", "env/tool.json", "current/note"} {
		for _, root := range []string{src, dst} {
			b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(name)))
			if err != nil || string(b) != name {
				t.Errorf("%s/%s: %s %v", root, name, b, err)
			}
		}
	}
}

func TestLegacyCopyLinks(t *testing.T) {
	for _, kind := range []string{"internal", "external", "broken"} {
		t.Run(kind, func(t *testing.T) {
			src := t.TempDir()
			dst := filepath.Join(t.TempDir(), "copy")
			target := filepath.Join(src, "payload")
			if err := os.WriteFile(target, []byte("contents"), 0755); err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "external":
				target = filepath.Join(t.TempDir(), "payload")
				if err := os.WriteFile(target, []byte("external"), 0600); err != nil {
					t.Fatal(err)
				}
			case "broken":
				target = filepath.Join(src, "absent")
			}
			if err := os.Symlink(target, filepath.Join(src, "link")); err != nil {
				t.Skipf("symlink unavailable: %v", err)
			}
			err := MigrateLegacyDir(src, dst)
			if kind == "internal" {
				if err != nil {
					t.Fatal(err)
				}
				actual, e := filepath.EvalSymlinks(filepath.Join(dst, "link"))
				if e != nil || actual != filepath.Join(dst, "payload") {
					t.Fatal("link not reanchored", actual, e)
				}
			} else {
				if err == nil {
					t.Fatal("unsupported link accepted")
				}
			}
			if _, err := os.Lstat(filepath.Join(src, "link")); err != nil {
				t.Fatal("source link lost", err)
			}
		})
	}
}

func TestLegacyCopyRejectsOverlapAndCollision(t *testing.T) {
	_, restore := env.UseInMemoryUserPathForTesting("")
	t.Cleanup(restore)
	for _, kind := range []string{"nested", "occupied", "relative"} {
		t.Run(kind, func(t *testing.T) {
			src := t.TempDir()
			dst := filepath.Join(t.TempDir(), "copy")
			switch kind {
			case "nested":
				dst = filepath.Join(src, "child")
			case "occupied":
				dst = t.TempDir()
				if err := os.WriteFile(filepath.Join(dst, "sentinel"), []byte("keep"), 0600); err != nil {
					t.Fatal(err)
				}
			case "relative":
				dst = "relative-copy"
			}
			if err := MigrateLegacyDir(src, dst); err == nil {
				t.Fatal("unsafe copy accepted")
			}
			if _, err := os.Stat(src); err != nil {
				t.Fatal("source removed", err)
			}
		})
	}
}
