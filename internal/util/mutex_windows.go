//go:build windows

package util

import (
	"fmt"
	"log"
	"syscall"
	"time"
	"unsafe"
)

var (
	procCreateMutexW        = kernel32.NewProc("CreateMutexW")
	procWaitForSingleObject = kernel32.NewProc("WaitForSingleObject")
	procReleaseMutex        = kernel32.NewProc("ReleaseMutex")
	procCloseHandle         = kernel32.NewProc("CloseHandle")
)

// acquireWindowsMutex creates or opens a named Windows mutex for the tool.
// Named mutexes are visible across all processes on the same session,
// making them ideal for preventing concurrent xdev operations.
func acquireWindowsMutex(toolName string, timeout time.Duration) (*ToolLock, error) {
	// Create a named mutex: Global\xdev_activate_<toolname>
	// Using "Global\\" prefix makes it visible across all sessions
	// (requires SeCreateGlobalPrivilege, which most users have on workstations)
	name, err := syscall.UTF16PtrFromString(fmt.Sprintf("Global\\xdev_activate_%s", toolName))
	if err != nil {
		return nil, fmt.Errorf("create mutex name for %q: %w", toolName, err)
	}

	handle, _, err := procCreateMutexW.Call(
		0, // default security
		0, // not initial owner
		uintptr(unsafe.Pointer(name)),
	)
	if handle == 0 {
		return nil, fmt.Errorf("CreateMutexW for %q: %w", toolName, err)
	}

	// Wait for the mutex with timeout
	waitResult, _, _ := procWaitForSingleObject.Call(
		handle,
		uintptr(timeoutToMilliseconds(timeout)),
	)

	const (
		WAIT_OBJECT_0  = 0
		WAIT_TIMEOUT   = 0x00000102
		WAIT_ABANDONED = 0x00000080
	)

	switch waitResult {
	case WAIT_OBJECT_0, WAIT_ABANDONED:
		// Mutex acquired (WAIT_ABANDONED means the previous owner crashed,
		// which is fine — we now own the mutex)
		return &ToolLock{
			release: func() error {
				// Ticket 19 (F-03 minimal hardening): ReleaseMutex fails
				// when the calling thread does not own the mutex — which
				// happens if the goroutine that acquired it migrated to
				// another OS thread before releasing (Go's scheduler does
				// this freely). The handle is still closed below so the
				// resource is not leaked, but the failure must be visible:
				// silently ignoring it is how the lock appeared to work
				// while another installer slipped through.
				relRet, _, relErr := procReleaseMutex.Call(handle)
				if relRet == 0 {
					log.Printf("[util] WARNING: ReleaseMutex failed for a tool lock (goroutine migrated threads?) — the lock may not have been released cleanly: %v", relErr)
				}
				procCloseHandle.Call(handle)
				return nil
			},
		}, nil
	case WAIT_TIMEOUT:
		procCloseHandle.Call(handle)
		return nil, fmt.Errorf("tool %q is locked by another xdev process (timeout after %v)", toolName, timeout)
	default:
		procCloseHandle.Call(handle)
		return nil, fmt.Errorf("WaitForSingleObject returned %d for tool %q", waitResult, toolName)
	}
}

// timeoutToMilliseconds converts a time.Duration to Windows INFINITE if
// negative, or to milliseconds otherwise.
func timeoutToMilliseconds(d time.Duration) uint32 {
	if d < 0 {
		return 0xFFFFFFFF // INFINITE
	}
	ms := uint32(d.Milliseconds())
	if ms == 0 {
		return 1 // minimum wait
	}
	return ms
}
