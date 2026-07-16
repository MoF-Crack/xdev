//go:build windows

package extractor

import "os"

func getNlinkUnix(_ os.FileInfo) uint64 {
	return 1 // unreachable on Windows, but needed for compilation
}
