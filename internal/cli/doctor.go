// doctor.go defines the `xdev doctor` command — a diagnostic health check.
//
// Doctor runs a battery of sanity checks:
//  1. XDev Home directory exists and is writable
//  2. All required subdirectories exist (install/, shims/, cache/, etc.)
//  3. Shim directory is in PATH
//  4. Shell RC has xdev cd hook block
//  5. Shim binaries are present for installed tools
//  6. Tool catalog is non-empty
//  7. User PATH has no stale/corrupted entries
//  8. System PATH has no double-backslash or corrupted entries
//
// With --fix, doctor attempts to automatically repair detected issues:
// cleans PATH, installs xdev self, fixes system PATH entries.
//
// The command returns a non-zero exit code if any check fails.
package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/xmdemt/xdev/internal/config"
	"github.com/xmdemt/xdev/internal/discovery"
	"github.com/xmdemt/xdev/internal/env"
	"github.com/xmdemt/xdev/internal/installer"
	"github.com/xmdemt/xdev/internal/manifest"
	"github.com/xmdemt/xdev/internal/util"
)

// doctorFixPlan 集中决定 doctor 允许执行哪些**宿主写**操作。
//
// F-31（P2）：修复前 `--fix` 分支无条件执行 env.CleanupUserPath +
// installer.InstallSelf + env.AppendToUserPath + env.CleanupSystemPath
// （doctor.go 全文 0 处引用 NoPathFix()），`xdev doctor --fix --no-path-fix`
// 实测照样写宿主注册表；CleanupSystemPath 更是直接写 HKLM。
//
// 逐项语义（注意区分「写注册表」与「写 xdev 自己的目录」）：
//   - CleanGOROOT     —— 写 HKCU\Environment（自愈陈旧 GOROOT）；
//   - CleanUserPath   —— 写 HKCU\Environment 的 Path；
//   - AppendUserPath  —— 写 HKCU\Environment 的 Path；
//   - CleanSystemPath —— 写 **HKLM**（机器级，需要管理员），因此除了
//     --no-path-fix 之外还要 `--fix-system-path` 显式 opt-in；
//   - InstallSelf     —— 只写 <xmdevenv_dir>/install/xdev/ 与 current/xdev
//     的 junction，**不碰注册表**，因此不受 --no-path-fix 门控：把它一并
//     关掉会让 `--fix` 失去作用（xdev 自身不再进入 PATH）。
//
// 与 init.go:173 / install.go:85,152 / profile.go:236 / upgrade_cmd.go:32
// 五处 SetNoEnvWrite(NoPathFix()) 同源：同一个 NoPathFix() 决定所有注册表写。
type doctorFixPlan struct {
	CleanGOROOT     bool
	CleanUserPath   bool
	AppendUserPath  bool
	CleanSystemPath bool
	InstallSelf     bool
}

// planDoctorFix 由 --no-path-fix 与 --fix-system-path 推导出允许的写操作集合。
func planDoctorFix(noPathFix, fixSystemPath bool) doctorFixPlan {
	return doctorFixPlan{
		// 用户级注册表写（HKCU\Environment）：统一受 --no-path-fix 门控。
		CleanGOROOT:    !noPathFix,
		CleanUserPath:  !noPathFix,
		AppendUserPath: !noPathFix,
		// 机器级注册表写（HKLM）：门控之外再加显式 opt-in。
		CleanSystemPath: !noPathFix && fixSystemPath,
		// 只写 xdev 自己的目录 —— 始终执行。
		InstallSelf: true,
	}
}

