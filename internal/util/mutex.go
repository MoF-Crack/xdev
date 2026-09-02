// Package util provides common utility functions for the xdev project.
//
// This file (mutex.go) implements cross-process named mutexes to prevent
// concurrent xdev operations (like Activate/Uninstall) from racing against
// each other. On Windows it uses named mutexes via CreateMutexW; on Unix
// it falls back to file locks.
package util

import (
	"crypto/sha256"
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
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
func AcquireToolLock(xmdeDir, toolName string, timeout time.Duration) (*ToolLock, error) {
	if runtime.GOOS == "windows" {
		// Namespace the named mutex by the xdev home so two different
		// installations (or two test sandboxes) sharing a session never
		// contend on the same tool name. The Unix path already derives
		// its lock file from xmdeDir.
		return acquireWindowsMutex(toolLockName(xmdeDir, toolName), timeout)
	}
	return acquireFileBasedToolLock(xmdeDir, toolName, timeout)
}

// toolLockName builds a collision-free mutex key from the home + tool.
func toolLockName(xmdeDir, toolName string) string {
	sum := sha256.Sum256([]byte(strings.ToLower(filepath.Clean(xmdeDir)) + "\x00" + toolName))
	return fmt.Sprintf("xdev_%x_%s", sum[:8], toolName)
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

// acquireFileBasedToolLock uses a file lock as a cross-process mutex on Unix.
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
