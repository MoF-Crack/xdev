//go:build !windows

package shim

import (
	"os"
	"path/filepath"
)

// clearReadOnly 在非 Windows 上是空操作（POSIX 的只读位在 RemoveAll 前不需要特殊处理，
// 父目录只要有写权限就能 unlink）。
func clearReadOnly(root string) {
	_ = filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil || info == nil {
			return nil //nolint:nilerr // best-effort 清理
		}
		if info.Mode()&0o200 == 0 {
			_ = os.Chmod(p, info.Mode()|0o200)
		}
		return nil
	})
}
