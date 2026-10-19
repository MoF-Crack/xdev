// Package util provides common utility functions for the xdev project.
//
// This file (mutex.go) implements the cross-process tool lock that prevents
// concurrent xdev operations (Activate/Uninstall/switch) from racing.
package util

import (
	"fmt"
	"path/filepath"
	"time"
)

// ToolLock represents a cross-process lock for a specific tool operation.
// It prevents concurrent xdev use/activate/uninstall operations from
// corrupting junctions, environment variables, and PATH entries.
type ToolLock struct {
	release func() error
}

// AcquireToolLock acquires a cross-process lock for the given tool name.
// This ensures that only one xdev process can activate, uninstall, or
// switch versions of a tool at a time.
//
// The lock is automatically released when the returned ToolLock's Release()
// method is called (typically via defer).
//
// timeout controls how long to wait if the lock is held by another process.
// A timeout of 0 means "fail immediately". A negative timeout means "wait
// indefinitely" (capped at 5 minutes for safety).
//
// 第十轮审计 P1-2（P1）：**两个平台现在都走文件锁**，不再在 Windows 上用命名
// 互斥量（`CreateMutexW`）。原因见 `mutex_windows.go` 的历史注释（该文件已删除，
// 关键结论保留在下面的「为什么不是命名互斥量」一节）：
//
//   - 命名互斥量的所有权**线程亲和**，而 Go 的 goroutine 在阻塞后会迁移 OS 线程
//     → `ReleaseMutex` 以 `ERROR_NOT_OWNER` 失败，`CloseHandle` 又**不释放所有权**
//     → 锁一直被原线程占有到线程退出。审计实测 30 次 Activate 中 **6 次漏释放
//     （20%）**；跨进程对照显示父进程会一直等到超时（3.01s），而同线程 Release
//     的对照只要 302ms。
//   - 后果：GUI 常驻 + CLI 并发时，CLI 对同一工具的 install **每次阻塞 5 分钟**
//     再失败、`xdev use` 每次 30 秒，直到 GUI 退出。
//   - `LockFileEx`（`AcquireFileLock`）**没有线程亲和**（所有权绑到文件句柄），
//     且具备命名互斥量所缺的**崩溃自愈**：进程异常退出时 OS 关闭句柄、锁自动释放。
//
// 顺带消除了两条平台的语义漂移 —— 此前 Windows 走命名互斥量（`Global\` 前缀、
// 跨会话）、Unix 走文件锁（per-user），两边的超时与释放语义并不完全一致。
func AcquireToolLock(xmdeDir, toolName string, timeout time.Duration) (*ToolLock, error) {
	return acquireFileBasedToolLock(xmdeDir, toolName, timeout)
}

// Release unlocks the tool mutex.
func (tl *ToolLock) Release() error {
	if tl.release == nil {
		return nil
	}
	err := tl.release()
	tl.release = nil
	return err
}

// acquireFileBasedToolLock uses a file lock as a cross-process mutex.
//
// 锁文件放在 `<xmdeDir>/.locks/<tool>.lock`：锁键天然按 xdev home 隔离，
// 两个不同的 home（或两个测试沙箱）不会在同一个工具名上互相争用。
func acquireFileBasedToolLock(xmdeDir, toolName string, timeout time.Duration) (*ToolLock, error) {
	lockPath := filepath.Join(xmdeDir, ".locks", toolName+".lock")
	fl, err := AcquireFileLock(lockPath, timeout)
	if err != nil {
		return nil, fmt.Errorf("tool lock for %q: %w", toolName, err)
	}
	return &ToolLock{
		release: fl.Release,
	}, nil
}
