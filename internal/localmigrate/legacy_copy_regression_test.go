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
				expected, expectedErr := filepath.EvalSymlinks(filepath.Join(dst, "payload"))
				if e != nil || expectedErr != nil || actual != expected {
					t.Fatal("link not reanchored", actual, expected, e, expectedErr)
				}
				copied, copiedErr := os.Stat(filepath.Join(dst, "payload"))
				resolved, resolvedErr := os.Stat(actual)
				original, originalErr := os.Stat(filepath.Join(src, "payload"))
				if copiedErr != nil || resolvedErr != nil || originalErr != nil || !os.SameFile(copied, resolved) || os.SameFile(original, resolved) {
					t.Fatal("link must resolve to the copied file, not the original source", copiedErr, resolvedErr, originalErr)
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

func TestLegacyCopyLinksThroughAliasedParent(t *testing.T) {
	physical := t.TempDir()
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(physical, alias); err != nil {
		t.Skipf("parent alias unavailable: %v", err)
	}
	src := filepath.Join(physical, "source")
	dst := filepath.Join(alias, "copy")
	if err := os.Mkdir(src, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "payload"), []byte("contents"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("payload", filepath.Join(src, "link")); err != nil {
		t.Fatal(err)
	}
	if err := MigrateLegacyDir(src, dst); err != nil {
		t.Fatal(err)
	}
	actual, actualErr := filepath.EvalSymlinks(filepath.Join(dst, "link"))
	expected, expectedErr := filepath.EvalSymlinks(filepath.Join(dst, "payload"))
	if actualErr != nil || expectedErr != nil || actual != expected {
		t.Fatal("aliased destination link not reanchored", actual, expected, actualErr, expectedErr)
	}
	copyInfo, copyErr := os.Stat(expected)
	sourceInfo, sourceErr := os.Stat(filepath.Join(src, "payload"))
	if copyErr != nil || sourceErr != nil || os.SameFile(copyInfo, sourceInfo) {
		t.Fatal("copy points back into original source", copyErr, sourceErr)
	}
	data, err := os.ReadFile(actual)
	if err != nil || string(data) != "contents" {
		t.Fatal("copied payload changed", string(data), err)
	}
	if _, err := os.Lstat(filepath.Join(src, "link")); err != nil {
		t.Fatal("original source link lost", err)
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
