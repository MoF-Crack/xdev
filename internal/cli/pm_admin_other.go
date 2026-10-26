//go:build !windows

package cli

import "os"

// pmIsAdmin 在非 Windows 平台报告是否以 root 运行。
//
// 用 euid==0 判断：这是 POSIX 下「有系统级写权限」的通行标准。
// 非 root 时返回 false，调用方会提示用户用 sudo 重跑。
func pmIsAdmin() bool {
	return os.Geteuid() == 0
}
