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
	"github.com/xmdemt/xdev/internal/manifest"
	"github.com/xmdemt/xdev/internal/plugin"
	"github.com/xmdemt/xdev/internal/project"
)

// newHookCmd handles `xdev hook --shell <shell>`.
//
// 2026-10-25（P3）：从空实现恢复为真正输出。
//
// 历史：Spec 012 删掉了 internal/project 与 internal/lockfile 后，
// 本命令退化为 `return nil`，而 `xdev activate` 仍然会向 RC 文件装一个
// 调用它的 cd hook —— 结果是每次换目录都执行一个什么都不输出的命令。
// P3 重建了 internal/project，这里同步恢复输出。
//
// 输出内容：当前目录向上找到的 .xdev.yaml 所声明的工具版本对应的
// PATH 条目与环境变量。设计为由 cd hook eval：
//
//	eval "$(xdev hook --shell bash)"
//
// 无项目配置时输出空字符串（且 exit 0）—— 这是**正常**路径，
// 不能报错，否则每个 cd 都会吐一堆错误。
func newHookCmd() *cobra.Command {
	var flagShell string
	cmd := &cobra.Command{
		Use:   "hook",
		Short: "Output environment setup for the current directory",
		Long: `hook outputs shell code (exports / set commands) that
configures PATH and environment variables for the current directory.

This is designed to be eval'd from a shell hook:
  eval "$(xdev hook --shell bash)"

Version selection comes from .xdev.yaml found by walking up from the
current directory. With no project config it prints nothing.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if flagShell == "" {
				flagShell = "bash"
			}

			cfg, err := project.Find("")
			if err != nil {
				// 无项目配置是正常情况（绝大多数目录）。
				// 读到了但解析失败时也不阻断 shell —— cd hook 不能因为
				// 一个写错的 YAML 就向用户终端刷错误。
				return nil
			}

			appCfg := Cfg()
			installRoot := appCfg.Core.InstallDir

			var binPaths []string
			vars := map[string]string{}

			for _, toolName := range cfg.ToolNames() {
				spec, _ := cfg.Spec(toolName)
				ver, ok := project.ResolveInstalled(installRoot, toolName, spec)
				if !ok {
					continue // 未安装：静默跳过（shim 路径会给出提示）
				}
				dir := project.DirFor(installRoot, toolName, ver)

				// PATH：优先 bin/，没有则用安装根（与 shim.FindInDir 同源逻辑）。
				binDir := filepath.Join(dir, "bin")
				if fi, err := os.Stat(binDir); err == nil && fi.IsDir() {
					binPaths = append(binPaths, binDir)
				} else {
					// strip:0 布局（如 Go 保留顶层 go/ 目录）下二进制在 dir/<tool>/bin。
					nested := filepath.Join(dir, toolName, "bin")
					if fi, err := os.Stat(nested); err == nil && fi.IsDir() {
						binPaths = append(binPaths, nested)
					} else {
						binPaths = append(binPaths, dir)
					}
				}

				// 环境变量：复用 manifest 的声明（需加载 catalog 拿插件）。
				// 路径型值做 Clean —— 与 envctx.Resolve 保持同一套语义，
				// 否则 `export GOPATH='.../1.27.3/../gopath'` 会带着 `..` 进入用户 shell。
				for k, v := range toolEnvVars(appCfg, toolName, dir) {
					if strings.ContainsAny(v, `\\/`) {
						v = filepath.Clean(v)
					}
					vars[k] = v
				}
			}

			out := env.HookOutput(flagShell, binPaths, vars)
			if out != "" {
				fmt.Print(out)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&flagShell, "shell", "", "shell type (bash|zsh|fish|pwsh|powershell)")
	return cmd
}

// toolEnvVars 取某个工具在指定安装目录下的环境变量。
//
// 2026-10-25（架构收口）：改用 plugin.BuildRegistry —— 修复前这里只
// NewGeneric 遍历 catalog，**拿不到 Lua/vfox 插件**，导致插件型工具在
// 项目级 hook 里完全没有 env 变量（Get 必然 miss）。
//
// 失败一律返回空 map：hook 是 cd 热路径，不能因 catalog 损坏向用户终端
// 刷错误（shim 侧有同样的降级约定）。
func toolEnvVars(cfg *config.Config, toolName, installDir string) map[string]string {
	cat, err := manifest.LoadCatalogCached(cfg.Core.ManifestDir, cfg.Core.CacheDir)
	if err != nil {
		return nil
	}
	reg := plugin.BuildRegistry(cat, pluginsDir())
	p, ok := reg.Get(toolName)
	if !ok {
		return nil
	}
	return p.EnvVars(installDir)
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
