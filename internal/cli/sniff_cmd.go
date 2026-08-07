package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/xmdemt/xdev/internal/config"
	"github.com/xmdemt/xdev/internal/discovery"
	"github.com/xmdemt/xdev/internal/util"
)

// ---------------------------------------------------------------------------
// newSniffCmd — xdev sniff [--all] [--tool <name>] [--headless]
// ---------------------------------------------------------------------------

func newSniffCmd() *cobra.Command {
	var flagAll bool
	var flagTool string
	var flagHeadless bool

	cmd := &cobra.Command{
		Use:   "sniff",
		Short: "Discover new tool versions from remote sources",
		Long: `sniff runs the automatic tool-version discovery pipeline.  It uses
four built-in sniffers (GitHub, JSON, HTML, ArchivePattern) to query
remote sources and writes discovered versions into the local manifest
cache and the sniff result cache.

Trigger strategies (configured in config.yaml under sniff:):
  interval   Check at startup if the sniff interval has elapsed (default).
  startup    Sniff asynchronously on every startup (non-blocking).
  service    Install a system scheduler task to sniff periodically.
  manual     Only sniff when explicitly invoked with 'xdev sniff'.

Examples:
  xdev sniff                  Sniff all AutoSniff-enabled tools
  xdev sniff --all            Same as above (explicit)
  xdev sniff --tool python    Sniff only python
  xdev sniff --headless       No terminal output (for system service calls)
  xdev sniff service install  Install system scheduler task (Win/Linux/macOS)
  xdev sniff service status   Show installed service status
  xdev sniff service uninstall  Remove system scheduler task`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			headless := flagHeadless

			// Suppress all log output in headless mode (FR-012).
			if headless {
				log.SetOutput(io.Discard)
			}

			cfg := Cfg()
			s := discovery.NewScheduler(&cfg.Sniff, cfg.Core.CacheDir, cfg.Core.ManifestDir)

			// Show last failures from cache before sniffing (FR-016).
			// Run for both --tool and --all paths.
			if !headless {
				showLastSniffFailures(cfg)
			}

			if flagTool != "" {
				// Single tool sniff.
				if !headless {
					fmt.Printf("🔍 sniffing %s...\n", flagTool)
				}
				if err := s.SniffNow(cmd.Context(), flagTool); err != nil {
					return fmt.Errorf("sniff %s: %w", flagTool, err)
				}
				if !headless {
					fmt.Printf("✔ sniff complete for %s\n", flagTool)
				}
				return nil
			}

			// All tools (default when no --tool is specified).
			if !headless {
				fmt.Println("🔍 sniffing all tools...")
			}
			if err := s.SniffAll(cmd.Context()); err != nil {
				if !headless {
					fmt.Fprintf(os.Stderr, "⚠ sniff completed with errors: %v\n", err)
				}
				return err
			}
			if !headless {
				fmt.Println("✔ sniff complete")
				showSniffSummary(cfg)
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&flagAll, "all", false, "Sniff all tools (default behaviour)")
	cmd.Flags().StringVar(&flagTool, "tool", "", "Sniff a specific tool by name")
	cmd.Flags().BoolVar(&flagHeadless, "headless", false, "Suppress terminal output (for system service calls)")

	// Service subcommands.
	cmd.AddCommand(newSniffServiceCmd())

	return cmd
}

// ---------------------------------------------------------------------------
// newSniffServiceCmd — xdev sniff service install/uninstall/status
// ---------------------------------------------------------------------------

func newSniffServiceCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "service",
		Short: "Manage the system scheduler task for automatic sniffing",
		Long: `Manages a platform-specific system scheduler task that calls
'xdev sniff --all --headless' periodically.

Platform support:
  Windows  Task Scheduler (schtasks /create)
  Linux    User crontab (crontab -e)
  macOS    LaunchAgent (~/Library/LaunchAgents/com.xdev.sniff.plist)`,
	}

	cmd.AddCommand(
		newSniffServiceInstallCmd(),
		newSniffServiceUninstallCmd(),
		newSniffServiceStatusCmd(),
	)

	return cmd
}

