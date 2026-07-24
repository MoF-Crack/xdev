//go:build windows

package util

import (
	"os/exec"
	"syscall"
)

// HideWindow sets the SysProcAttr on an exec.Cmd to prevent the
// child process from creating a visible console window on Windows.
func HideWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: 0x08000000, // CREATE_NO_WINDOW
	}
}

// ExeName appends ".exe" on Windows.
func ExeName(name string) string {
	return name + ".exe"
}
