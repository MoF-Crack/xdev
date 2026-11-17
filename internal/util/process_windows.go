//go:build windows

package util

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"time"
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

// TrackProcessGroup cancels the command tree before the direct child exits.
// GUI-created commands are not shim processes and do not inherit shim Job
// Objects. taskkill /T targets the still-owned child and its descendants.
func TrackProcessGroup(cmd *exec.Cmd) {
	cmd.WaitDelay = 5 * time.Second
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		root := os.Getenv("SystemRoot")
		if root == "" {
			return cmd.Process.Kill()
		}
		killCtx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
		defer cancel()
		kill := exec.CommandContext(killCtx, filepath.Join(root, "System32", "taskkill.exe"), "/PID", strconv.Itoa(cmd.Process.Pid), "/T", "/F")
		HideWindow(kill)
		if err := kill.Run(); err != nil {
			return cmd.Process.Kill()
		}
		return nil
	}
}
