//go:build linux || darwin || freebsd || openbsd || netbsd || dragonfly

package extractor

import "golang.org/x/sys/unix"

func availableBytes(dir string) (uint64, error) {
	var st unix.Statfs_t
	if err := unix.Statfs(dir, &st); err != nil {
		return 0, err
	}
	return uint64(st.Bavail) * uint64(st.Bsize), nil
}
