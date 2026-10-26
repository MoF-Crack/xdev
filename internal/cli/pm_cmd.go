// pm_cmd.go 实现 `xdev pm` 命令族：列出与安装包管理器。
//
// # 为什么 CLI 需要这个（2026-10-25 架构收口）
//
// 包管理器定义原本硬编码在 internal/gui 里，GetPackageManagers 只有 GUI
// 能调 —— 命令行用户完全看不到「本机有哪些包管理器、哪些能装」。
// 定义搬到 manifests/package-managers.yaml（internal/pm 加载）后，
// CLI 与 GUI 共享同一份数据源，本文件提供对应的命令行入口。
//
// 安全边界：install 只会执行数据文件里声明的 install_command。
// 该文件随 manifest 一起分发、一起进入签名覆盖范围 —— 不再是「改一行
// Go 代码就能改掉用户机器上会执行的命令」。
package cli

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/spf13/cobra"

	"github.com/xmdemt/xdev/internal/pm"
)

// newPMCmd 构建 `xdev pm` 命令族。
func newPMCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "pm",
		Short: "Inspect and install package managers",
		Long: `pm lists the package managers xdev knows about and can install
the ones that ship an official install script.

Package manager definitions live in manifests/package-managers.yaml
(distributed and signed with the other manifests).`,
	}
	cmd.AddCommand(newPMListCmd(), newPMInstallCmd())
	return cmd
}

