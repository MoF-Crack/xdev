//go:build !windows

package util

import (
	"os/exec"
	"syscall"
)

// HideWindow is a no-op on non-Windows platforms.
func HideWindow(cmd *exec.Cmd) {}

// DetachConsole is a no-op on non-Windows platforms.
func DetachConsole(cmd *exec.Cmd) {}

// TrackProcessGroup starts the child in its own process group and makes
// context cancellation kill the whole group. Without this, cancelling an
// install (Ctrl-C / timeout) only kills the direct child, and installer
// helpers such as rustup-init leave grandchildren writing into the install
// tree (Windows has Job Objects for the same purpose).
func TrackProcessGroup(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); err != nil {
			return cmd.Process.Kill()
		}
		return nil
	}
}

// ExeName returns the name unchanged on non-Windows platforms.
func ExeName(name string) string {
	return name
}
