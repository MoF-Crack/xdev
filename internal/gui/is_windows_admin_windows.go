// is_windows_admin_windows.go — Windows 实现 of isWindowsAdmin.
// 用 Windows token elevation API 检查，而非文件探测（L3 审查阻塞项1）。
//
//go:build windows

package gui

import (
	"golang.org/x/sys/windows"
)

// isWindowsAdmin 报告当前进程是否以管理员（elevated token）权限运行。
// 用 OpenProcessToken + GetTokenInformation(TokenElevation) 检查，
// 比 ProgramData 文件探测可靠：不受 ACL/环境变量重定向影响。
func isWindowsAdmin() bool {
	token, err := windows.OpenCurrentProcessToken()
	if err != nil {
		return false
	}
	defer token.Close()
	return token.IsElevated()
}
