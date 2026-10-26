//go:build windows

package cli

import (
	"os/exec"
	"strings"
)

// pmIsAdmin 报告当前进程是否以管理员权限运行。
//
// 用 `net session` 探测：它需要管理员权限才能成功，
// 且不依赖外部包、不需要 syscall 绑定。
// 失败（非管理员或命令不存在）一律视为非管理员 —— 保守方向正确：
// 会提示用户提权，而不是在无权限时盲目执行安装脚本。
func pmIsAdmin() bool {
	out, err := exec.Command("net", "session").CombinedOutput()
	if err != nil {
		return false
	}
	// 中文/英文系统输出不同，只要不报错就说明有权限。
	return !strings.Contains(strings.ToLower(string(out)), "access is denied")
}