func newSniffServiceInstallCmd() *cobra.Command {
	var flagSchedule string
	cmd := &cobra.Command{
		Use:   "install",
		Short: "Install system scheduler task for periodic sniffing",
		Long: `Installs a system scheduler task that runs 'xdev sniff --all --headless'
on a schedule.  Default is daily at 03:00.

Schedule format (platform-dependent):
  Windows: HH:MM (e.g. "03:00")
  Linux:   cron expression, e.g. "0 3 * * *"
  macOS:   HH:MM (e.g. "03:00")`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			si, err := discovery.NewServiceInstaller()
			if err != nil {
				return fmt.Errorf("service installer: %w", err)
			}
			if err := si.Install(flagSchedule); err != nil {
				return fmt.Errorf("install service: %w", err)
			}
			fmt.Println("✔ sniff service installed")
			return nil
		},
	}
	cmd.Flags().StringVar(&flagSchedule, "schedule", "", "Schedule for sniffing (default: daily 03:00)")
	return cmd
}

func newSniffServiceUninstallCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "uninstall",
		Short: "Remove the system scheduler task for sniffing",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			si, err := discovery.NewServiceInstaller()
			if err != nil {
				return fmt.Errorf("service installer: %w", err)
			}
			if err := si.Uninstall(); err != nil {
				return fmt.Errorf("uninstall service: %w", err)
			}
			fmt.Println("✔ sniff service uninstalled")
			return nil
		},
	}
}

func newSniffServiceStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show the status of the sniff system scheduler task",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			si, err := discovery.NewServiceInstaller()
			if err != nil {
				return fmt.Errorf("service installer: %w", err)
			}
			status, err := si.Status()
			if err != nil {
				return fmt.Errorf("service status: %w", err)
			}
			if status == nil {
				fmt.Println("sniff service: not installed")
				return nil
			}
			fmt.Printf("sniff service:\n")
			fmt.Printf("  installed: %v\n", status.Installed)
			if status.Schedule != "" {
				fmt.Printf("  schedule:  %s\n", status.Schedule)
			}
			if !status.LastRun.IsZero() {
				fmt.Printf("  last run:  %s\n", status.LastRun.Format(time.RFC3339))
			}
			if !status.NextRun.IsZero() {
				fmt.Printf("  next run:  %s\n", status.NextRun.Format(time.RFC3339))
			}
			return nil
		},
	}
}

// ---------------------------------------------------------------------------
// newUpdatesCmd — xdev updates
// ---------------------------------------------------------------------------

func newUpdatesCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "updates",
		Short: "List tools with available version upgrades from last sniff",
		Long: `Reads the sniff cache to show which installed tools have newer versions
available.  Run 'xdev sniff --all' first to refresh the cache.

Output format:
  python  3.11.10 → 3.13.0  (xdev install python@3.13.0)`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return showUpdates(cmd)
		},
	}
}

