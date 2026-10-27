//go:build !windows

package e2e

import "syscall"

// hideWindow 在非 Windows 平台返回 nil（不存在子进程控制台窗口问题）。
//
// 与 hidewindow_windows.go 配对。拆文件的原因：`SysProcAttr.HideWindow`
// 字段只在 Windows 的 syscall 包中存在，内联用 runtime.GOOS 判定会在
// 非 Windows 上编译失败。
func hideWindow() *syscall.SysProcAttr {
	return nil
}
