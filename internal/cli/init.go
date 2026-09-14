// init.go defines the `xdev init` command — the first-time setup wizard.
//
// The init command performs the following steps:
//  1. Choose XDev Home directory (interactive drive selection on Windows)
//  2. Create directory structure (install/, shims/, cache/, downloads/, etc.)
//  3. Write config.yaml with defaults
//  4. Self-install xdev into current/xdev/ (gated by NoPathFix())
//  5. Interactive tool selection and installation
//  6. Shell RC injection (cd hook) for automatic version switching
//
// # Windows Drive Selection
//
// On Windows, xdev scans D:-J: drives and recommends the one with the
// most free space (≥20 GiB threshold). The C: drive is never recommended
// because language runtimes and Android SDKs can be very large. The user
// can override with --xmdevenv-dir or --yes (non-interactive mode).
//
// # --no-path-fix
//
// When set, Step 4 (self-install to PATH) and Step 6 (shell RC injection)
// are skipped. This is used by integration tests to avoid polluting the
// host environment.
//
// 票 31（P1-23 / E-04）：本命令不再定义同名局部 flag——局部定义会遮蔽
// root.go 的持久 flag，导致 `xdev init --no-path-fix` 只让「自装门」生效、
// 而向导内的安装/注册表阶段读到 false（NoPathFix()==false），出现
// 「部分生效」。现在三处门（SetNoEnvWrite / shell RC 注入 / Windows PATH）
// 全部读同一个持久 flag 值（经 initContext 写入 globalCtx）。
package cli

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/spf13/cobra"

	"github.com/xmdemt/xdev/internal/config"
	"github.com/xmdemt/xdev/internal/env"
	"github.com/xmdemt/xdev/internal/installer"
	"github.com/xmdemt/xdev/internal/plugin"
)

