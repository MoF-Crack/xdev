//go:build windows

package installer

import (
	"errors"
	"fmt"
	"os"
	"time"

	"golang.org/x/sys/windows"
)

// Only the confirmed legacy directory gets this retry. A briefly open member
// without FILE_SHARE_DELETE can make its parent's rename return ACCESS_DENIED.
// Ten attempts, with 1..9ms backoff (45ms total), leave persistent failures
// visible to removeLinkEntry; links are never recursively removed here.
func renameLegacyDirectory(oldPath, newPath string, expected os.FileInfo) error {
	return renameLegacyDirectoryWith(oldPath, newPath, expected, os.Rename, time.Sleep)
}

func renameLegacyDirectoryWith(oldPath, newPath string, expected os.FileInfo, rename func(string, string) error, wait func(time.Duration)) error {
	for attempt := 0; ; attempt++ {
		current, err := os.Lstat(oldPath)
		if err != nil {
			return err
		}
		if !isLegacyRealDir(current) || !os.SameFile(expected, current) {
			return fmt.Errorf("legacy directory changed identity or type: %s", oldPath)
		}
		err = rename(oldPath, newPath)
		if err == nil || attempt == 9 || (!errors.Is(err, windows.ERROR_ACCESS_DENIED) && !errors.Is(err, windows.ERROR_SHARING_VIOLATION)) {
			return err
		}
		wait(time.Duration(attempt+1) * time.Millisecond)
	}
}
