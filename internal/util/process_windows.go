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

// DetachConsole attaches Windows CREATE_NEW_CONSOLE so long-running
// service children get their own console and do not block the parent.
// Safe to call after HideWindow; replaces SysProcAttr flags as needed.
func DetachConsole(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	// CREATE_NEW_CONSOLE = 0x00000010
	cmd.SysProcAttr.CreationFlags = 0x00000010
	cmd.SysProcAttr.HideWindow = false
}

// ExeName appends ".exe" on Windows.
func ExeName(name string) string {
	return name + ".exe"
}
