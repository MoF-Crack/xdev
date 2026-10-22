//go:build !bindings

// Package main is the single-entry binary (Spec 034+ 审计：双二进制合并)。
// 一个可执行程序按调用形态多态执行：
//   - 无参数        → 启动 GUI（桌面应用）
//   - xdev tui      → 启动 TUI（子命令）
//   - xdev gui      → 进程内启动 GUI（子命令）
//   - 其他参数      → 作为 CLI 命令执行（install/use/list/...）
//
// 代价：CLI 二进制现在链接 Wails（体积 +~3MB）；收益：每平台只需编译/分发一个
// 可执行文件，不再维护 xdev + xdev-gui 双产物。
package main

import (
	"embed"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/xmdemt/xdev/internal/cli"
	"github.com/xmdemt/xdev/internal/manifest"
	"github.com/xmdemt/xdev/internal/shim"
	"github.com/xmdemt/xdev/internal/signer"
)

//go:embed manifests/*.yaml
var manifestFS embed.FS

//go:embed manifests/*.yaml.sig
var sigFS embed.FS

const (
	toolName = "xdev"
	toolDesc = "XDev - Development Environment Manager - " +
		"Cross-platform dev environment manager (CLI/TUI/GUI)"
)

func init() {
	manifest.InitEmbedded(manifestFS)
	signer.SetEmbeddedSignaturesFS(sigFS)
}

func main() {
	// 单二进制多态入口：无参数 → GUI；其余交给 Cobra CLI
	// （tui/gui 子命令与普通命令由命令树分派）。
	if len(os.Args) <= 1 {
		// 零参数调用的工具 shim（python/node 等进 REPL 的工具）必须照常
		// 分发，不能落入 GUI 分支（Spec 035 F1）。MaybeExec 对
		// xdev/xdev-*/xdev-gui*/wailsbindings 名字在 Lookup 前短路返回
		// false，真 GUI 启动零影响。错误处理与 root.go 同构。
		if handled, err := shim.MaybeExec(filepath.Base(os.Args[0])); handled {
			if err != nil {
				if exitErr, ok := err.(*shim.ExitCodeError); ok {
					os.Exit(exitErr.Code)
				}
				fmt.Fprintln(os.Stderr, "shim:", err)
				os.Exit(1)
			}
			os.Exit(0)
		}
		runGUI()
		return
	}

	// `xdev gui` 子命令需要进程内启动 GUI 的句柄（cli 包不依赖 gui 包，
	// 通过注入避免 CLI 链接语义被破坏——Spec 030 守卫仍成立）。
	cli.SetGUILauncher(runGUI)

	root := cli.NewRootCommand(toolName, toolDesc)

	execErr := root.Execute()
	// 票 34（P1-30 / E-11）：等待条件必须在 Shutdown 之前判定——Shutdown
	// 会把 sniff-cache 的 last_sniff_at 刷成 now，令 interval 策略的 TTL
	// 判定失真（从「到期」翻成「新鲜」）。谓词语义见 cli.HasStartupSniff。
	startupSniffPending := cli.HasStartupSniff()
	// 第十一轮审计（Round 10 报告待办 6）：收尾刷盘失败必须**可见**（此前静默）。
	//
	// 刻意**不影响退出码**：票 33（P1-29 / E-10）确立的契约是「退出码原样反映子进程
	// 真实退出码」，一次收尾缓存刷盘失败不该覆盖它 —— 那会让 CI 脚本把「工具跑成功
	// 但缓存没刷上」误判成命令失败。因此只写 stderr 提示，退出码仍由下面的
	// execErr / exitCodeForError 决定。
	if shutdownErr := cli.ShutdownSniffScheduler(); shutdownErr != nil {
		fmt.Fprintf(os.Stderr, "xdev: warning: %v\n", shutdownErr)
	}

	if execErr != nil {
		// 票 33（P1-29 / E-10）：子进程退出码原样传播——isolate exec /
		// shim 转发把子进程退出码包在 *shim.ExitCodeError 里，此前这里
		// 无条件 os.Exit(1)，调用方（CI 脚本、"xdev isolate exec -- go test"）
		// 永远看不到真实退出码。errors.As 兼容被 %w 包装的错误链，
		// 与本文件上方 shim 分发路径（:52-59）的先例同构。
		if code, ok := exitCodeForError(execErr); ok {
			os.Exit(code)
		}
		fmt.Fprintln(os.Stderr, "error:", execErr)
		os.Exit(1)
	}

	// 票 34（P1-30 / E-11）：固定 100ms 等待改为「本次（交互入口）确实
	// 触发了后台嗅探」才等待——一次性子命令（list/install/config/...）
	// 与 cd-hook 路径（hook/env）不再付出这 100ms。
	if startupSniffPending {
		time.Sleep(100 * time.Millisecond)
	}
}

// exitCodeForError maps a command error to the process exit code that main
// must use (票 33 / P1-29 / E-10).  *shim.ExitCodeError (isolate exec, shim
// dispatch) carries the child process exit code and must be propagated
// verbatim through a wrapped error chain; everything else is a generic
// failure (exit 1, caller prints the error).  The bool reports whether the
// error carried an explicit child exit code.
func exitCodeForError(err error) (int, bool) {
	var ece *shim.ExitCodeError
	if !errors.As(err, &ece) {
		return 1, false
	}
	code := ece.Code
	if code == 0 {
		// 矛盾输入（有错误却报 0）：退化为通用失败，绝不静默成功。
		code = 1
	}
	return code, true
}
