//go:build !windows

package env

import (
	"os"
	"path/filepath"
)

// documentsDir 是 PowerShell profile 定位在非 Windows 平台的占位实现。
//
// pwshProfile / psProfile 只在 Windows 分支（detectWindowsShell）使用；
// 这里保留与旧行为等价的路径推导，避免把这套「文档目录」语义带进 Unix。
func documentsDir() string {
	if home := os.Getenv("HOME"); home != "" {
		return filepath.Join(home, "Documents")
	}
	return filepath.Join(os.Getenv("USERPROFILE"), "Documents")
}
