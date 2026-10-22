//go:build windows

package shim

import (
	"os"
	"path/filepath"

	"golang.org/x/sys/windows"
)

// clearReadOnly 递归清掉只读属性（best-effort）。
//
// Windows 上 `os.RemoveAll` 失败最常见的两个原因就是「里面有只读文件」与
// 「文件被占用」。只读可以就地解决；被占用只能等（所以才有按年龄的重试）。
func clearReadOnly(root string) {
	_ = filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil || info == nil {
			return nil //nolint:nilerr // best-effort 清理
		}
		if info.Mode()&0o200 == 0 { // 无写权限位
			_ = os.Chmod(p, info.Mode()|0o200)
		}
		return nil
	})
	_ = windows.SetFileAttributes(
		windows.StringToUTF16Ptr(root),
		windows.FILE_ATTRIBUTE_NORMAL,
	)
}
