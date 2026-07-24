//go:build !windows

package util

import "os/exec"

// HideWindow is a no-op on non-Windows platforms.
func HideWindow(cmd *exec.Cmd) {}

// ExeName returns the name unchanged on non-Windows platforms.
func ExeName(name string) string {
	return name
}
