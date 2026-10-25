//go:build windows

package installer

import (
	"syscall"
	"unsafe"
)

var (
	kernel32Proc         = syscall.NewLazyDLL("kernel32.dll")
	procCreateFileW      = kernel32Proc.NewProc("CreateFileW")
	procCloseHandleWin32 = kernel32Proc.NewProc("CloseHandle")
)

const (
	genericReadWin32  = 0x80000000
	genericWriteWin32 = 0x40000000
	openExistingWin32 = 3
	invalidHandleVal  = ^uintptr(0) // INVALID_HANDLE_VALUE
)

// fileInUse 判断文件是否被其它进程占用（Windows 实现）。
//
// 审查（卸载 node.exe 报 unlinkat Access is denied）：用 CreateFileW 以
// **零共享模式**（dwShareMode = 0）打开目标文件 —— 只要有别的进程持有该
// 文件的句柄（尤其正在执行的 exe 映像），这次打开就会失败并返回
// ERROR_SHARING_VIOLATION (32)。这是 Windows 上判断「文件能否被删除」
// 最直接可靠的方式。
//
// 注意：不能用 os.Open 代替 —— Go 的 os.Open 默认以 FILE_SHARE_READ|
// FILE_SHARE_WRITE|FILE_SHARE_DELETE 打开，正在运行的文件照样能打开成功。
func fileInUse(path string) bool {
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return false
	}
	h, _, errno := procCreateFileW.Call(
		uintptr(unsafe.Pointer(p)),
		// 用 READ|WRITE：删除需要 DELETE 权限，而 Windows 对「刚退出、
		// 映像未完全释放」的文件，GENERIC_READ 往往仍能成功。实测对照：
		// .NET File.Open(path, Open, ReadWrite, None) 报「正由另一进程使用」，
		// 而同样参数的 Read 模式却成功 —— 所以必须带 WRITE。
		uintptr(genericReadWin32|genericWriteWin32),
		0, // dwShareMode = 0：独占，任何其它句柄都会让本次打开失败
		0, // lpSecurityAttributes
		uintptr(openExistingWin32),
		0, // dwFlagsAndAttributes
		0, // hTemplateFile
	)
	if h == invalidHandleVal {
		// 32 = ERROR_SHARING_VIOLATION，33 = ERROR_LOCK_VIOLATION。
		// 其它错误（如 5 ACCESS_DENIED）也保守视为「不可删」。
		if errno == syscall.Errno(32) || errno == syscall.Errno(33) || errno == syscall.Errno(5) {
			return true
		}
		// 文件不存在等：不算占用。
		return false
	}
	procCloseHandleWin32.Call(h)
	return false
}
