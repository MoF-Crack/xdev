package util

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
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
// Symlinks/junctions are skipped.
func CopyDir(src, dst string) error {
	return copyDir(src, dst, nil)
}

// CopyDirProgress recursively copies a directory tree and reports progress.
// onProgress is called with (copiedFiles, totalFiles) after each file is
// copied; a nil onProgress skips progress reporting.
func CopyDirProgress(src, dst string, onProgress func(copied, total int)) error {
	return copyDir(src, dst, onProgress)
}

// copyDir is the single implementation behind CopyDir and CopyDirProgress.
// When onProgress is non-nil it is invoked after every copied file.
func copyDir(src, dst string, onProgress func(copied, total int)) error {
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

		if info.IsDir() {
			dirs = append(dirs, target)
		} else {
			// Skip symlinks/junctions
			if info.Mode()&os.ModeSymlink != 0 {
				return nil
			}
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
			if onProgress != nil {
				copiedMu.Lock()
				copied++
				c := copied
				copiedMu.Unlock()
				onProgress(c, total)
			}
		}(task)
	}
	wg.Wait()

	return firstErr
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
