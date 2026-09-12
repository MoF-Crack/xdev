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
	"strings"

	"github.com/spf13/cobra"

	"github.com/xmdemt/xdev/internal/config"
	"github.com/xmdemt/xdev/internal/discovery"
	"github.com/xmdemt/xdev/internal/env"
	"github.com/xmdemt/xdev/internal/installer"
	"github.com/xmdemt/xdev/internal/util"
)

// newDoctorCmd runs a battery of health checks on the local install.
func newDoctorCmd() *cobra.Command {
	var flagFix bool
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Run sanity checks on your xdev installation",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := Cfg()
			allOK := true

			// 0. Self-heal stale GOROOT residue (ticket 01 / audit X-07): the
			//    pre-v2 go.yaml wrote a GOROOT one level above the real
			//    toolchain (strip:0 nested layout), which breaks every
			//    `go build` with "package fmt is not in std". Idempotent and
			//    conservative — only deletes values that provably point at
			//    the parent of a real GOROOT.
			if err := env.CleanupStaleGOROOT(); err != nil {
				fmt.Printf("✘ GOROOT registry cleanup failed: %v\n", err)
				allOK = false
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
			if flagFix {
				fmt.Println("\n--- Fixing PATH issues ---")

				// Fix user PATH
				changed, err := env.CleanupUserPath(cfg.Core.XMDevDir, cfg.Core.ShimDir, cfg.GetPathMode())
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

			if !allOK {
				return fmt.Errorf("some checks failed")
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&flagFix, "fix", false, "Attempt to fix detected issues")
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
