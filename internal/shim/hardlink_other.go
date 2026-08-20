//go:build !windows

package shim

import "os"

func createHardLink(dst, src string) error {
	return os.ErrInvalid
}
