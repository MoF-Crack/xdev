//go:build !windows

package util

// ToLongPath is a no-op on non-Windows platforms (no MAX_PATH limit).
func ToLongPath(p string) string { return p }
