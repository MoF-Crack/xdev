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
	//
	// 审查 S-08（2026-10-24）：此处原为 `release, _ := jobKillOnClose(); defer release()`。
	// 但 release() 会清零 JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE，而 defer 在 **panic
	// 路径也会执行** —— panic 属于「异常终止」，恰恰是 Job 应当连坐子树的场景，
	// 却被提前关掉了标志，孙进程反而变成孤儿（与 job_windows.go 的设计目标相反）。
	// 已实测：复现同样的 defer 结构后 panic，jobHasKillOnClose 返回 false。
	//
	// 现改为：只在 cmd.Run() **正常返回**后才 release()。panic 时标志保留，
	// 进程崩溃 → 系统关闭 Job 句柄 → 整棵子树被终止。
	release, jobOK := jobKillOnClose()

	runErr := cmd.Run()
	if jobOK {
		// 子进程已退出（无论成败），此时清掉 kill-on-close 不会误杀任何东西，
		// 且能保证「shim 正常退出后，工具自己 daemon 化的孙进程继续存活」。
		release()
	}
	if runErr != nil {
		if exitErr, ok := runErr.(*exec.ExitError); ok {
			return &ExitCodeError{Code: exitErr.ExitCode()}
		}
		return fmt.Errorf("run %s: %w", target, runErr)
	}
	return nil
}
