//go:build !windows

package util

import "os"

// createJunction 在非 Windows 平台上创建目录符号链接。
//
// 第九轮审计 P1-1：Windows 侧的 `cmd /c mklink` 会二次解析命令行导致命令注入，
// 实现已移到 junction_windows.go（自行构造命令行 + 严格字符校验）。
// 非 Windows 走 os.Symlink，不经过任何 shell。
func createJunction(link, target string) error {
	return os.Symlink(target, link)
}
