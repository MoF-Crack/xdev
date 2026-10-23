//go:build windows

package shim

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/xmdemt/xdev/internal/env"
)

// run starts the real tool and waits. Windows has no execve overlay.
// EnrichFromRegistry injects HKCU tool homes the current shell may
// not have received after WM_SETTINGCHANGE (M-14).
//
// 2026-10-23（进程树收口）：把 shim 自己加入一个 kill-on-close 的 Job Object，
// 让**异常终止**（上层 taskkill / 崩溃）连带终止整棵子树，避免真实工具变孤儿。
// 正常退出时 release() 会先清掉该标志，因此 `gradle --daemon` 这类故意
// daemon 化的工具不受影响。详见 job_windows.go。
//
// Job 建立失败时保持旧行为（只记日志）—— 进程管理不能因它失败而拒绝执行工具。
func run(target string, args []string) error {
	cmd := exec.Command(target, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = env.EnrichFromRegistry(os.Environ())

	// 必须在 Start 之前加入：子进程在创建时继承 Job 归属（无竞态）。
	// release 必须在 cmd.Run() 之后执行 —— 它负责「正常退出不连坐」。
	release, _ := jobKillOnClose()
	defer release()

	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return &ExitCodeError{Code: exitErr.ExitCode()}
		}
		return fmt.Errorf("run %s: %w", target, err)
	}
	return nil
}
