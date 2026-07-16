// Package util provides common utility functions for the xdev project.
//
// This file (filelock.go) implements cross-process file locking for
// protecting concurrent operations on shared resources (lockfile, PATH,
// registry). Platform-specific implementations are in filelock_windows.go
// and filelock_unix.go.
package util

import (
	"fmt"
	"os"
	"time"
)

// FileLock represents an acquired cross-process file lock.
// The lock is held until Release() is called. The underlying file
// descriptor is kept open for the duration of the lock.
type FileLock struct {
	file *os.File
}

// AcquireFileLock attempts to acquire an exclusive (write) file lock
// on the given path. The lock file is created if it doesn't exist.
//
// timeout controls how long to wait if the lock is already held by
// another process. A timeout of 0 means "fail immediately if locked".
// A negative timeout means "wait indefinitely".
//
// Returns the FileLock (which must be Released) or an error.
func AcquireFileLock(path string, timeout time.Duration) (*FileLock, error) {
	dir := ""
	if idx := lastIndexAny(path, "/\\"); idx >= 0 {
		dir = path[:idx]
	}
	if dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("create lock dir: %w", err)
		}
	}

	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open lock file: %w", err)
	}

	fl := &FileLock{file: f}

	if timeout == 0 {
		// Non-blocking: try once
		if err := fl.tryLockExclusive(); err != nil {
			f.Close()
			return nil, fmt.Errorf("file is locked by another process: %s", path)
		}
		return fl, nil
	}

	// Blocking with timeout
	deadline := time.Now().Add(timeout)
	if timeout < 0 {
		deadline = time.Now().Add(24 * time.Hour)
	}

	for {
		if err := fl.tryLockExclusive(); err == nil {
			return fl, nil
		}
		if time.Now().After(deadline) {
			f.Close()
			return nil, fmt.Errorf("timeout acquiring file lock on %s after %v", path, timeout)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// AcquireSharedFileLock attempts to acquire a shared (read) file lock.
// This allows multiple readers but blocks writers.
func AcquireSharedFileLock(path string, timeout time.Duration) (*FileLock, error) {
	dir := ""
	if idx := lastIndexAny(path, "/\\"); idx >= 0 {
		dir = path[:idx]
	}
	if dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("create lock dir: %w", err)
		}
	}

	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open lock file: %w", err)
	}

	fl := &FileLock{file: f}

	if timeout == 0 {
		if err := fl.tryLockShared(); err != nil {
			f.Close()
			return nil, fmt.Errorf("file is locked exclusively by another process: %s", path)
		}
		return fl, nil
	}

	deadline := time.Now().Add(timeout)
	if timeout < 0 {
		deadline = time.Now().Add(24 * time.Hour)
	}

	for {
		if err := fl.tryLockShared(); err == nil {
			return fl, nil
		}
		if time.Now().After(deadline) {
			f.Close()
			return nil, fmt.Errorf("timeout acquiring shared file lock on %s after %v", path, timeout)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// Release unlocks the file and closes the underlying file descriptor.
func (fl *FileLock) Release() error {
	if fl.file == nil {
		return nil
	}
	err := fl.unlock()
	fl.file.Close()
	fl.file = nil
	return err
}

// lastIndexAny finds the last occurrence of any character from chars in s.
func lastIndexAny(s, chars string) int {
	last := -1
	for i := 0; i < len(s); i++ {
		for _, c := range chars {
			if s[i] == byte(c) {
				last = i
			}
		}
	}
	return last
}
