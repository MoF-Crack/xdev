//go:build windows

package extractor

import (
	"errors"
	"golang.org/x/sys/windows"
)

func retryPublish(err error) bool {
	return errors.Is(err, windows.ERROR_SHARING_VIOLATION) || errors.Is(err, windows.ERROR_ACCESS_DENIED)
}

func availableBytes(dir string) (uint64, error) {
	p, e := windows.UTF16PtrFromString(dir)
	if e != nil {
		return 0, e
	}
	var free uint64
	e = windows.GetDiskFreeSpaceEx(p, &free, nil, nil)
	return free, e
}
