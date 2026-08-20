//go:build windows

package shim

import (
	"fmt"

	"golang.org/x/sys/windows"
)

func createHardLink(dst, src string) error {
	from, err := windows.UTF16PtrFromString(src)
	if err != nil {
		return err
	}
	to, err := windows.UTF16PtrFromString(dst)
	if err != nil {
		return err
	}
	if err := windows.CreateHardLink(to, from, 0); err != nil {
		return fmt.Errorf("CreateHardLink %s -> %s: %w", dst, src, err)
	}
	return nil
}
