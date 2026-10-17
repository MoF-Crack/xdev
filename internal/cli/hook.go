// hook.go defines the `xdev hook`, `xdev activate`, and `xdev deactivate`
// commands for shell integration.
//
// # hook
//
// Outputs shell-specific export statements for the current environment.
// Designed to be eval'd from a cd hook:
//
//	eval "$(xdev hook --shell bash)"
//
// Supports bash, zsh, fish, and PowerShell output formats.
//
// # activate
//
// Installs a cd hook into the user's shell RC file so that xdev
// automatically switches tool versions when changing directories.
// The hook is wrapped in `# >>> xdev hook >>>` / `# <<< xdev hook <<<`
// markers for idempotent removal.
//
// # deactivate
//
// Removes the cd hook block from the shell RC file.
package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/xmdemt/xdev/internal/config"
	"github.com/xmdemt/xdev/internal/env"
)

// newHookCmd handles `xdev hook --shell <shell>`.
// It outputs shell code to set up the environment for the current
// directory based on .xdev.toml.
func newHookCmd() *cobra.Command {
	var flagShell string
	cmd := &cobra.Command{
		Use:   "hook",
		Short: "Output environment setup for the current directory",
		Long: `hook outputs shell code (exports / set commands) that
configures PATH and environment variables for the current directory.

This is designed to be eval'd from a shell hook:
  eval "$(xdev hook --shell bash)"`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if flagShell == "" {
				flagShell = "bash"
			}
			// No project-based tool management — hook outputs nothing
			return nil
		},
	}
	cmd.Flags().StringVar(&flagShell, "shell", "", "shell type (bash|zsh|fish|pwsh|powershell)")
	return cmd
}

// newActivateCmd handles `xdev activate <shell>`.
// It injects the cd hook into the user's shell RC file.
func newActivateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "activate [shell]",
		Short: "Install the cd hook into your shell RC",
		Long: `activate injects a cd hook into your shell's RC file
so that xdev automatically switches tool versions when you
change directories.

Supported shells: bash, zsh, fish, pwsh, powershell`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := Cfg()
			detected := env.DetectShell()
			requested := ""
			if len(args) == 1 {
				requested = args[0]
			}
			info, err := resolveActivateShell(requested, detected)
			if err != nil {
				return err
			}
			// Inject the hook script into the RC file
			if err := injectHookRC(info, cfg); err != nil {
				return err
			}
			fmt.Printf("✔ cd hook installed for %s (RC: %s)\n", info.Shell, info.RCFile)
			fmt.Println("  Open a new terminal or source your RC file to activate.")
			return nil
		},
	}
}

// resolveActivateShell 决定 activate 实际要写入哪个 shell 的 RC。
//
// F-34（P2）：旧实现只做 `info.Shell = args[0]`，**不重解析 info.RCFile**。
// 后果：Windows 上 `xdev activate zsh` 把 zsh 的 hook 语法
// （add-zsh-hook chpwd …）写进 PowerShell profile——语法与文件错配，用户的
// shell 下次启动即报错；反向（activate pwsh 写 .zshrc）同理。
//
// 修复选择「不一致即报错」而不是「按参数猜 RC 路径」：
//   - 猜路径必然错（zsh 的 RC 位置、fish 的 config.fish、Windows PowerShell
//     与 pwsh 的 profile 目录各不相同，且受 $ZDOTDIR 等环境变量影响）；
//   - 报错把决定权交还用户（改 $SHELL / 用 --shell 一致的 shell 运行 /
//     在目标 shell 里执行 activate），符合「宁可拒绝也不写错文件」的宿主安全取向。
//
// 一致时 RCFile 一律沿用**检测结果**，不做任何改写。
// 只有 pwsh 与 powershell 视为等价——HookScript 对两者返回同一份模板
// （internal/env/hook.go:53 `case "powershell", "pwsh"`），语法确实相同。
func resolveActivateShell(requested string, detected env.ShellInfo) (env.ShellInfo, error) {
	req := strings.ToLower(strings.TrimSpace(requested))
	if req == "" {
		return detected, nil
	}

	got := strings.ToLower(strings.TrimSpace(detected.Shell))
	if req == got || shellSyntaxFamily(req) == shellSyntaxFamily(got) {
		// 用户显式给出的写法可能与检测结果不同名（pwsh vs powershell）；
		// 统一采用检测到的名字，保证打印与 HookScript 分派一致。
		return detected, nil
	}

	return env.ShellInfo{}, fmt.Errorf(
		"refusing to install the %s cd hook: the detected shell is %q (RC: %s), "+
			"and %s syntax would be written into that file — run `xdev activate` from a %s shell, "+
			"or unset/override the shell detection so it matches",
		requested, detected.Shell, detected.RCFile, requested, requested)
}

