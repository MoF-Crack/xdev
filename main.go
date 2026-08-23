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
	"fmt"
	"os"
	"time"

	"github.com/xmdemt/xdev/internal/cli"
	"github.com/xmdemt/xdev/internal/manifest"
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
		runGUI()
		return
	}

	// `xdev gui` 子命令需要进程内启动 GUI 的句柄（cli 包不依赖 gui 包，
	// 通过注入避免 CLI 链接语义被破坏——Spec 030 守卫仍成立）。
	cli.SetGUILauncher(runGUI)

	root := cli.NewRootCommand(toolName, toolDesc)

	if err := root.Execute(); err != nil {
		cli.ShutdownSniffScheduler()
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}

	cli.ShutdownSniffScheduler()
	time.Sleep(100 * time.Millisecond)
}
