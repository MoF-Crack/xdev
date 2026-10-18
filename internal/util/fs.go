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
	"time"
)

// bufPool provides reusable 1MB buffers for concurrent file copies.
var bufPool = sync.Pool{
	New: func() interface{} {
		buf := make([]byte, 1024*1024) // 1MB buffer
		return &buf
	},
}

// CopyFile copies a single file from src to dst using a pooled 1MB buffer.
//
// Ticket 38 (P2-32): permission bits come from the SOURCE file (forcing
// 0755 turned copied data/config files executable), and an existing
// destination keeps its own mode (a copy must not re-permission a file the
// user or another component already owns).
func CopyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	mode := os.FileMode(0o755)
	if fi, statErr := in.Stat(); statErr == nil {
		mode = fi.Mode().Perm()
	}
	if fi, statErr := os.Stat(dst); statErr == nil {
		mode = fi.Mode().Perm()
	}

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
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

// IsAllZeroHash reports whether s is the canonical all-zero SHA-256
// placeholder: exactly 64 hexadecimal zeros, optionally prefixed with
// "0x"/"0X". Used to detect placeholder hashes in manifests that should be
// skipped (and TOFU-pinned) rather than verified.
//
// Ticket 39 (P2-12): the previous version accepted ANY all-zero string —
// including a single "0" or a truncated value — so a malformed hash was
// silently treated as "placeholder, skip verification". Requiring the
// canonical 64-digit width (what the signing side emits) makes a truncated
// value fail loudly instead.
func IsAllZeroHash(s string) bool {
	s = strings.TrimPrefix(s, "0x")
	s = strings.TrimPrefix(s, "0X")
	return len(s) == 64 && strings.Trim(s, "0") == ""
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
//
// 第九轮审计 A-10（P1-24 本体迁移）：临时文件必须**唯名**。
//
// 旧实现用固定 `<path>.tmp`。本函数是**所有调用点的共享底层** ——
// internal/config（home 缓存）、internal/manifest（文本级 manifest 改写）、
// internal/discovery（sniff 缓存）、internal/isolator（隔离状态）等全部经它落盘。
// 两个并发写者共享同一个中间文件时会：
//   - **跨写者内容污染**：写者 A 写 tmp("AAAAAAAA")、写者 B 写 tmp("BBBBBBBB")、
//     A 执行 rename → 目标文件内容是 "BBBBBBBB"（A 发布了 B 的字节）；
//   - **大量并发失败**：16 写者 × 40 轮实测 610 次
//     `rename ... The process cannot access the file because it is being used by
//     another process` / `The system cannot find the file specified`
//     （一个写者已把 tmp rename 走，另一个的 rename 便找不到源）。
//
// 仓库守卫 internal/config/atomicwrite_convergence_test.go 早已把本文件登记进
// fixedTmpExceptions（注释：「P1-24 本体 AtomicWriteFile，待迁移 CreateTemp」）——
// 本次完成该迁移。改用 os.CreateTemp 后每个写者持有自己的临时文件，rename 成为
// 「自己的临时文件 → 目标」的原子发布，不再互相截断或污染。
//
// 命名沿用仓库既有约定 `<base>.tmp-*`（与 internal/config 的 writeHomeCache、
// internal/update/policy.go 的 writeJSONAtomic 一致），使守卫的固定名正则
// （`+ ".tmp"`）不会误报。
func AtomicWriteFile(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	base := filepath.Base(path)

	f, err := os.CreateTemp(dir, base+".tmp-*")
	if err != nil {
		return fmt.Errorf("create temp file in %s: %w", dir, err)
	}
	tmpPath := f.Name()

	// 任何失败路径都必须清掉临时文件。
	cleanup := func() {
		f.Close()
		os.Remove(tmpPath)
	}

	if _, err := f.Write(data); err != nil {
		cleanup()
		return fmt.Errorf("write tmp %s: %w", tmpPath, err)
	}
	// 先落盘再发布：崩溃时不会发布空/半截文件。
	if err := f.Sync(); err != nil {
		cleanup()
		return fmt.Errorf("sync tmp %s: %w", tmpPath, err)
	}
	if err := f.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("close tmp %s: %w", tmpPath, err)
	}
	// CreateTemp 固定 0600；显式套用调用方要求的权限位。
	//（Windows 上仅只读位有效，但不要因此丢掉 perm 语义。）
	if err := os.Chmod(tmpPath, perm); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("chmod tmp %s: %w", tmpPath, err)
	}
	// Windows 上多个进程/goroutine 同时以 MoveFileEx(REPLACE_EXISTING) 覆盖同一
	// 目标会偶发 ERROR_ACCESS_DENIED（已知行为，不是调用方的错）——直接冒泡会把
	// 一次正常的并发写当成失败。与仓库既有收敛实现（internal/config 的
	// writeFileCreateTemp → renameWithRetry，票 31）保持一致，做一次有界重试。
	if err := renameWithRetry(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("rename %s -> %s: %w", tmpPath, path, err)
	}
	return nil
}

// renameWithRetry 以有界重试窗口执行 rename。
//
// Windows 上多个进程/goroutine 同时以 MoveFileEx(REPLACE_EXISTING) 覆盖同一目标
// 时会偶发 ERROR_ACCESS_DENIED：直接冒泡会把这个正常的并发写当失败。重试窗口
// ~55ms，超时后原样返回错误。
//
// 与 internal/config 的同名实现语义一致（那边是包内私有函数，本包不能引用，
// 故在此保留一份等价实现；两边都由各自的并发守卫钉住）。
func renameWithRetry(oldPath, newPath string) error {
	const attempts = 10
	var err error
	for i := 0; i < attempts; i++ {
		if err = os.Rename(oldPath, newPath); err == nil {
			return nil
		}
		time.Sleep(time.Duration(i+1) * time.Millisecond)
	}
	return err
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