// shellSyntaxFamily 把 shell 归到「hook 模板族」。HookScript 只为这些族各写
// 一份模板（internal/env/hook.go），同族内互换是安全的。
func shellSyntaxFamily(shell string) string {
	switch shell {
	case "powershell", "pwsh":
		return "powershell"
	default:
		return shell
	}
}

// newDeactivateCmd handles `xdev deactivate`.
// It removes the cd hook from the user's shell RC file.
func newDeactivateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "deactivate",
		Short: "Remove the cd hook from your shell RC",
		RunE: func(cmd *cobra.Command, args []string) error {
			info := env.DetectShell()
			if err := removeHookRC(info); err != nil {
				return err
			}
			fmt.Printf("✔ cd hook removed from %s\n", info.RCFile)
			return nil
		},
	}
}

const hookMarker = "# >>> xdev hook >>>"
const hookEndMarker = "# <<< xdev hook <<<"

// injectHookRC adds the cd hook script to the user's shell RC.
func injectHookRC(info env.ShellInfo, cfg *config.Config) error {
	if info.RCFile == "" {
		return fmt.Errorf("no RC file for shell %s", info.Shell)
	}
	if err := os.MkdirAll(filepath.Dir(info.RCFile), 0o755); err != nil {
		return err
	}

	existing, err := os.ReadFile(info.RCFile)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	text := string(existing)

	// Remove any existing hook block
	text = removeBlock(text, hookMarker, hookEndMarker)

	// Generate the hook script
	hookScript := env.HookScript(info.Shell, cfg.Core.XMDevDir)
	block := fmt.Sprintf("%s\n%s\n%s\n", hookMarker, hookScript, hookEndMarker)
	text += block

	return os.WriteFile(info.RCFile, []byte(text), 0o644)
}

// removeHookRC removes the cd hook block from the user's shell RC.
func removeHookRC(info env.ShellInfo) error {
	if info.RCFile == "" {
		return nil
	}
	data, err := os.ReadFile(info.RCFile)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	text := removeBlock(string(data), hookMarker, hookEndMarker)
	return os.WriteFile(info.RCFile, []byte(text), 0o644)
}

// stripAllXdevRCBlocks 清除 RC 文件里**全部**已知的 xdev 注入块。
//
// F-33（P2）②：`xdev self uninstall` 此前只调 env.RemoveShellRC，而它只认
// env.Marker()（`# >>> xdev init >>>`）与历史 marker——`xdev activate` 写入的
// `# >>> xdev hook >>>` 块留在 RC 里，卸载后 shell 每次换目录仍会调用已经不
// 存在的 xdev（并吞掉错误，属静默残留）。
//
// 实现上两件事都做：env.RemoveShellRC（init/legacy 块，含它自己的幂等语义）
// + 本包的 hook 块移除。
func stripAllXdevRCBlocks(info env.ShellInfo) error {
	if info.RCFile == "" {
		return nil
	}
	if err := env.RemoveShellRC(info); err != nil {
		return err
	}
	return removeHookRC(info)
}

func removeBlock(s, start, end string) string {
	i := strings.Index(s, start)
	if i < 0 {
		return s
	}
	j := strings.Index(s[i:], end)
	if j < 0 {
		return s
	}
	return s[:i] + s[i+j+len(end):]
}
