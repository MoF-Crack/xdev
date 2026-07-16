package util

import (
	"os/exec"
	"runtime"
	"syscall"
)

// HideWindow sets the SysProcAttr on an exec.Cmd to prevent the
// child process from creating a visible console window on Windows.
// On other platforms it is a no-op.
func HideWindow(cmd *exec.Cmd) {
	if runtime.GOOS == "windows" {
		cmd.SysProcAttr = &syscall.SysProcAttr{
			HideWindow:    true,
			CreationFlags: 0x08000000, // CREATE_NO_WINDOW
		}
	}
}

// ExeName appends ".exe" on Windows, returns the name unchanged on
// other platforms.
func ExeName(name string) string {
	if runtime.GOOS == "windows" {
		return name + ".exe"
	}
	return name
}
