// hook.go defines the `xdev hook`, `xdev activate`, and `xdev deactivate`
// commands for shell integration.
//
// # hook
//
// Outputs shell-specific export statements for the tools specified in the
// nearest .xdev.toml. Designed to be eval'd from a cd hook:
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
	"github.com/xmdemt/xdev/internal/plugin"
	"github.com/xmdemt/xdev/internal/project"
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
configures PATH and environment variables for the tools specified
in the nearest .xdev.toml file.

This is designed to be eval'd from a shell hook:
  eval "$(xdev hook --shell bash)"`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if flagShell == "" {
				flagShell = "bash"
			}
			cfg := Cfg()
			dir, proj := project.FindConfig(".")
			if proj == nil || len(proj.Tools) == 0 {
				// No project config — output nothing (keep current env)
				return nil
			}

			_, reg := loadCatalogAndRegistry()
			binPaths, envVars := resolveProjectEnv(cfg, reg, dir, proj)
			output := env.HookOutput(flagShell, binPaths, envVars)
			if output != "" {
				fmt.Print(output)
			}
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
			info := env.DetectShell()
			if len(args) == 1 {
				info.Shell = args[0]
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

// resolveProjectEnv resolves the bin paths and env vars for a project config.
func resolveProjectEnv(cfg *config.Config, reg *plugin.Registry, projectDir string, proj *project.ProjectConfig) ([]string, map[string]string) {
	var binPaths []string
	envVars := map[string]string{}

	for toolName, versionSpec := range proj.Tools {
		p, ok := reg.Get(toolName)
		if !ok {
			continue
		}
		// Try to resolve the version spec
		ver, err := p.ResolveVersion(versionSpec)
		installPath := ""
		if err == nil {
			// Check if this resolved version is installed
			ip := p.InstallDir(cfg.Core.InstallDir, ver)
			if fi, err2 := os.Stat(ip); err2 == nil && fi.IsDir() {
				installPath = ip
			}
		}
		// Fallback: try current version (the active junction)
		if installPath == "" {
			installPath = currentInstallPath(cfg, toolName)
		}
		if installPath == "" {
			continue
		}
		// Collect bin paths
		paths := env.ToolBinPaths(toolName, installPath)
		binPaths = append(binPaths, paths...)
		// Collect env vars
		for k, v := range p.EnvVars(installPath) {
			envVars[k] = v
		}
	}

	// Add project-level env vars
	for k, v := range proj.Env {
		envVars[k] = v
	}

	return binPaths, envVars
}

// currentInstallPath returns the install path of the currently active version.
func currentInstallPath(cfg *config.Config, toolName string) string {
	return activeInstallPath(toolName)
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
