// is_windows_admin_other.go — 非 Windows 实现 of isWindowsAdmin (stub)。
//
//go:build !windows

package gui

// isWindowsAdmin 在非 Windows 平台始终返回 false。
// 调用方应在 runtime.GOOS=="windows" 时才依赖其返回值；
// installPackageManagerViaScript 的平台检查会先拦截不支持的平台。
func isWindowsAdmin() bool {
	return false
}