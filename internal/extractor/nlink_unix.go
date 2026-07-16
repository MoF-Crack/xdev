//go:build !windows

package extractor

import (
	"os"
	"syscall"
)

func getNlinkUnix(info os.FileInfo) uint64 {
	if stat, ok := info.Sys().(*syscall.Stat_t); ok {
		return uint64(stat.Nlink)
	}
	return 1
}
