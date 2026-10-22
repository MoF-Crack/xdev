//go:build integration && !windows

package integration

import "syscall"

// hideWindowAttr returns nil on non-Windows platforms (no console to hide).
func hideWindowAttr() *syscall.SysProcAttr { return nil }
