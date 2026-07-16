//go:build integration && windows

package integration

import "syscall"

// hideWindowAttr returns a SysProcAttr that hides the child console window
// so integration tests do not pop up cmd windows on Windows.
func hideWindowAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{HideWindow: true}
}