func newInitCmd() *cobra.Command {
	var (
		flagDownloadDir string
		flagInstallDir  string
		flagForce       bool
		flagYes         bool
	)
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize xdev with an interactive setup wizard",
		Long: `init creates the xdev home directory, writes a default
config.yaml, and prepares the directory layout.  It then launches an
interactive wizard that lets you pick languages to install and offers
to inject shims into your shell RC / Windows PATH.

By default the home directory is NEVER placed on C:.  On Windows
xdev scans every mounted drive and asks you in the terminal
to pick which one to use (with the largest non-system drive as
recommendation).  Pass --yes to skip the prompt and use the
recommendation.

Override with --xmdevenv-dir, the XMDEV_HOME environment variable,
or XMDEV_FORCE_HOME=1 to fall back to ~/.xdev.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runInitWizard(cmd, flagDownloadDir, flagInstallDir, flagForce, flagYes)
		},
	}
	cmd.Flags().StringVar(&flagDownloadDir, "download-dir", "",
		"override the default download directory (can also be set in config.yaml)")
	cmd.Flags().StringVar(&flagInstallDir, "install-dir", "",
		"override the default install directory (can also be set in config.yaml)")
	cmd.Flags().BoolVar(&flagForce, "force", false, "overwrite an existing config without prompting")
	cmd.Flags().BoolVar(&flagYes, "yes", false,
		"non-interactive: use the recommended drive (largest non-system drive) without prompting")
	return cmd
}

func runInitWizard(cmd *cobra.Command, flagDownloadDir, flagInstallDir string, flagForce, flagYes bool) error {
	reader := bufio.NewReader(os.Stdin)

	fmt.Println("Welcome to xdev! Let's set up your development environment.")
	fmt.Println()

	// Step 1: Initialize config (existing logic)
	config.InvalidateHomeCache()

	cfg := Cfg()

	if !alreadyChosenHome() && !flagYes && runtime.GOOS == "windows" {
		choice, err := promptDrive(os.Stdin, os.Stdout)
		if err != nil {
			return err
		}
		if choice.Quit {
			fmt.Println("init aborted.")
			return nil
		}
		if choice.Path != "" {
			cfg.Core.XMDevDir = choice.Path
			cfg.Core.DownloadDir = ""
			cfg.Core.InstallDir = ""
			cfg.Core.ShimDir = ""
			cfg.Core.CacheDir = ""
			cfg.Core.EnvDir = ""
			cfg.Core.ManifestDir = ""
			cfg.ResolvePaths(cfg.Core.XMDevDir)
		}
	}

	if flagDownloadDir != "" {
		cfg.Core.DownloadDir = flagDownloadDir
	}
	if flagInstallDir != "" {
		cfg.Core.InstallDir = flagInstallDir
	}
	if err := cfg.EnsureDirs(); err != nil {
		return fmt.Errorf("create directories: %w", err)
	}
	if err := cfg.Save(); err != nil {
		return err
	}
	fmt.Printf("✔ xdev home:       %s\n", cfg.Core.XMDevDir)
	fmt.Printf("✔ download dir:    %s\n", cfg.Core.DownloadDir)
	fmt.Printf("✔ install dir:     %s\n", cfg.Core.InstallDir)
	fmt.Printf("✔ shim dir:        %s\n", cfg.Core.ShimDir)
	fmt.Printf("✔ config written:  %s\n", filepath.Join(cfg.Core.XMDevDir, "config.yaml"))

	if runtime.GOOS == "windows" {
		if drive := filepath.VolumeName(cfg.Core.XMDevDir); drive != "" {
			if free, ok := config.DriveFreeBytes(drive); ok {
				fmt.Printf("• drive %s has %s free\n", drive, humanBytes(free))
				if len(drive) >= 1 && drive[0] == 'C' {
					fmt.Printf("⚠ xdev is on your system drive %s\n", drive)
					fmt.Printf("  - Language runtimes + Android SDK can grow to 10+ GiB\n")
					fmt.Printf("  - System updates and Windows Temp files compete for space\n")
					fmt.Printf("  - Recommended: move it to a data drive, e.g.\n")
					fmt.Printf("      xdev init --xmdevenv-dir D:\\XDev\n")
				} else if free < config.MinFreeBytesDefault {
					fmt.Printf("⚠ %s is below the recommended %s free - consider\n",
						drive, humanBytes(config.MinFreeBytesDefault))
					fmt.Printf("  cleaning up or picking a different drive.\n")
				}
			}
		}
	}

	// Step 2: Tool selection
	type toolSel struct {
		name    string
		version string
	}
	var selected []toolSel

	tools := []struct {
		displayName string
		toolName    string
	}{
		{"Python", "python"},
		{"Node.js", "node"},
		{"Go", "go"},
		{"Rust", "rust"},
		{"Java", "java"},
	}

	_, reg := loadCatalogAndRegistry()
	ins := installer.New(cfg, reg)
	ins.SetNoEnvWrite(NoPathFix())

	for {
		fmt.Println("\nAvailable languages:")
		for i, t := range tools {
			fmt.Printf("  %d. %s\n", i+1, t.displayName)
		}
		fmt.Print("\nSelect a language to install (number, or 'skip'): ")
		input, _ := reader.ReadString('\n')
		input = strings.TrimSpace(input)
		if input == "skip" || input == "" {
			break
		}
		var choice int
		if _, err := fmt.Sscanf(input, "%d", &choice); err != nil || choice < 1 || choice > len(tools) {
			fmt.Println("⚠ invalid selection")
			continue
		}
		tool := tools[choice-1]

		p, ok := reg.Get(tool.toolName)
		if !ok {
			fmt.Printf("⚠ %s not found in catalog, skipping\n", tool.displayName)
			continue
		}

		// Show available versions
		version := pickVersion(reader, p, tool.displayName)
		if version == "" {
			fmt.Println("  skipped.")
			continue
		}

		fmt.Printf("Installing %s (%s)...\n", tool.displayName, version)
		_, err := ins.InstallWithProgress(cmd.Context(), p, version, nil, installer.InstallOptions{})
		if err != nil {
			fmt.Printf("⚠ failed to install %s: %v\n", tool.displayName, err)
		} else {
			fmt.Printf("✔ %s installed\n", tool.displayName)
			selected = append(selected, toolSel{tool.toolName, version})
		}

		fmt.Print("\nAdd more tools? (y/N): ")
		more, _ := reader.ReadString('\n')
		if !strings.EqualFold(strings.TrimSpace(more), "y") {
			break
		}
	}

	// Step 3: Shell RC injection
	// 票 31（E-04）：与 :169/:254 同源——读 root 持久 flag（NoPathFix()），
	// 不再读会被遮蔽的局部 flag。
	if !NoPathFix() {
		fmt.Print("\nInject into shell RC? (Y/n): ")
		inject, _ := reader.ReadString('\n')
		if !strings.EqualFold(strings.TrimSpace(inject), "n") {
			info := env.DetectShell()
			if info.RCFile != "" {
				if err := env.InjectShellRC(info, cfg.Core.XMDevDir, cfg.Core.ShimDir); err != nil {
					fmt.Printf("⚠ failed to inject shell RC: %v\n", err)
				} else {
					fmt.Printf("✔ injected into %s\n", info.RCFile)
				}
			} else {
				fmt.Println("⚠ no shell RC file detected; PATH mode will be used instead")
			}
		}

		// On Windows, also persist the shim dir in the user PATH
		// via the registry so that new terminal sessions can find
		// the shims even before the profile script runs.
		if runtime.GOOS == "windows" && cfg.Path.InjectToWindowsPath {
			if err := env.SetWindowsUserPath(cfg.Core.ShimDir); err != nil {
				fmt.Printf("⚠ failed to add shim dir to Windows user PATH: %v\n", err)
			} else {
				fmt.Printf("✔ added shim dir to Windows user PATH (registry)\n")
			}
		}
	}

	// Step 4: Install self
	if installDir, isNew, err := installer.InstallSelf(cfg); err != nil {
		fmt.Printf("⚠ failed to install xdev self: %v\n", err)
	} else {
		if isNew {
			fmt.Printf("✔ installed xdev to %s\n", installDir)
		}
		if runtime.GOOS == "windows" && !NoPathFix() {
			xdevBinDir := filepath.Join(cfg.Core.XMDevDir, "current", "xdev")
			if err := env.AppendToUserPath(xdevBinDir); err != nil {
				fmt.Printf("⚠ failed to add xdev dir to Windows user PATH: %v\n", err)
			} else {
				fmt.Printf("✔ added xdev dir to Windows user PATH: %s\n", xdevBinDir)
			}
		}
	}

	// Done
	fmt.Println("\n✔ xdev setup complete!")
	if len(selected) > 0 {
		fmt.Println("Installed tools:")
		for _, s := range selected {
			fmt.Printf("  - %s@%s\n", s.name, s.version)
		}
	}
	fmt.Println("\nNext steps:")
	fmt.Println("  xdev doctor     # verify your setup")
	fmt.Println("  xdev available  # list all available tools")
	fmt.Println("  xdev install <tool>@<version>  # install more tools")
	return nil
}

// pickVersion presents the user with available versions from the
// plugin's manifest and returns their choice.  An empty string means
// "skip this tool".
func pickVersion(reader *bufio.Reader, p plugin.Plugin, displayName string) string {
	m := p.Manifest()
	defaultVer := p.DefaultVersion()
	if defaultVer == "" {
		defaultVer = "latest"
	}

	var versions []string
	if m != nil {
		for _, v := range m.VersionList {
			versions = append(versions, v.Version)
		}
	}

	fmt.Printf("\nAvailable versions for %s:\n", displayName)
	if len(versions) == 0 {
		fmt.Printf("  (no versions listed in manifest; will use %q)\n", defaultVer)
		fmt.Printf("  Press Enter to use %s, or type a version: ", defaultVer)
		input, _ := reader.ReadString('\n')
		input = strings.TrimSpace(input)
		if input == "" {
			return defaultVer
		}
		return input
	}

	maxShow := 10
	for i, v := range versions {
		if i >= maxShow {
			fmt.Printf("  ... and %d more (type the version manually)\n", len(versions)-maxShow)
			break
		}
		marker := ""
		if v == defaultVer {
			marker = " (default)"
		}
		fmt.Printf("  %d. %s%s\n", i+1, v, marker)
	}

	fmt.Printf("  0. latest (resolve to newest)\n")
	fmt.Printf("\nSelect version (number, or press Enter for %s): ", defaultVer)
	input, _ := reader.ReadString('\n')
	input = strings.TrimSpace(input)
	if input == "" {
		return defaultVer
	}
	var choice int
	if _, err := fmt.Sscanf(input, "%d", &choice); err == nil {
		if choice == 0 {
			return "latest"
		}
		if choice >= 1 && choice <= len(versions) {
			return versions[choice-1]
		}
	}
	return input
}

// alreadyChosenHome returns true when the user has already made a
// home-directory decision via flag or environment, in which case we
// must NOT re-prompt.
func alreadyChosenHome() bool {
	if XMDevDirOverride() != "" {
		return true
	}
	if os.Getenv("XMDEV_HOME") != "" {
		return true
	}
	if ForceHome() {
		return true
	}
	return false
}

// humanBytes formats a byte count in IEC binary units.  Local copy
// of the helper that lives in internal/downloader so the cli package
// has no dependencies on it.
func humanBytes(n int64) string {
	const (
		KiB = 1 << 10
		MiB = 1 << 20
		GiB = 1 << 30
		TiB = 1 << 40
	)
	switch {
	case n >= TiB:
		return fmt.Sprintf("%.2f TiB", float64(n)/TiB)
	case n >= GiB:
		return fmt.Sprintf("%.2f GiB", float64(n)/GiB)
	case n >= MiB:
		return fmt.Sprintf("%.2f MiB", float64(n)/MiB)
	case n >= KiB:
		return fmt.Sprintf("%.2f KiB", float64(n)/KiB)
	}
	return fmt.Sprintf("%d B", n)
}
