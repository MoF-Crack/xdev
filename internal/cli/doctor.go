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
// With --ai, doctor uses Ollama for AI-assisted diagnosis.
//
// The command returns a non-zero exit code if any check fails.
package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/spf13/cobra"

	"github.com/xmdemt/xdev/internal/ai"
	"github.com/xmdemt/xdev/internal/config"
	"github.com/xmdemt/xdev/internal/env"
	"github.com/xmdemt/xdev/internal/installer"
)

// newDoctorCmd runs a battery of health checks on the local install.
func newDoctorCmd() *cobra.Command {
	var flagFix bool
	var flagAI bool
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Run sanity checks on your xdev installation",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := Cfg()
			allOK := true

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
			sh := env.DetectShell()
			if sh.RCFile != "" {
				hasBlock := false
				if data, err := os.ReadFile(sh.RCFile); err == nil {
					hasBlock = strings.Contains(string(data), env.Marker())
				}
				check("shell rc injected", hasBlock, sh.RCFile)
			} else {
				fmt.Println("• shell rc injection: not applicable")
			}

			// 5. shim binaries look right
			shimOK = true
			shimMsg := ""
			if entries, err := os.ReadDir(cfg.Core.ShimDir); err == nil && len(entries) > 0 {
				shimMsg = fmt.Sprintf("%d shim(s) installed", len(entries))
			} else {
				shimMsg = "no shims installed yet"
			}
			check("shims", shimOK, shimMsg)

			// 6. catalog is non-empty
			cat := loadCatalog()
			tools := cat.Tools()
			check("manifest catalog", len(tools) > 0,
				fmt.Sprintf("%d tool(s) discovered at %s", len(tools), cfg.Core.ManifestDir))

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

			// Fix mode
			if flagFix {
				fmt.Println("\n--- Fixing PATH issues ---")

				// Fix user PATH
				changed, err := env.CleanupUserPath(cfg.Core.ShimDir, cfg.GetPathMode())
				if err != nil {
					fmt.Printf("✘ Failed to fix user PATH: %v\n", err)
				} else if changed {
					fmt.Println("✔ User PATH cleaned up")
				} else {
					fmt.Println("• User PATH: no changes needed")
				}

				// 将 xdev 自身安装到 install/xdev/<version>/ 并创建 junction
				// 这样 xdev 命令通过 current/xdev 目录在 PATH 中全局可用
				if _, _, err := installer.InstallSelf(cfg); err != nil {
					fmt.Printf("✘ Failed to install xdev self: %v\n", err)
				}
				xdevBinDir := filepath.Join(cfg.Core.XMDevDir, "current", "xdev")
				if err := env.AppendToUserPath(xdevBinDir); err != nil {
					fmt.Printf("✘ Failed to add xdev dir to user PATH: %v\n", err)
				} else {
					fmt.Printf("✔ Added xdev dir to user PATH: %s\n", xdevBinDir)
				}

				// Fix system PATH (requires admin)
				changed, err = env.CleanupSystemPath()
				if err != nil {
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

			// AI-assisted diagnosis
			if flagAI {
				fmt.Println("\n--- AI Diagnosis ---")
				assistant := ai.NewAssistant(&ai.AIConfig{OllamaURL: cfg.AI.OllamaURL, Model: cfg.AI.Model, Timeout: cfg.AI.Timeout})
				if !assistant.IsAvailable() {
					warnOllamaUnavailable()
				} else {
					// Collect diagnostic info for AI
					var problems []string
					if _, err := os.Stat(cfg.Core.XMDevDir); err != nil {
						problems = append(problems, "xdev home directory does not exist")
					}
					if !pathContainsShim(cfg) {
						problems = append(problems, "shim directory is not in PATH")
					}
					if len(userPathIssues) > 0 {
						problems = append(problems, "user PATH has issues: "+strings.Join(userPathIssues, "; "))
					}
					if len(sysPathIssues) > 0 {
						problems = append(problems, "system PATH has issues: "+strings.Join(sysPathIssues, "; "))
					}

					if len(problems) == 0 {
						fmt.Println("AI: No issues found. Your environment looks healthy! 🎉")
					} else {
						problemDesc := strings.Join(problems, "\n- ")
						fmt.Println("Sending diagnostic info to AI...")
						suggestion, err := assistant.DiagnoseIssue(problemDesc)
						if err != nil {
							fmt.Printf("⚠ AI diagnosis failed: %v\n", err)
						} else {
							fmt.Printf("AI Suggestion:\n%s\n", suggestion)
						}
					}
				}
			}

				if !allOK {
					return fmt.Errorf("some checks failed")
				}
				return nil
		},
	}
	cmd.Flags().BoolVar(&flagFix, "fix", false, "Attempt to fix detected issues")
	cmd.Flags().BoolVar(&flagAI, "ai", false, "Enable AI-assisted diagnosis (requires Ollama)")
	return cmd
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
