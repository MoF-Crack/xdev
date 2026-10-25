//go:build !windows

package installer

import "os"

// fileInUse 判断文件是否被其它进程占用（非 Windows 实现）。
//
// 审查（卸载 node.exe 报 unlinkat Access is denied）：Windows 侧用零共享
// 模式的 CreateFileW 做可靠探测（见 file_in_use_windows.go）。
//
// POSIX 上没有等价的“共享模式”概念，但正在执行的二进制会返回
// ETXTBSY（"text file busy"）。用 O_RDWR 打开可覆盖该情形；
// 其它错误（不存在/无权限）不当作占用，交给后续 RemoveAll 报真实错误。
func fileInUse(path string) bool {
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err == nil {
		_ = f.Close()
		return false
	}
	// ETXTBSY = 26 (Linux) / 26 (Darwin)。用字符串匹配避免平台常量差异。
	return err != nil && containsTextBusy(err.Error())
}

func containsTextBusy(msg string) bool {
	const needle = "text file busy"
	for i := 0; i+len(needle) <= len(msg); i++ {
		match := true
		for j := 0; j < len(needle); j++ {
			c := msg[i+j]
			if c >= 'A' && c <= 'Z' {
				c += 'a' - 'A'
			}
			if c != needle[j] {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}
