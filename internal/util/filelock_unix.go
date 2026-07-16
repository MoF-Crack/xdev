//go:build !windows

package util

import (
	"syscall"
)

// tryLockExclusive attempts an exclusive (write) lock without blocking.
func (fl *FileLock) tryLockExclusive() error {
	return syscall.Flock(int(fl.file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
}

// tryLockShared attempts a shared (read) lock without blocking.
func (fl *FileLock) tryLockShared() error {
	return syscall.Flock(int(fl.file.Fd()), syscall.LOCK_SH|syscall.LOCK_NB)
}

// unlock releases the file lock.
func (fl *FileLock) unlock() error {
	return syscall.Flock(int(fl.file.Fd()), syscall.LOCK_UN)
}
