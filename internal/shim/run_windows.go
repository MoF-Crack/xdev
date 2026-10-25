//go:build windows

package shim

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"

	"github.com/xmdemt/xdev/internal/env"
	"github.com/xmdemt/xdev/internal/envctx"
)

// 控制台判定所需的 Win32 API。
//
// 为什么不能只看 GetConsoleWindow 是否非 0：实测在 VS Code / FunIDE 的
// **集成终端**里跑，GetConsoleWindow() 返回非 0（例如 7081206），但
// IsWindowVisible 是 false —— 集成终端把 conhost 的窗口隐藏了，只把
// 输出管道接给终端面板。只看非 0 会误判为「用户交互场景」而保留弹窗。
var (
	kernel32DLL       = syscall.NewLazyDLL("kernel32.dll")
	procGetConsoleWin = kernel32DLL.NewProc("GetConsoleWindow")
	user32DLL         = syscall.NewLazyDLL("user32.dll")
	procIsWindowVis   = user32DLL.NewProc("IsWindowVisible")
)

// hasVisibleConsole 判断本进程是否挂着一个**真实可见**的控制台窗口。
//
// 为什么需要它（2026-10-25，用户报告「频繁弹出 go/python 终端窗口，
// 且 python 窗口关掉会自动重建」）：
//
// shim（E:\XDev\shims\*.exe）是 **GUI 子系统**二进制（PE Subsystem=2），
// 它自己没有控制台。当 IDE 扩展 / MCP 服务把 shim 当控制台工具调用时：
//   1. Windows 为 shim 分配一个新的控制台（conhost）；
//   2. shim 再 exec 真正的工具时没有 HideWindow，子进程继承那个控制台，
//      而该控制台的窗口是**可见**的 → 用户看到弹窗；
//   3. 语言服务器（jedi-language-server）与 MCP server 都是长驻进程，
//      于是窗口一直不销毁，用户关掉后 IDE 重启它又出现。
//
// 反向约束：用户在终端里手动敲 `python` / `go` 时，**必须**保留窗口
// （否则交互式工具无法显示输出）。因此不能无条件 HideWindow。
//
// 判定标准 = 窗口存在 且 可见：
//   - 普通 cmd/PowerShell 窗口：hwnd!=0 且 visible=true → 不隐藏（正确）
//   - IDE 集成终端：hwnd!=0 但 visible=false → 隐藏（避免弹窗）
//   - 无控制台（GUI 宿主调用）：hwnd==0 → 隐藏（避免弹窗）
func hasVisibleConsole() bool {
	if procGetConsoleWin.Find() != nil || procIsWindowVis.Find() != nil {
		// 极端情况下拿不到 API：保守认为有可见控制台，保持原行为（不隐藏），
		// 宁可弹窗也不要把交互式工具的输出弄丢。
		return true
	}
	hwnd, _, _ := procGetConsoleWin.Call()
	if hwnd == 0 {
		return false
	}
	visible, _, _ := procIsWindowVis.Call(hwnd)
	return visible != 0
}

// childSysProcAttrFor 根据「是否需要隐藏子进程窗口」构造 SysProcAttr。
// hide==false 返回 nil（保持默认继承行为，交互式终端不受影响）。
//
// 单独抽成函数是为了可测试：run() 的决策分支能被单元测试直接覆盖，
// 不依赖测试进程自身有没有控制台。
func childSysProcAttrFor(hide bool) *syscall.SysProcAttr {
	if !hide {
		return nil
	}
	return &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: 0x08000000, // CREATE_NO_WINDOW
	}
}

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
//
// 2026-10-25（修复控制台窗口弹窗）：当 shim 自身没有可见控制台（被 IDE 扩展 /
// MCP 服务等无窗口宿主调用）时，给子进程加 CREATE_NO_WINDOW + HideWindow，
// 避免每个长驻工具（语言服务器、MCP server）都弹出一个关不掉的终端窗口。
// 用户在终端里调用时 hasOwnConsole()==true，行为完全不变。
func run(target string, args []string, ctx *envctx.Context) error {
	cmd := exec.Command(target, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	// 2026-10-25（P2）：先叠加注册表里已有的工具环境，再叠加本次解析的
	// 进程级上下文。后者优先 —— 它对应「当前激活版本」，比注册表里的
	// 全局值更新（多版本并存时尤其重要）。
	cmd.Env = env.EnrichFromRegistry(os.Environ())
	if ctx != nil && !ctx.Empty() {
		cmd.Env = ctx.Apply(cmd.Env)
	}

	// 无可见控制台 ⇒ 被无窗口宿主 / IDE 集成终端调用 ⇒ 隐藏子进程窗口。
	// 有可见控制台 ⇒ 用户交互场景 ⇒ 保持继承，不干扰 stdin/stdout。
	cmd.SysProcAttr = childSysProcAttrFor(!hasVisibleConsole())

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