// newDoctorCmd runs a battery of health checks on the local install.
func newDoctorCmd() *cobra.Command {
	var flagFix bool
	var flagFixSystemPath bool
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Run sanity checks on your xdev installation",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := Cfg()
			allOK := true

			// F-31：宿主写操作的单一决策点。planDoctorFix 同时覆盖
			// 下面第 0 步的 GOROOT 自愈与 --fix 分支的全部注册表写。
			fixPlan := planDoctorFix(NoPathFix(), flagFixSystemPath)

			// 0. Self-heal stale GOROOT residue (ticket 01 / audit X-07): the
			//    pre-v2 go.yaml wrote a GOROOT one level above the real
			//    toolchain (strip:0 nested layout), which breaks every
			//    `go build` with "package fmt is not in std". Idempotent and
			//    conservative — only deletes values that provably point at
			//    the parent of a real GOROOT.
			//
			//    F-31：这一步写 HKCU\Environment，因此同样受 --no-path-fix
			//    门控（修复前它在 --fix 分支之外**无条件**执行，连
			//    `xdev doctor --no-path-fix` 都会写宿主注册表）。
			if fixPlan.CleanGOROOT {
				if err := env.CleanupStaleGOROOT(); err != nil {
					fmt.Printf("✘ GOROOT registry cleanup failed: %v\n", err)
					allOK = false
				}
			} else {
				fmt.Println("• GOROOT registry cleanup: skipped (--no-path-fix)")
			}

			check := func(name string, ok bool, msg string) {
				if ok {
					fmt.Printf("✔ %s\n  %s\n", name, msg)
				} else {
					fmt.Printf("✘ %s\n  %s\n", name, msg)
					allOK = false
				}
			}

			// 1. xdev home exists and is writable
			info, err := os.Stat(cfg.Core.XMDevDir)
			check("xmdevenv home", err == nil && info.IsDir(),
				cfg.Core.XMDevDir)

			// 2. every required subdir exists
			for _, d := range []string{cfg.Core.DownloadDir, cfg.Core.InstallDir,
				cfg.Core.ShimDir, cfg.Core.CacheDir, cfg.Core.EnvDir, cfg.Core.ManifestDir} {
				_, err := os.Stat(d)
				check("dir: "+filepath.Base(d), err == nil, d)
			}

			// 3. shim dir in PATH
			shimOK := pathContainsShim(cfg)
			check("shim dir in PATH", shimOK,
				fmt.Sprintf("expected %s to appear in PATH; current PATH entries (first 3): %s",
					cfg.Core.ShimDir, firstPathEntries(os.Getenv("PATH"), 3)))

			// 4. shell RC has the xdev block
			//
			// F-33（P2）：修复前只匹配 env.Marker()（`# >>> xdev init >>>`），
			// 而 `xdev activate` 注入的是 hookMarker（`# >>> xdev hook >>>`）。
			// 两个 marker 三处不一致的后果之一就是这里：activate 成功后
			// doctor 报 `✘ shell rc injected`（假阴性），并因此让 doctor
			// 退出码变非零。
			sh := env.DetectShell()
			if sh.RCFile != "" {
				hasBlock := false
				if data, err := os.ReadFile(sh.RCFile); err == nil {
					hasBlock = hasXdevRCBlock(data)
				}
				check("shell rc injected", hasBlock, sh.RCFile)
			} else {
				fmt.Println("• shell rc injection: not applicable")
			}

			// 5. shim binaries look right
			//
			// 票 40（P2-18 / E-14）：此处原先写死 shimOK=true——ShimDir 被
			// 删除/权限不足时 doctor 仍报 ✔，安装树残迹（llvm 装错工具、
			// typescript 空目录等）因此更不可见。改为由 os.ReadDir 的结果
			// 推导；「零 shim」（新装用户首跑）仍是 info 而非 fail——doctor
			// 的退出码被首跑体验与 CI 断言依赖，不能因零 shim 变化。
			shimsOK, shimMsg := shimDirCheck(cfg.Core.ShimDir)
			check("shims", shimsOK, shimMsg)

			// 6. catalog is non-empty
			cat := loadCatalog()
			tools := cat.Tools()
			check("manifest catalog", len(tools) > 0,
				fmt.Sprintf("%d tool(s) discovered at %s", len(tools), cfg.Core.ManifestDir))

			// 6a. 不可安装条目（票 18 / P2-29）：manifest 里可以有「有版本、没有
			// url/format」的占位条目（实测 mingw 曾整条如此）——`xdev available`
			// 会照常展示，直到用户点安装才失败。这里显式点名，且**不算检查失败**：
			// 平台相关的不可安装（例如 Windows-only 工具在 Linux 上）属正常现象。
			if notInstallable := notInstallableTools(tools); len(notInstallable) > 0 {
				fmt.Printf("\n• Not installable on this platform (%d): %s\n",
					len(notInstallable), strings.Join(notInstallable, ", "))
				fmt.Println("  (manifest lacks url/format for a matching archive, or the tool targets another OS)")
			}

			// 6b. Available updates from sniff cache
			updates := loadSniffUpdates(cfg)
			if len(updates) > 0 {
				fmt.Println("\n• Available updates (from last sniff):")
				for _, u := range updates {
					fmt.Printf("  %-16s ↑ %s\n", u.Tool, u.Available)
				}
				_ = allOK // don't mark as check failure, it's informational
			}

			// 7. Check user PATH for issues
			userPath, _ := env.ReadUserPath()
			userPathIssues := diagnosePathEntries(userPath, cfg.Core.XMDevDir)
			if len(userPathIssues) > 0 {
				fmt.Println("\n⚠ User PATH issues:")
				for _, issue := range userPathIssues {
					fmt.Printf("  - %s\n", issue)
				}
				allOK = false
			} else {
				fmt.Println("\n✔ User PATH: no issues")
			}

			// 8. Check system PATH for issues
			sysPath, _ := env.ReadSystemPath()
			sysPathIssues := diagnosePathEntries(sysPath, "")
			if len(sysPathIssues) > 0 {
				fmt.Println("\n⚠ System PATH issues:")
				for _, issue := range sysPathIssues {
					fmt.Printf("  - %s\n", issue)
				}
				allOK = false
			} else {
				fmt.Println("✔ System PATH: no issues")
			}

			// 9. Check PATH for tools that shadow the shim directory.
			//    On Windows, Machine PATH entries are merged before User PATH entries,
			//    so a system-level tool directory (e.g. D:\Env\Cangjie\bin) can
			//    appear before the shim dir and shadow the shim-managed version.
			shadowIssues := detectPathShadowConflicts(cfg.Core.ShimDir)
			if len(shadowIssues) > 0 {
				fmt.Println("\n⚠ PATH shadow conflicts detected:")
				for _, issue := range shadowIssues {
					fmt.Printf("  - %s\n", issue)
				}
				fmt.Println("  Suggestion: Remove the conflicting directory from Machine (system) PATH,")
				fmt.Println("  or ensure the shim directory appears first in the merged PATH.")
				fmt.Println("  You can also temporarily prepend the shim dir with:")
				fmt.Println("    $env:PATH = \"" + cfg.Core.ShimDir + ";$env:PATH\"")
				allOK = false
			} else {
				fmt.Println("✔ PATH shadow: no conflicts detected")
			}

			// Fix mode
			//
			// F-31（P2）：分支内每一个**写注册表**的动作都由 fixPlan 门控
			// （见 planDoctorFix 的逐项说明）。修复前这里是无条件执行，
			// `--no-path-fix` 被完全忽略，实测会写宿主 PATH。
			if flagFix {
				fmt.Println("\n--- Fixing PATH issues ---")

				if !fixPlan.CleanUserPath {
					fmt.Println("• User PATH cleanup: skipped (--no-path-fix)")
				} else if changed, err := env.CleanupUserPath(cfg.Core.XMDevDir, cfg.Core.ShimDir, cfg.GetPathMode()); err != nil {
					fmt.Printf("✘ Failed to fix user PATH: %v\n", err)
				} else if changed {
					fmt.Println("✔ User PATH cleaned up")
				} else {
					fmt.Println("• User PATH: no changes needed")
				}

				// 将 xdev 自身安装到 install/xdev/<version>/ 并创建 junction
				// 这样 xdev 命令通过 current/xdev 目录在 PATH 中全局可用。
				//
				// F-31：InstallSelf 只写 xdev 自己的目录（install/xdev/ 与
				// current/xdev junction），不写任何注册表键，因此**不受**
				// --no-path-fix 门控——把它一并关掉会让 `--fix` 失去作用。
				if fixPlan.InstallSelf {
					if _, _, err := installer.InstallSelf(cfg); err != nil {
						fmt.Printf("✘ Failed to install xdev self: %v\n", err)
					}
				}
				if !fixPlan.AppendUserPath {
					fmt.Println("• Add xdev dir to user PATH: skipped (--no-path-fix)")
				} else {
					xdevBinDir := filepath.Join(cfg.Core.XMDevDir, "current", "xdev")
					if err := env.AppendToUserPath(xdevBinDir); err != nil {
						fmt.Printf("✘ Failed to add xdev dir to user PATH: %v\n", err)
					} else {
						fmt.Printf("✔ Added xdev dir to user PATH: %s\n", xdevBinDir)
					}
				}

				// Fix system PATH (writes HKLM, requires admin).
				if !fixPlan.CleanSystemPath {
					fmt.Println("• System PATH cleanup: skipped (requires --fix-system-path)")
				} else if changed, err := env.CleanupSystemPath(); err != nil {
					fmt.Printf("✘ Failed to fix system PATH: %v (may need admin privileges)\n", err)
				} else if changed {
					fmt.Println("✔ System PATH cleaned up")
				} else {
					fmt.Println("• System PATH: no changes needed")
				}
			}

			if allOK {
				fmt.Println("\nAll checks passed ✔")
			} else {
				fmt.Println("\nSome checks failed ✘ - see above")
				if !flagFix {
					fmt.Println("Run 'xdev doctor --fix' to attempt automatic fixes")
				}
			}

			if !allOK {
				return fmt.Errorf("some checks failed")
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&flagFix, "fix", false, "Attempt to fix detected issues")
	cmd.Flags().BoolVar(&flagFixSystemPath, "fix-system-path", false,
		"with --fix, also clean the machine-level (HKLM) system PATH — requires admin and an explicit opt-in")
	return cmd
}

