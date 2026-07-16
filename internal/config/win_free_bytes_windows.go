//go:build windows

package config

import (
	"syscall"
	"unsafe"
)

// winFreeBytesImpl is the Windows implementation that talks to
// GetDiskFreeSpaceExW via kernel32.dll.  This file is only built on
// Windows; the non-Windows counterpart lives in win_free_bytes_other.go.
func winFreeBytesImpl(drive string) (int64, bool) {
	free, _, _, ok := queryDiskSpace(drive)
	return free, ok
}

// driveTotalBytesImpl asks the kernel for the total size of a
// drive.  Implemented in terms of the same GetDiskFreeSpaceExW
// call.
func driveTotalBytesImpl(drive string) (int64, bool) {
	_, total, _, ok := queryDiskSpace(drive)
	return total, ok
}

// queryDiskSpace invokes GetDiskFreeSpaceExW and returns
// (freeAvail, total, totalFree, ok).
func queryDiskSpace(drive string) (int64, int64, int64, bool) {
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	proc := kernel32.NewProc("GetDiskFreeSpaceExW")
	root, err := syscall.UTF16PtrFromString(drive)
	if err != nil {
		return 0, 0, 0, false
	}
	var freeAvail, totalBytes, totalFree int64
	r1, _, _ := proc.Call(
		uintptr(unsafe.Pointer(root)),
		uintptr(unsafe.Pointer(&freeAvail)),
		uintptr(unsafe.Pointer(&totalBytes)),
		uintptr(unsafe.Pointer(&totalFree)),
	)
	if r1 == 0 {
		return 0, 0, 0, false
	}
	return freeAvail, totalBytes, totalFree, true
}
