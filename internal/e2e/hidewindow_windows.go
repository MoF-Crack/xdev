//go:build windows

package e2e

import "syscall"

// hideWindow returns a Windows SysProcAttr that hides the child console.
//
// 2026-10-27：本函数原先内联在 epic1_test.go 里并用 runtime.GOOS 判定，
// 但 `SysProcAttr.HideWindow` 字段**只存在于 Windows 的 syscall 包**，
// 在 Linux/macOS 上是编译错误 —— 运行时守卫救不了编译期缺失。
// e2e 进 CI（跑 ubuntu）后立刻暴露：unknown field HideWindow。
//
// 现按平台拆文件：本文件（Windows）返回实际值，epic1_other.go 返回 nil。
func hideWindow() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{HideWindow: true}
}