// newPMListCmd 实现 `xdev pm list`。
func newPMListCmd() *cobra.Command {
	var flagAll bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List package managers and whether they are installed",
		RunE: func(cmd *cobra.Command, args []string) error {
			defs := loadPMDefs()

			// 默认只列本平台适用的；--all 列全部。
			type row struct {
				name, display, version, status string
			}
			var rows []row
			for _, d := range defs {
				if !flagAll && !pmPlatformMatches(d.Platform) {
					continue
				}
				installed, version := detectPM(d)
				status := "-"
				switch {
				case installed:
					status = "installed"
				case d.InstallCommand != "":
					status = "installable"
				case d.ToolName != "":
					status = "via xdev install " + d.ToolName
				default:
					status = "manual"
				}
				rows = append(rows, row{d.Name, d.DisplayName, version, status})
			}

			if len(rows) == 0 {
				fmt.Println("(no package managers for this platform; try --all)")
				return nil
			}

			w := 0
			for _, r := range rows {
				if len(r.name) > w {
					w = len(r.name)
				}
			}
			fmt.Printf("%-*s  %-14s  %-10s  %s\n", w, "NAME", "VERSION", "STATUS", "DISPLAY")
			for _, r := range rows {
				v := r.version
				if v == "" {
					v = "-"
				}
				fmt.Printf("%-*s  %-14s  %-10s  %s\n", w, r.name, v, r.status, r.display)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&flagAll, "all", false, "list package managers for all platforms")
	return cmd
}

// newPMInstallCmd 实现 `xdev pm install <name>`。
//
// 只支持带 install_command 的包管理器（brew/scoop/choco）。
// 其余情况给出明确指引：要么用 `xdev install <tool>`，要么按 hint 手动装。
func newPMInstallCmd() *cobra.Command {
	var flagDryRun bool
	cmd := &cobra.Command{
		Use:   "install <name>",
		Short: "Run the official install script for a package manager",
		Long: `install runs the official install command declared in
manifests/package-managers.yaml for the given package manager.

Only package managers that declare install_command are supported.
Others either come with an xdev tool (use 'xdev install <tool>') or
must be installed manually.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := strings.ToLower(strings.TrimSpace(args[0]))
			defs := loadPMDefs()

			d, ok := pm.Find(defs, name)
			if !ok {
				return fmt.Errorf("unknown package manager %q (see 'xdev pm list --all')", name)
			}

			if !pmPlatformMatches(d.Platform) {
				return fmt.Errorf("%q does not support this platform (declared platform: %q)", d.Name, d.Platform)
			}

			// 已经装了就不重复执行安装脚本。
			if installed, version := detectPM(d); installed {
				fmt.Printf("%s is already installed", d.DisplayName)
				if version != "" {
					fmt.Printf(" (version %s)", version)
				}
				fmt.Println()
				return nil
			}

			if d.InstallCommand == "" {
				// 区分两种「不能 script install」的情况，给出可执行指引。
				if d.ToolName != "" {
					return fmt.Errorf("%q has no install script; install it via xdev: xdev install %s",
						d.Name, d.ToolName)
				}
				hint := d.InstallHint
				if hint == "" {
					hint = "see the project homepage"
				}
				return fmt.Errorf("%q has no install script; %s", d.Name, hint)
			}

			if d.RequiresAdmin && !pmIsAdmin() {
				return fmt.Errorf("%q requires administrator privileges; re-run from an elevated shell", d.Name)
			}

			shell, shellArgs := installShell(d.ExecutionEnv)
			if shell == "" {
				return fmt.Errorf("%q declares unsupported execution_env %q", d.Name, d.ExecutionEnv)
			}

			if flagDryRun {
				fmt.Printf("[dry-run] would run via %s:\n  %s\n", d.ExecutionEnv, d.InstallCommand)
				return nil
			}

			fmt.Printf("Installing %s via %s...\n  %s\n", d.DisplayName, d.ExecutionEnv, d.InstallCommand)

			c := exec.Command(shell, append(shellArgs, d.InstallCommand)...)
			c.Stdout = os.Stdout
			c.Stderr = os.Stderr
			c.Stdin = os.Stdin
			if err := c.Run(); err != nil {
				return fmt.Errorf("install script failed: %w", err)
			}

			fmt.Printf("\n%s install script finished.\n", d.DisplayName)
			fmt.Println("Open a new shell (or re-run 'xdev pm list') to pick up the change.")
			return nil
		},
	}
	cmd.Flags().BoolVar(&flagDryRun, "dry-run", false, "print the command without executing it")
	return cmd
}

// loadPMDefs 从配置的 manifest 目录加载包管理器定义。
func loadPMDefs() []pm.Def {
	dir := ""
	if cfg := Cfg(); cfg != nil {
		dir = cfg.Core.ManifestDir
	}
	return pm.LoadOrDefault(dir)
}

// detectPM 检测包管理器是否已安装并取版本号。
//
// 与 GUI 侧的检测语义一致：exec.LookPath 找得到就算已安装，
// 再跑一次 --version 取版本（失败不算未安装 —— 有些工具 --version 返回非零）。
func detectPM(d pm.Def) (bool, string) {
	if d.DetectCmd == "" {
		return false, ""
	}
	if _, err := exec.LookPath(d.DetectCmd); err != nil {
		return false, ""
	}
	out, err := exec.Command(d.DetectCmd, d.DetectArgs...).Output()
	if err != nil && len(out) == 0 {
		return true, ""
	}
	return true, firstVersionToken(string(out))
}

// firstVersionToken 从 --version 输出里取第一个像版本号的 token。
//
// 不同工具的格式差异很大（"npm 10.2.0" / "v20.11.0" / "pip 23.3.1 from ..."），
// 取第一个含数字且以数字开头的 token 足够用，且不会把工具名当版本。
func firstVersionToken(s string) string {
	for _, tok := range strings.Fields(s) {
		tok = strings.TrimPrefix(tok, "v")
		if tok == "" {
			continue
		}
		if tok[0] >= '0' && tok[0] <= '9' {
			// 去掉可能的尾随标点
			return strings.TrimRight(tok, ",;:)")
		}
	}
	return ""
}

// pmPlatformMatches 判断 platform 声明是否适用于当前系统。
//
// 与 GUI 侧 pmPlatformMatches 同源语义：
//
//	windows / darwin / linux → 精确匹配
//	unix                     → darwin 或 linux
//	any 或空                 → 全部
func pmPlatformMatches(platform string) bool {
	switch strings.ToLower(strings.TrimSpace(platform)) {
	case "", "any":
		return true
	case "unix":
		return runtime.GOOS == "darwin" || runtime.GOOS == "linux"
	default:
		return runtime.GOOS == strings.ToLower(platform)
	}
}

// installShell 把 execution_env 映射成 (可执行文件, 前置参数)。
func installShell(env string) (string, []string) {
	switch strings.ToLower(strings.TrimSpace(env)) {
	case "powershell":
		// pwsh 优先（跨平台），回退到 Windows PowerShell。
		if _, err := exec.LookPath("pwsh"); err == nil {
			return "pwsh", []string{"-NoProfile", "-Command"}
		}
		if _, err := exec.LookPath("powershell"); err == nil {
			return "powershell", []string{"-NoProfile", "-Command"}
		}
		return "", nil
	case "bash":
		if _, err := exec.LookPath("bash"); err == nil {
			return "bash", []string{"-c"}
		}
		return "", nil
	default:
		return "", nil
	}
}
