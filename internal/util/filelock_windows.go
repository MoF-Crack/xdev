//go:build windows

package util

import (
	"os"
	"syscall"
	"unsafe"
)

var (
	kernel32           = syscall.NewLazyDLL("kernel32.dll")
	procLockFileEx     = kernel32.NewProc("LockFileEx")
	procUnlockFile     = kernel32.NewProc("UnlockFile")
)

const (
	_LOCKFILE_EXCLUSIVE_LOCK   = 0x00000002
	_LOCKFILE_FAIL_IMMEDIATELY = 0x00000001
)

// tryLockExclusive attempts an exclusive (write) lock without blocking.
// Returns error if the lock is held by another process.
func (fl *FileLock) tryLockExclusive() error {
	flags := uint32(_LOCKFILE_EXCLUSIVE_LOCK | _LOCKFILE_FAIL_IMMEDIATELY)
	return lockFileEx(fl.file, flags)
}

// tryLockShared attempts a shared (read) lock without blocking.
// Returns error if an exclusive lock is held by another process.
func (fl *FileLock) tryLockShared() error {
	flags := uint32(_LOCKFILE_FAIL_IMMEDIATELY)
	return lockFileEx(fl.file, flags)
}

// unlock releases the file lock.
func (fl *FileLock) unlock() error {
	handle := fl.file.Fd()
	ret, _, err := procUnlockFile.Call(
		uintptr(handle),
		0,
		0,
		0xFFFFFFFF,
		0xFFFFFFFF,
	)
	if ret == 0 {
		return err
	}
	return nil
}

func lockFileEx(f *os.File, flags uint32) error {
	var overlapped syscall.Overlapped
	handle := f.Fd()
	ret, _, err := procLockFileEx.Call(
		uintptr(handle),
		uintptr(flags),
		0,
		0xFFFFFFFF, // lock entire file (low dword)
		0xFFFFFFFF, // lock entire file (high dword)
		uintptr(unsafe.Pointer(&overlapped)),
	)
	if ret == 0 {
		return err
	}
	return nil
}
