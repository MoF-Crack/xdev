package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/spf13/cobra"

	"github.com/xmdemt/xdev/internal/installer"
	"github.com/xmdemt/xdev/internal/plugin"
	"github.com/xmdemt/xdev/internal/util"
)

func newUpgradeCmd() *cobra.Command {
	var flagAll, flagCheck, flagCleanup bool
	cmd := &cobra.Command{
		Use:   "upgrade [tool]",
		Short: "Upgrade installed tools to their latest versions",
		Long: `upgrade checks the manifest's Latest version for each tool
and, when a newer version is available, installs and activates it.

Use --all to upgrade every installed tool at once.
Use --check to only report what would be upgraded without making changes.
Use --cleanup to remove the previous (now-old) version after a successful upgrade.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, reg := loadCatalogAndRegistry()
			cfg := Cfg()
			ins := installer.New(cfg, reg)
			ins.SetNoEnvWrite(NoPathFix())

			if flagAll {
				return upgradeAll(cmd.Context(), ins, reg, flagCheck, flagCleanup)
			}
			if len(args) == 0 {
				return fmt.Errorf("specify a tool name or use --all")
			}
			return upgradeOne(cmd.Context(), ins, reg, args[0], flagCheck, flagCleanup)
		},
	}
	cmd.Flags().BoolVar(&flagAll, "all", false, "Upgrade all installed tools")
	cmd.Flags().BoolVar(&flagCheck, "check", false, "Only check for upgrades, don't execute")
	cmd.Flags().BoolVar(&flagCleanup, "cleanup", false, "Remove old versions after upgrade")
	return cmd
}

func upgradeOne(ctx context.Context, ins *installer.Installer, reg *plugin.Registry, toolName string, checkOnly, cleanup bool) error {
	p, ok := reg.Get(toolName)
	if !ok {
		return unsupportedToolErr(toolName)
	}

	latest, available := ins.IsUpgradeAvailable(toolName)
	if !available {
		fmt.Printf("✔ %s is already up to date\n", toolName)
		return nil
	}

	if checkOnly {
		fmt.Printf("  %s: upgrade available → %s\n", toolName, latest)
		return nil
	}

	fmt.Printf("⏳ Upgrading %s to %s...\n", toolName, latest)
	oldVer, _ := ins.CurrentVersion(toolName)

	res, err := ins.InstallWithProgress(ctx, p, latest, nil, installer.InstallOptions{})
	if err != nil {
		return fmt.Errorf("upgrade %s failed: %w", toolName, err)
	}
	fmt.Printf("✔ upgraded %s → %s\n", toolName, res.Version)

	if cleanup && oldVer != "" && oldVer != res.Version {
		if err := ins.Uninstall(p, oldVer); err != nil {
			fmt.Printf("⚠ failed to clean up old version %s@%s: %v\n", toolName, oldVer, err)
		} else {
			fmt.Printf("✔ cleaned up old version %s@%s\n", toolName, oldVer)
		}
	}
	return nil
}

func upgradeAll(ctx context.Context, ins *installer.Installer, reg *plugin.Registry, checkOnly, cleanup bool) error {
	tools := ins.ListInstalledTools()
	if len(tools) == 0 {
		fmt.Println("(no tools installed)")
		return nil
	}
	sort.Strings(tools)

	anyAvailable := false
	for _, toolName := range tools {
		latest, available := ins.IsUpgradeAvailable(toolName)
		if !available {
			fmt.Printf("✔ %s is already up to date\n", toolName)
			continue
		}
		if checkOnly {
			fmt.Printf("  %s: upgrade available → %s\n", toolName, latest)
			anyAvailable = true
			continue
		}
		p, ok := reg.Get(toolName)
		if !ok {
			fmt.Printf("⚠ %s: not in registry, skipping\n", toolName)
			continue
		}
		fmt.Printf("⏳ Upgrading %s to %s...\n", toolName, latest)
		oldVer, _ := ins.CurrentVersion(toolName)

		res, err := ins.InstallWithProgress(ctx, p, latest, nil, installer.InstallOptions{})
		if err != nil {
			fmt.Printf("⚠ upgrade %s failed: %v\n", toolName, err)
			continue
		}
		fmt.Printf("✔ upgraded %s → %s\n", toolName, res.Version)

		if cleanup && oldVer != "" && oldVer != res.Version {
			if err := ins.Uninstall(p, oldVer); err != nil {
				fmt.Printf("⚠ failed to clean up old version %s@%s: %v\n", toolName, oldVer, err)
			} else {
				fmt.Printf("✔ cleaned up old version %s@%s\n", toolName, oldVer)
			}
		}
	}

	if checkOnly && !anyAvailable {
		fmt.Println("All tools are up to date.")
	}
	return nil
}

// installedToolDirs returns subdirectory names under installDir that
// contain at least one version directory.  This is used as a fallback
// for scanning installed tools outside the installer package.
func installedToolDirs(installDir string) []string {
	entries, err := os.ReadDir(installDir)
	if err != nil {
		return nil
	}
	var tools []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		// 20261021 审计 P1-4：与 xdev list 共用同一版本判据，避免把
		// gopath / export_templates 之类辅助目录当成“已安装版本”。
		versions, err := util.ListVersionDirNames(filepath.Join(installDir, e.Name()))
		if err != nil || len(versions) == 0 {
			continue
		}
		tools = append(tools, e.Name())
	}
	return tools
}