// showUpdates reads the sniff cache and displays tools with upgrades.
func showUpdates(cmd *cobra.Command) error {
	cfg := Cfg()
	cachePath := filepath.Join(cfg.Core.CacheDir, "sniff-cache.json")

	data, err := os.ReadFile(cachePath)
	if err != nil {
		if os.IsNotExist(err) {
			// Check if running with headless flag (from sniff_cmd context).
			fmt.Println("No sniff data yet. Run 'xdev sniff --all' to discover new versions.")
			return nil
		}
		return fmt.Errorf("read sniff cache: %w", err)
	}

	var cache discovery.SniffCache
	if err := json.Unmarshal(data, &cache); err != nil {
		return fmt.Errorf("parse sniff cache: %w", err)
	}

	// Load the manifest catalog to know which tools exist and their installed versions.
	cat := loadCatalog()
	_ = cat // retained for future use (tool manifest comparison)

	installDir := cfg.Core.InstallDir
	type upgrade struct {
		Tool       string
		Installed  string
		Available  string
	}
	var upgrades []upgrade

	for toolName, result := range cache.Tools {
		if result.Unsupported || result.Error != "" || len(result.Versions) == 0 {
			continue
		}

		// Check if the tool is installed.
		toolDir := filepath.Join(installDir, toolName)
		if _, err := os.Stat(toolDir); os.IsNotExist(err) {
			continue
		}

		// Find the active (installed) version.
		active, ok := util.ActiveVersion(cfg.Core.XMDevDir, toolName)
		if !ok {
			// Try to find any installed version.
			entries, err := os.ReadDir(toolDir)
			if err != nil || len(entries) == 0 {
				continue
			}
			// Sort by name descending to get latest.
			sort.Slice(entries, func(i, j int) bool {
				return entries[i].Name() > entries[j].Name()
			})
			active = entries[0].Name()
		}

		// Sort available versions (highest first).
		avail := result.Versions
		sort.Slice(avail, func(i, j int) bool {
			return util.CompareSemver(avail[i], avail[j]) > 0
		})

		if len(avail) > 0 {
			latest := avail[0]
			if util.CompareSemver(latest, active) > 0 {
				upgrades = append(upgrades, upgrade{
					Tool:      toolName,
					Installed: active,
					Available: latest,
				})
			}
		}
	}

	if len(upgrades) == 0 {
		fmt.Println("All tools up to date.")
		return nil
	}

	sort.Slice(upgrades, func(i, j int) bool {
		return upgrades[i].Tool < upgrades[j].Tool
	})

	fmt.Println("Available upgrades:")
	for _, u := range upgrades {
		fmt.Printf("  %-16s %s → %s  (xdev install %s@%s)\n",
			u.Tool, u.Installed, u.Available, u.Tool, u.Available)
	}
	return nil
}

// showSniffSummary prints a brief summary after a successful sniff.
func showSniffSummary(cfg *config.Config) {
	cachePath := filepath.Join(cfg.Core.CacheDir, "sniff-cache.json")
	data, err := os.ReadFile(cachePath)
	if err != nil {
		return
	}
	var cache discovery.SniffCache
	if err := json.Unmarshal(data, &cache); err != nil {
		return
	}

	newVersions := 0
	unsupported := 0
	errors := 0
	for _, r := range cache.Tools {
		if r.Unsupported {
			unsupported++
		} else if r.Error != "" {
			errors++
		} else if len(r.Versions) > 0 {
			newVersions++
		}
	}

	var parts []string
	if newVersions > 0 {
		parts = append(parts, fmt.Sprintf("%d tool(s) with new versions", newVersions))
	}
	if unsupported > 0 {
		parts = append(parts, fmt.Sprintf("%d unsupported", unsupported))
	}
	if errors > 0 {
		parts = append(parts, fmt.Sprintf("%d error(s)", errors))
	}
	if cache.Partial {
		parts = append(parts, "partial")
	}
	if len(parts) > 0 {
		fmt.Printf("  summary: %s\n", strings.Join(parts, ", "))
	}
}

// showLastSniffFailures reads the sniff cache and prints tools that had
// errors during the last sniff.  This gives the user visibility into
// persistent failures (FR-016).
func showLastSniffFailures(cfg *config.Config) {
	cachePath := filepath.Join(cfg.Core.CacheDir, "sniff-cache.json")
	data, err := os.ReadFile(cachePath)
	if err != nil {
		return
	}
	var cache discovery.SniffCache
	if err := json.Unmarshal(data, &cache); err != nil {
		return
	}

	var failures []string
	for toolName, r := range cache.Tools {
		if r.Error != "" {
			failures = append(failures, fmt.Sprintf("    %s: %s", toolName, r.Error))
		}
	}
	if len(failures) > 0 {
		fmt.Println("⚠ Last sniff had errors:")
		for _, f := range failures {
			fmt.Println(f)
		}
		fmt.Println()
	}
}