// xdevRCMarkers 是 RC 文件中「xdev 已经注入过」的全部可能标记。
//
// F-33（P2）：仓库里存在两套 marker——
//   - env.Marker()（`# >>> xdev init >>>`）：`xdev init` /
//     env.InjectShellRC 写的那一块；
//   - hookMarker（`# >>> xdev hook >>>`）：`xdev activate` 写的那一块；
//   - 以及 env 的历史 marker（`# >>> xmdevenvmt init >>>`）。
//
// 消费侧（doctor 的校验、self uninstall 的清理）此前只认其中一套，
// 于是 activate 之后 doctor 报假阴性、self uninstall 之后 RC 里仍留 hook 块。
// 集中在这里，让两处消费点共用同一份清单。
func xdevRCMarkers() []string {
	markers := []string{hookMarker, env.Marker()}
	// 历史 marker：升级用户 RC 里的旧块也必须算「已注入」，否则同样假阴性。
	// env 包未导出它们，这里按文档化的历史字符串列一份（仅用于识别）。
	markers = append(markers, "# >>> xmdevenvmt init >>>")
	return markers
}

// hasXdevRCBlock 报告 RC 内容里是否存在**任一**已知的 xdev 注入块。
func hasXdevRCBlock(data []byte) bool {
	text := string(data)
	for _, m := range xdevRCMarkers() {
		if m != "" && strings.Contains(text, m) {
			return true
		}
	}
	return false
}

