package localmigrate

import (
	"crypto/sha256"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// LegacyDirInfo describes an old root and its suggested replacement.
type LegacyDirInfo struct {
	Path    string `json:"path"`
	NewPath string `json:"newPath"`
}

func ScanLegacyDir() []LegacyDirInfo {
	if runtime.GOOS != "windows" {
		return nil
	}
	var found []LegacyDirInfo
	for c := 'A'; c <= 'Z'; c++ {
		drive := string(c) + ":"
		legacy := filepath.Join(drive, "XimuLocalDev", "xmdevenvmt")
		if fi, err := os.Stat(legacy); err != nil || !fi.IsDir() {
			continue
		}
		_, installErr := os.Stat(filepath.Join(legacy, "install"))
		_, configErr := os.Stat(filepath.Join(legacy, "config.yaml"))
		if installErr == nil || configErr == nil {
			found = append(found, LegacyDirInfo{legacy, filepath.Join(drive, "XDev")})
		}
	}
	return found
}

// MigrateLegacyDir copies a legacy root into an empty destination, verifies every
// copied file, and leaves the source intact. Root config and discovery metadata
// are owned by the caller's subsequent config commit. Arbitrary data, manifests,
// and shims are preserved. Internal symlinks are reanchored; external, broken and
// unsupported special files fail closed. It never changes PATH or deletes data.
// The caller must stop writers to the source and serialize destination writers.
// A failed copy leaves a partial destination for inspection, never a usable
// config commit; use a fresh empty destination on retry.
func MigrateLegacyDir(legacyDir, newDir string) error {
	if !filepath.IsAbs(legacyDir) || !filepath.IsAbs(newDir) || strings.HasPrefix(newDir, `\\`) || strings.HasPrefix(newDir, "//") {
		return fmt.Errorf("source and target must be local absolute paths")
	}
	info, err := os.Lstat(legacyDir)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("source must be a real directory")
	}
	src, err := filepath.EvalSymlinks(legacyDir)
	if err != nil {
		return err
	}
	dst := filepath.Clean(newDir)
	// Resolve the nearest existing ancestor without creating a nested target.
	ancestor := dst
	var suffix []string
	for {
		actual, e := filepath.EvalSymlinks(ancestor)
		if e == nil {
			for i := len(suffix) - 1; i >= 0; i-- {
				actual = filepath.Join(actual, suffix[i])
			}
			dst = actual
			break
		}
		p := filepath.Dir(ancestor)
		if p == ancestor {
			return e
		}
		suffix = append(suffix, filepath.Base(ancestor))
		ancestor = p
	}
	if within(src, dst) || within(dst, src) {
		return fmt.Errorf("source and target directories overlap")
	}
	entries, err := os.ReadDir(dst)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	for _, entry := range entries {
		if entry.Name() != ".root-switch.lock" {
			return fmt.Errorf("target must be empty: %s", dst)
		}
	}
	if err := os.MkdirAll(dst, 0755); err != nil {
		return err
	}
	return filepath.WalkDir(src, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		if filepath.Dir(rel) == "." {
			switch rel {
			case "config.yaml", ".root-redirect.json", ".root-switch.lock", ".home.cache":
				if entry.IsDir() {
					return fmt.Errorf("reserved metadata is a directory: %s", path)
				}
				return nil
			}
		}
		target := filepath.Join(dst, rel)
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			actual, err := filepath.EvalSymlinks(path)
			if err != nil {
				return fmt.Errorf("resolve link %s: %w", path, err)
			}
			if !within(src, actual) {
				return fmt.Errorf("external link requires manual migration: %s", path)
			}
			linkRel, err := filepath.Rel(src, actual)
			if err != nil {
				return err
			}
			// Relative links keep working if the copy is subsequently renamed.
			mapped, err := filepath.Rel(filepath.Dir(target), filepath.Join(dst, linkRel))
			if err != nil {
				return err
			}
			if err := os.Symlink(mapped, target); err != nil {
				return fmt.Errorf("copy link %s: %w", path, err)
			}
			got, err := os.Readlink(target)
			if err != nil || got != mapped {
				return fmt.Errorf("verify link %s: %v", target, err)
			}
			return nil
		}
		if info.IsDir() {
			return os.Mkdir(target, info.Mode().Perm()|0700)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("unsupported file type: %s", path)
		}
		if err := copyVerified(path, target, info.Mode().Perm()); err != nil {
			return err
		}
		return nil
	})
}

func within(root, path string) bool {
	if runtime.GOOS == "windows" {
		root = strings.ToLower(root)
		path = strings.ToLower(path)
	}
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) && !filepath.IsAbs(rel)
}

func copyVerified(src, dst string, mode fs.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	h := sha256.New()
	_, copyErr := io.Copy(io.MultiWriter(out, h), in)
	syncErr := out.Sync()
	closeErr := out.Close()
	if copyErr != nil {
		return copyErr
	}
	if syncErr != nil {
		return syncErr
	}
	if closeErr != nil {
		return closeErr
	}
	check, err := os.Open(dst)
	if err != nil {
		return err
	}
	defer check.Close()
	actual := sha256.New()
	if _, err := io.Copy(actual, check); err != nil {
		return err
	}
	if string(actual.Sum(nil)) != string(h.Sum(nil)) {
		return fmt.Errorf("copy checksum mismatch: %s", dst)
	}
	return nil
}
