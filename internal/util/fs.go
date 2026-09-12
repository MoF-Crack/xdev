package util

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

// bufPool provides reusable 1MB buffers for concurrent file copies.
var bufPool = sync.Pool{
	New: func() interface{} {
		buf := make([]byte, 1024*1024) // 1MB buffer
		return &buf
	},
}

// CopyFile copies a single file from src to dst using a pooled 1MB buffer.
func CopyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	defer out.Close()

	buf := bufPool.Get().(*[]byte)
	defer bufPool.Put(buf)

	_, err = io.CopyBuffer(out, in, *buf)
	return err
}

// CopyDir recursively copies a directory tree from src to dst.
// It uses concurrent file copying for improved throughput.
// Symlinks/junctions are resolved to their targets and copied as real
// files/directories (needed for Scoop apps/*/current junctions).
func CopyDir(src, dst string) error {
	return copyDir(src, dst, nil, nil)
}

// CopyDirProgress recursively copies a directory tree and reports progress.
// onProgress is called with (copiedFiles, totalFiles) after each file is
// copied; a nil onProgress skips progress reporting.
func CopyDirProgress(src, dst string, onProgress func(copied, total int)) error {
	return copyDir(src, dst, onProgress, nil)
}

// CopyDirReport recursively copies a directory tree and additionally
// reports how many files were copied and how many link entries were
// skipped (broken or unresolvable links — ticket 09 / N-01: every skip
// must be observable because callers may RemoveAll the source right
// after a "successful" copy).
func CopyDirReport(src, dst string, onProgress func(copied, total int)) (files, skipped int, err error) {
	var stats copyStats
	if err := copyDir(src, dst, onProgress, &stats); err != nil {
		return stats.files, stats.skipped, err
	}
	return stats.files, stats.skipped, nil
}

// copyStats accumulates per-copy statistics for CopyDirReport.
type copyStats struct {
	files   int
	skipped int
}

// copyDir is the single implementation behind CopyDir and CopyDirProgress.
// When onProgress is non-nil it is invoked after every copied file.
// stats, when non-nil, receives the copied-file and skipped-link counts.
func copyDir(src, dst string, onProgress func(copied, total int), stats *copyStats) error {
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}

	// Collect all file copy tasks first
	type copyTask struct {
		src string
		dst string
	}
	var tasks []copyTask
	var dirs []string

	// Walk the source tree to collect directories and files
	err := filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)

		// Ticket 09 (audit-fixes / P0-04 + N-01): symlink AND Windows
		// junction entries (junctions surface as ModeIrregular, not
		// ModeSymlink) are resolved to their real targets and copied as
		// real content — needed for Scoop apps/*/current junctions.
		if info.Mode()&(os.ModeSymlink|os.ModeIrregular) != 0 {
			resolved, rerr := filepath.EvalSymlinks(path)
			if rerr != nil || resolved == path {
				// EvalSymlinks returns a trailing junction unchanged
				// (verified empirically on Go 1.26/Windows) and fails on
				// cyclic/dangling links — fall back to os.Readlink,
				// which understands junctions.
				if target, lerr := os.Readlink(path); lerr == nil && target != path {
					if abs, aerr := filepath.Abs(target); aerr == nil {
						resolved = abs
					} else {
						resolved = target
					}
					rerr = nil
				} else {
					// Unresolvable link entry (self-referential, cyclic,
					// or a non-link ModeIrregular file): visible skip
					// rather than a silent drop (N-01) — callers may
					// RemoveAll the source right after a "successful"
					// copy, so every loss must be observable.
					log.Printf("[copydir] WARN: unresolvable link entry %s: %v", path, rerr)
					if stats != nil {
						stats.skipped++
					}
					return nil
				}
			}
			fi, serr := os.Stat(resolved)
			if serr != nil {
				// Broken link entry — same visibility rule as above
				// (N-01): warn and count instead of silently skipping.
				log.Printf("[copydir] WARN: broken link entry %s -> %s: %v", path, resolved, serr)
				if stats != nil {
					stats.skipped++
				}
				return nil
			}
			if fi.IsDir() {
				if samePath(resolved, path) {
					// Self-recursion guard (semantic ambiguity — the
					// resolved target IS the link entry itself).
					return nil
				}
				if err := copyDir(resolved, target, onProgress, stats); err != nil {
					return err
				}
				// Ticket 09 (P0-04): NOT filepath.SkipDir — returning
				// SkipDir from a non-directory entry makes filepath.Walk
				// skip the REMAINING SIBLINGS of the parent directory,
				// silently losing everything sorted after the link
				// (unrecoverable when the caller removes the source).
				return nil
			}
			tasks = append(tasks, copyTask{src: resolved, dst: target})
			return nil
		}
		if info.IsDir() {
			dirs = append(dirs, target)
		} else {
			tasks = append(tasks, copyTask{src: path, dst: target})
		}
		return nil
	})
	if err != nil {
		return err
	}

	// Create all directories first (sequential, fast)
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return err
		}
	}

	total := len(tasks)
	copied := 0
	var copiedMu sync.Mutex

	// Copy files concurrently (IO-bound, use limited parallelism)
	limiter := make(chan struct{}, 8) // 8 concurrent file copies
	var wg sync.WaitGroup
	var firstErr error
	var errMu sync.Mutex

	for _, task := range tasks {
		wg.Add(1)
		limiter <- struct{}{}
		go func(t copyTask) {
			defer wg.Done()
			defer func() { <-limiter }()
			if err := CopyFile(t.src, t.dst); err != nil {
				errMu.Lock()
				if firstErr == nil {
					firstErr = err
				}
				errMu.Unlock()
			}
			copiedMu.Lock()
			copied++
			c := copied
			copiedMu.Unlock()
			if onProgress != nil {
				onProgress(c, total)
			}
		}(task)
	}
	wg.Wait()

	if stats != nil {
		stats.files += copied
	}
	return firstErr
}