// notInstallableTools 返回本平台不可安装的工具名（排序）——票 18 / P2-29。
//
// 判据是 manifest 派生标记 Tool.Installable（LoadCatalog 计算：本平台上至少有一个
// 「结构完整」的归档，即 url+format，npm 型只要求条目存在）。实测 mingw 曾整条缺失
// url/format：`xdev available` 照常展示、点安装才失败；现在 doctor 会点名。
func notInstallableTools(tools []*manifest.Tool) []string {
	var out []string
	for _, t := range tools {
		if t == nil || t.Installable {
			continue
		}
		// 零版本条目不是「看着能装其实装不了」的工具，而是非工具 YAML 被解析后的
		// 产物（实测 manifests/index.yaml 会成为名为 "index" 的零版本条目；注册侧
		// register.go 同样跳过它）。把它列进「不可安装」只会制造噪声。
		if len(t.VersionList) == 0 {
			continue
		}
		out = append(out, t.Name)
	}
	sort.Strings(out)
	return out
}

// diagnosePathEntries checks a PATH string for common issues.
func diagnosePathEntries(pathStr, xdevDir string) []string {
	var issues []string
	if pathStr == "" {
		return issues
	}
	sep := string(os.PathListSeparator)
	parts := strings.Split(pathStr, sep)
	for i, p := range parts {
		trimmed := strings.TrimSpace(p)
		if trimmed == "" {
			continue
		}
		if runtime.GOOS == "windows" {
			// Windows-specific checks
			// Double backslash
			if strings.Contains(trimmed, "\\\\") {
				issues = append(issues, fmt.Sprintf("entry #%d has double backslashes: %s", i+1, trimmed))
			}
			// Quoted path
			if strings.HasPrefix(trimmed, "\"") || strings.HasPrefix(trimmed, "'") {
				issues = append(issues, fmt.Sprintf("entry #%d is quoted (should not be): %s", i+1, trimmed))
			}
			// xdev install directory in PATH
			if xdevDir != "" && strings.Contains(strings.ToLower(filepath.Clean(trimmed)), "\\xdev\\install\\") {
				issues = append(issues, fmt.Sprintf("entry #%d is xdev install dir (should use shim instead): %s", i+1, trimmed))
			}
		} else {
			// Unix-specific checks
			// xdev install directory in PATH
			if xdevDir != "" && strings.Contains(filepath.Clean(trimmed), "/xdev/install/") {
				issues = append(issues, fmt.Sprintf("entry #%d is xdev install dir (should use shim instead): %s", i+1, trimmed))
			}
		}
		// Cross-platform: Path doesn't exist
		clean := strings.Trim(trimmed, "\"'")
		if _, err := os.Stat(clean); os.IsNotExist(err) {
			issues = append(issues, fmt.Sprintf("entry #%d does not exist: %s", i+1, trimmed))
		}
	}
	return issues
}

