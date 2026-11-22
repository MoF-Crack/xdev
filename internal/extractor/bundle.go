package extractor

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"
)

func (x *extraction) symlink(root, out, target string) error {
	if target == "" || !utf8.ValidString(target) || strings.IndexFunc(target, unicode.IsControl) >= 0 || strings.ContainsAny(target, `\:`) || strings.HasPrefix(target, "/") || !isSafePath(root, filepath.Join(filepath.Dir(out), target)) {
		return fmt.Errorf("unsafe symlink %s -> %q", out, target)
	}
	if err := os.MkdirAll(filepath.Dir(out), 0755); err != nil {
		return err
	}
	return os.Symlink(target, out)
}

// Walk mounted directories in bounded batches (WalkDir/ReadDir(-1) would
// allocate a whole directory before an entry limit can reject it).
func eachChild(path string, visit func(os.DirEntry) error) error {
	f, e := os.Open(path)
	if e != nil {
		return e
	}
	defer f.Close()
	for {
		es, e := f.ReadDir(64)
		for _, ent := range es {
			if err := visit(ent); err != nil {
				return err
			}
		}
		if e == io.EOF {
			return nil
		}
		if e != nil {
			return e
		}
	}
}
func (x *extraction) copyAppsFromMount(mount string) (int, error) {
	n := 0
	err := eachChild(mount, func(d os.DirEntry) error {
		if !d.IsDir() || !strings.HasSuffix(d.Name(), ".app") {
			_, e := x.entry(d.Name(), 0)
			return e
		}
		e := x.copyBundle(mount, d.Name(), filepath.Join(x.dst, d.Name()))
		if e != nil {
			return fmt.Errorf("copy %s: %w", d.Name(), e)
		}
		n++
		return nil
	})
	return n, err
}
func (x *extraction) copyBundle(mount, rel, appRoot string) error {
	out, e := x.entry(filepath.ToSlash(rel), 0)
	if e != nil {
		return e
	}
	src := filepath.Join(mount, rel)
	if err := checkBundleMetadata(src); err != nil {
		return err
	}
	fi, e := os.Lstat(src)
	if e != nil {
		return e
	}
	switch {
	case fi.IsDir():
		if e = os.MkdirAll(out, 0755); e != nil {
			return e
		}
		return eachChild(src, func(d os.DirEntry) error { return x.copyBundle(mount, filepath.Join(rel, d.Name()), appRoot) })
	case fi.Mode().IsRegular():
		return x.copyRegular(src, out, fi.Mode())
	case fi.Mode()&os.ModeSymlink != 0:
		target, e := os.Readlink(src)
		if e != nil {
			return e
		}
		return x.symlink(appRoot, out, target)
	default:
		return fmt.Errorf("unsupported bundle entry: %s", rel)
	}
}

// Testable without mounting a disk image; the production caller provides its
// already-private staging directory and extraction-wide limits.
func copyAppsFromMount(mount, dst string) (int, error) {
	if e := os.MkdirAll(dst, 0755); e != nil {
		return 0, e
	}
	ctx, cancel := context.WithTimeout(context.Background(), DefaultBudget().Timeout)
	defer cancel()
	x := extraction{ctx: ctx, Budget: DefaultBudget(), dst: dst, buf: make([]byte, 32<<10)}
	return x.copyAppsFromMount(mount)
}
