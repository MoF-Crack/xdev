//go:build windows

package util

import (
	"path/filepath"
	"strings"
)

// ToLongPath prefixes an absolute Windows path with \\?\ to bypass
// MAX_PATH (~260) when the caller needs to write deeply nested files
// (Spec 032). UNC paths are left untouched. Non-absolute or short
// paths are returned unchanged.
func ToLongPath(p string) string {
	if len(p) == 0 || len(p) <= 248 {
		return p
	}
	if strings.HasPrefix(p, `\\`) {
		return p
	}
	if filepath.IsAbs(p) {
		return `\\?\` + p
	}
	return p
}