// samePath reports whether two paths refer to the same entry
// (case-insensitive on Windows, exact elsewhere). EvalSymlinks may return
// a normalized string that differs from the input while referring to the
// same entry, so a plain == comparison is not reliable.
func samePath(a, b string) bool {
	if runtime.GOOS == "windows" {
		return strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
	}
	return filepath.Clean(a) == filepath.Clean(b)
}

// FileSHA256 computes the SHA-256 hash of a file and returns it as a
// lowercase hex string.
func FileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// IsAllZeroHash returns true if s consists entirely of '0' characters
// (optionally prefixed with "0x"). This is used to detect placeholder
// SHA-256 values in manifests that should be skipped.
func IsAllZeroHash(s string) bool {
	s = strings.TrimPrefix(s, "0x")
	s = strings.TrimPrefix(s, "0X")
	if len(s) == 0 {
		return false
	}
	for _, c := range s {
		if c != '0' {
			return false
		}
	}
	return true
}

// CreateJunction creates a Windows junction (directory symlink) from
// link to target. On non-Windows platforms it creates a symlink.
// On Windows it uses `cmd /c mklink /J` (reliable). A native
// DeviceIoControl implementation was prototyped (Spec 032) but
// reverted because the hand-built REPARSE_DATA_BUFFER was fragile
// across path lengths; mklink remains the supported path.
func CreateJunction(link, target string) error {
	if runtime.GOOS != "windows" {
		return os.Symlink(target, link)
	}
	cmd := exec.Command("cmd", "/c", "mklink", "/J", link, target)
	HideWindow(cmd)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("create junction %s -> %s: %v: %s", link, target, err, out)
	}
	return nil
}

// AtomicWriteFile atomically writes data to a file by first writing to a
// temporary file in the same directory, then renaming. This ensures that a
// crash during write leaves the original file intact (no partial writes).
func AtomicWriteFile(path string, data []byte, perm os.FileMode) error {
	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, data, perm); err != nil {
		return fmt.Errorf("write tmp %s: %w", tmpPath, err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("rename %s -> %s: %w", tmpPath, path, err)
	}
	return nil
}

// ActiveVersion reads the current/<tool> symlink to determine the active
// version of a tool. Returns ("", false) when no active version exists.
func ActiveVersion(xmdeDir, toolName string) (string, bool) {
	link := filepath.Join(xmdeDir, "current", toolName)
	target, err := os.Readlink(link)
	if err != nil {
		return "", false
	}
	return filepath.Base(target), true
}

// ActiveInstallPath returns the install path of the currently active version
// of a tool by reading the current/<tool> symlink, or "" if no version is active.
func ActiveInstallPath(xmdeDir, toolName string) string {
	link := filepath.Join(xmdeDir, "current", toolName)
	target, err := os.Readlink(link)
	if err != nil {
		return ""
	}
	return target
}
