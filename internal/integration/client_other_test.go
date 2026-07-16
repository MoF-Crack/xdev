//go:build integration && !windows

package integration

// hideWindowAttr returns nil on non-Windows platforms (no console to hide).
func hideWindowAttr() any { return nil }