// shimDirCheck reports the health of the shim directory for the doctor
// "shims" check.
//
// 票 40（P2-18 / E-14）：检查结论必须来自磁盘事实，而不是常量：
//   - 目录不可读（不存在 / 权限不足 / 路径不是目录）→ ok=false，消息带原因；
//   - 目录可读但没有任何条目 → ok=true 且消息保持「尚无 shim」的 info
//     语义：新装用户首跑时 shim 目录本就为空，这不是故障，改判 fail 会让
//     doctor 退出码从 0 变非 0，破坏首跑体验与既有 CI 断言；
//   - 目录可读且有 N 个条目 → ok=true，消息报告数量。
//
// 拆成独立函数是为了让守卫测试能直接驱动文件系统分支——测试不得构造
// root 命令（进程级 OnInitialize 会把配置解析回真实 home）。
func shimDirCheck(dir string) (bool, string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false, fmt.Sprintf("cannot read shim dir %s: %v", dir, err)
	}
	if len(entries) == 0 {
		return true, "no shims installed yet (shim dir is empty — nothing to check on a fresh install)"
	}
	return true, fmt.Sprintf("%d shim(s) installed", len(entries))
}

// pathContainsShim reports whether the shim dir appears in the
// current process PATH.  On Windows we also try to read the user
// PATH from the registry via PowerShell as a fallback.
func pathContainsShim(cfg *config.Config) bool {
	sep := string(os.PathListSeparator)
	for _, p := range strings.Split(os.Getenv("PATH"), sep) {
		if runtime.GOOS == "windows" {
			if strings.EqualFold(p, cfg.Core.ShimDir) {
				return true
			}
		} else {
			if p == cfg.Core.ShimDir {
				return true
			}
		}
	}
	return false
}

func firstPathEntries(path string, n int) string {
	sep := string(os.PathListSeparator)
	parts := strings.Split(path, sep)
	if len(parts) > n {
		parts = parts[:n]
	}
	return strings.Join(parts, sep)
}

