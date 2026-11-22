//go:build linux

package downloader

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/xmdemt/xdev/internal/util"
)

// A queued writer may already have opened the lock before the cleaner releases
// it. Its descriptor must still exclude a contender that opens the path later.
func TestRemediationE_PreopenedWaiterUsesSameLock(t *testing.T) {
	part := filepath.Join(t.TempDir(), "tool.zip.0123456789abcdef.part")
	if err := os.WriteFile(part, []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}
	lockPath := part + ".lock"
	var waiter *os.File
	defer func() {
		if waiter != nil {
			waiter.Close()
		}
	}()
	_, err := cleanupGenerationWith(part, func(path string) error {
		var e error
		waiter, e = os.OpenFile(lockPath, os.O_RDWR, 0)
		if e != nil {
			return e
		}
		e = syscall.Flock(int(waiter.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if !errors.Is(e, syscall.EWOULDBLOCK) {
			t.Fatalf("waiter during cleanup: %v", e)
		}
		return os.Remove(path)
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := syscall.Flock(int(waiter.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	if fresh, e := util.AcquireFileLock(lockPath, 0); e == nil {
		fresh.Release()
		t.Fatal("fresh writer acquired a different inode")
	}
}
