//go:build !windows

package installer

import "os"

func renameLegacyDirectory(oldPath, newPath string, _ os.FileInfo) error {
	return os.Rename(oldPath, newPath)
}
