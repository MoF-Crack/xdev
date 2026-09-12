package util

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

// rotateLockTimeout 是获取轮转锁的等待上限：轮转本身是毫秒级操作，
// 5 秒足够跨进程排队，又不会让调用方（审计日志写入）长时间阻塞。
const rotateLockTimeout = 5 * time.Second

// RotateLog checks if the log file at path exceeds maxSize bytes.
// If so, it archives the current file to path.1, path.1 to path.2, etc.,
// keeping at most maxBackups old files. The oldest backup is deleted.
// Returns nil if no rotation was needed or rotation succeeded.
//
// 票 40（P2-20 / F-08）三处加固：
//  1. 轮转先取跨进程文件锁 `<path>.lock`：审计日志可能同时被长驻 GUI 与
//     CLI（或外部进程）写入，无锁时两个进程会各自走一遍 shift 循环，
//     备份链相互覆盖（AcquireFileLock 是 OS 字节范围锁，进程崩溃自动
//     释放，不需要额外的陈旧锁清理）。
//  2. 归档当前文件在 Windows 上改用 copy + truncate（见 archiveCurrent）：
//     对「仍有其他句柄打开」的日志文件 rename 必然失败，会导致审计日志
//     永不轮转。
//  3. 错误不再静默：删除最旧备份/移动备份/归档当前文件失败都返回错误
//     （旧实现里 shift 循环与最旧备份删除的错误被完全丢弃，看起来轮转
//     成功而备份链其实断了）。
func RotateLog(path string, maxSize int64, maxBackups int) error {
	// 取锁后再 stat：等待期间别的进程可能已经完成轮转（文件已归零），
	// 此时本次轮转应当直接判定「无需轮转」。
	lock, err := AcquireFileLock(path+".lock", rotateLockTimeout)
	if err != nil {
		return fmt.Errorf("acquire log rotate lock: %w", err)
	}
	defer lock.Release()

	fi, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // file doesn't exist, nothing to rotate
		}
		return fmt.Errorf("stat log file: %w", err)
	}
	if fi.Size() <= maxSize {
		return nil // no rotation needed
	}

	if maxBackups < 1 {
		// 至少保留 1 份备份：maxBackups==0 时旧实现会在删除 path.0 后仍把
		// 当前文件归档成 path.1（与调用方「不保留备份」的意图不符，且
		// shift 循环的 i 会从 -1 起步而什么都不做）。
		maxBackups = 1
	}

	// Delete the oldest backup if it exists. A missing backup is the normal
	// case before the chain is full, so only real failures are reported.
	oldest := fmt.Sprintf("%s.%d", path, maxBackups)
	if err := os.Remove(oldest); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("rotate log: remove oldest backup %s: %w", oldest, err)
	}

	// Shift backups: .N-1 -> .N, .N-2 -> .N-1, ..., .1 -> .2
	for i := maxBackups - 1; i >= 1; i-- {
		src := fmt.Sprintf("%s.%d", path, i)
		dst := fmt.Sprintf("%s.%d", path, i+1)
		if err := os.Rename(src, dst); err != nil {
			// The backup simply not existing yet is normal (chain not full).
			if os.IsNotExist(err) {
				continue
			}
			return fmt.Errorf("rotate log: shift %s -> %s: %w", src, dst, err)
		}
	}

	// Archive current file to .1
	if err := archiveCurrent(path, path+".1"); err != nil {
		return fmt.Errorf("rotate log: %w", err)
	}

	return nil
}

// archiveCurrent moves the live log at path to dst, leaving path empty.
//
// Non-Windows: a plain rename is atomic and cheap.
//
// 票 40（P2-20 / F-08）Windows: rename 一个仍被其他进程/句柄打开的日志文件
// 会失败（GUI 长驻进程持有句柄时轮转永不发生），因此改为 copy + truncate：
//  1. 流式拷贝到唯名临时文件（os.CreateTemp，与 P1-24 的收敛方向一致——
//     固定 "<dst>.tmp" 会被并发写者互相截断），拷贝并 Sync 成功后才 rename
//     到 dst —— 中途失败只删临时文件，绝不留下半截备份覆盖完整的旧备份；
//  2. 备份确认落盘后才把源文件截断为 0：即使截断失败，日志内容也只多不少
//     （宁可下次再轮转，也不能丢日志）。
func archiveCurrent(path, dst string) error {
	if runtime.GOOS != "windows" {
		return os.Rename(path, dst)
	}

	src, err := os.OpenFile(path, os.O_RDWR, 0o644)
	if err != nil {
		return fmt.Errorf("open log for rollover: %w", err)
	}
	defer src.Close()

	out, err := os.CreateTemp(filepath.Dir(dst), filepath.Base(dst)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create backup temp for %s: %w", dst, err)
	}
	tmp := out.Name()
	if err := out.Chmod(0o644); err != nil { // CreateTemp 是 0600，还原日志文件权限
		out.Close()
		os.Remove(tmp)
		return fmt.Errorf("chmod backup temp %s: %w", tmp, err)
	}

	_, copyErr := io.Copy(out, src)
	if copyErr == nil {
		copyErr = out.Sync()
	}
	if closeErr := out.Close(); copyErr == nil {
		copyErr = closeErr
	}
	if copyErr != nil {
		os.Remove(tmp) // 失败不留半截备份
		return fmt.Errorf("copy log to backup %s: %w", dst, copyErr)
	}
	if err := os.Rename(tmp, dst); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("activate backup %s: %w", dst, err)
	}

	if err := src.Truncate(0); err != nil {
		return fmt.Errorf("truncate log %s: %w", path, err)
	}
	if err := src.Sync(); err != nil {
		return fmt.Errorf("sync truncated log %s: %w", path, err)
	}
	return nil
}

// OpenLogForAppend opens a log file for append, rotating if needed.
// This replaces the common pattern of os.OpenFile with O_APPEND.
func OpenLogForAppend(path string, maxSize int64, maxBackups int) (*os.File, error) {
	// Ensure directory exists
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create log dir: %w", err)
	}

	// Rotate if needed before opening.
	//
	// 票 40（P2-20 / F-08）：错误不再静默吞掉——调用方（downloader 的审计
	// 日志）依赖轮转控制文件体积，静默失败意味着日志无限增长而无人知晓。
	// 这里仍继续打开文件（保持既有契约：轮转失败不阻断审计写入），但把
	// 失败原因写进 log，使「审计日志永不轮转」可被观测。
	if err := RotateLog(path, maxSize, maxBackups); err != nil {
		log.Printf("[logrotate] WARNING: failed to rotate %s (continuing without rotation): %v", path, err)
	}

	return os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
}