// detectPathShadowConflicts checks whether any PATH entry that appears
// before the shim directory contains executables that could shadow
// shim-managed tool binaries. This is common on Windows where the
// Machine (system) PATH is merged before the User PATH.
//
// Known conflict directories include Cangjie, Java, Go, and other
// tool installation directories that may have been added to the
// system PATH by their installers.
func detectPathShadowConflicts(shimDir string) []string {
	var issues []string
	sep := string(os.PathListSeparator)
	path := os.Getenv("PATH")
	if path == "" {
		return issues
	}

	shimDirClean := strings.ToLower(filepath.Clean(shimDir))
	parts := strings.Split(path, sep)

	// Find the shim directory position in the merged PATH.
	// If shim is missing entirely, treat every entry as potentially
	// shadowing (scan full PATH). If shim is first, no conflict.
	shimPos := -1
	for i, p := range parts {
		clean := strings.ToLower(filepath.Clean(strings.Trim(p, "\" ")))
		if clean == shimDirClean {
			shimPos = i
			break
		}
	}
	if shimPos == 0 {
		return issues
	}
	scanUntil := shimPos
	if scanUntil < 0 {
		scanUntil = len(parts)
	}

	// Known tool binary names that xdev manages via shims.
	// We check whether any entry before the shim dir contains these.
	knownToolBins := []string{
		"cjc.exe", "cjc",
		"java.exe", "java",
		"javac.exe", "javac",
		"go.exe", "go",
		"node.exe", "node",
		"python.exe", "python", "python3.exe", "python3",
		"dotnet.exe", "dotnet",
		"rustc.exe", "rustc",
		"cargo.exe", "cargo",
		"flutter.exe", "flutter",
		"dart.exe", "dart",
		"gradle.exe", "gradle",
		"mvn.exe", "mvn",
		"cmake.exe", "cmake",
		"git.exe", "git",
	}

	for i := 0; i < scanUntil; i++ {
		entry := strings.Trim(parts[i], "\" ")
		if entry == "" {
			continue
		}
		// Check for known tool binaries in this directory.
		for _, bin := range knownToolBins {
			candidate := filepath.Join(entry, bin)
			if fi, err := os.Stat(candidate); err == nil && !fi.IsDir() {
				msg := fmt.Sprintf(
					"entry #%d %q contains %q — this appears BEFORE the shim dir and will shadow the shim-managed version",
					i+1, entry, bin,
				)
				if shimPos < 0 {
					msg = fmt.Sprintf(
						"entry #%d %q contains %q — shim dir %q is missing/late in PATH so system tools take precedence",
						i+1, entry, bin, shimDir,
					)
				}
				issues = append(issues, msg)
				break // one issue per directory entry is enough
			}
		}
	}

	return issues
}

// loadSniffUpdates reads the sniff cache and returns a list of tools
// that have newer versions available than the currently installed one.
// Used by doctor to display available updates.
func loadSniffUpdates(cfg *config.Config) []struct {
	Tool      string
	Installed string
	Available string
} {
	cachePath := filepath.Join(cfg.Core.CacheDir, "sniff-cache.json")
	data, err := os.ReadFile(cachePath)
	if err != nil {
		return nil
	}
	var cache discovery.SniffCache
	if err := json.Unmarshal(data, &cache); err != nil || cache.Tools == nil {
		return nil
	}

	installDir := cfg.Core.InstallDir
	var updates []struct {
		Tool      string
		Installed string
		Available string
	}

	for toolName, result := range cache.Tools {
		if result.Unsupported || result.Error != "" || len(result.Versions) == 0 {
			continue
		}
		// Check if tool is installed.
		toolDir := filepath.Join(installDir, toolName)
		if _, err := os.Stat(toolDir); os.IsNotExist(err) {
			continue
		}
		// Find installed version.
		active, ok := activeVersion(toolName)
		if !ok {
			continue
		}
		// Find latest sniffed version.
		latest := result.Versions[0]
		for _, v := range result.Versions[1:] {
			if util.CompareSemver(v, latest) > 0 {
				latest = v
			}
		}
		if util.CompareSemver(latest, active) > 0 {
			updates = append(updates, struct {
				Tool      string
				Installed string
				Available string
			}{Tool: toolName, Installed: active, Available: latest})
		}
	}
	return updates
}
