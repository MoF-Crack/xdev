// env.go defines the `xdev env` and `xdev shell` commands.
//
// # env
//
// Prints shell-specific env-var declarations for a tool version.
// Designed for eval: `eval "$(xdev env go@1.22.0 --shell bash)"`
// Supports --shell flag: "sh" (default), "powershell".
//
// # shell
//
// Drops into a sub-shell with xdev environment pre-exported.
// Without arguments, launches with all active tools' env vars.
// With tool specs, launches with only those tools' vars.
// Auto-detects the user's shell; override with --shell flag.
package cli

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/spf13/cobra"

	envpkg "github.com/xmdemt/xdev/internal/env"
	"github.com/xmdemt/xdev/internal/util"
)

// newEnvCmd prints the env vars a tool would export on activation.
//
// The output is `eval`-friendly: on Unix you can
//
//	eval "$(xdev env python@3.12.0)"
//
// on PowerShell you can
//
//	iex "$(xdev env python@3.12.0 | Out-String)"
func newEnvCmd() *cobra.Command {
	var flagShell string
	cmd := &cobra.Command{
		Use:   "env <tool>[@<version>]",
		Short: "Print env-var declarations for a tool version",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name, ver := splitToolVer(args[0])
			if ver == "" {
				return fmt.Errorf("env requires a version, e.g. %s@3.12.0", name)
			}
			_, reg := loadCatalogAndRegistry()
			p, ok := reg.Get(name)
			if !ok {
				return unsupportedToolErr(name)
			}
			installPath := p.InstallDir(Cfg().Core.InstallDir, ver)
			vars := p.EnvVars(installPath)
			// Security (C-1 fix): this output is designed to be eval'd, so it
			// must go through envpkg.HookOutput, which validates env-var names
			// and single-quotes values for POSIX shells / escapes them for
			// PowerShell. A manifest env_vars value containing ", $, backtick
			// or $(...) must never execute as shell code.
			shell := strings.ToLower(strings.TrimSpace(flagShell))
			switch shell {
			case "powershell", "pwsh":
				shell = "powershell"
			case "":
				shell = "sh"
			}
			fmt.Print(envpkg.HookOutput(shell, nil, vars))
			return nil
		},
	}
	cmd.Flags().StringVar(&flagShell, "shell", "sh", "output format (sh | powershell)")
	return cmd
}

func newShellCmd() *cobra.Command {
	var flagShell string
	cmd := &cobra.Command{
		Use:   "shell [tool@version ...]",
		Short: "Drop into a sub-shell with xdev env pre-exported",
		Long: `Launch a new shell with the specified tools' environment
variables pre-exported. Without arguments, launches a shell with
all currently active tools' env vars set.

Examples:
  xdev shell              # shell with all active tools
  xdev shell python@3.12  # shell with only python@3.12
  xdev shell java@21 go@1.22  # shell with multiple tools`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Determine which tools to activate
			var envVars map[string]string
			if len(args) == 0 {
				// All currently active tools
				envVars = collectActiveEnvVars()
			} else {
				// Only specified tools
				envVars = make(map[string]string)
				_, reg := loadCatalogAndRegistry()
				for _, arg := range args {
					name, ver := splitToolVer(arg)
					p, ok := reg.Get(name)
					if !ok {
						return unsupportedToolErr(name)
					}
					if ver == "" {
						ver = p.DefaultVersion()
					}
					installPath := p.InstallDir(Cfg().Core.InstallDir, ver)
					for k, v := range p.EnvVars(installPath) {
						envVars[k] = v
					}
				}
			}

			// Detect the user's shell
			shellType := flagShell
			if shellType == "" {
				info := envpkg.DetectShell()
				shellType = info.Shell
			}

			// Find the shell binary
			shellBin, err := findShellBinary(shellType)
			if err != nil {
				return fmt.Errorf("cannot find shell %q: %w", shellType, err)
			}

			// Build the new environment
			newEnv := os.Environ()
			for k, v := range envVars {
				newEnv = appendEnv(newEnv, k, v)
			}
			// Ensure shim dir is in PATH
			newEnv = appendEnv(newEnv, "PATH", Cfg().Core.ShimDir+string(os.PathListSeparator)+os.Getenv("PATH"))

			// Launch the sub-shell
			subCmd := exec.Command(shellBin)
			subCmd.Env = newEnv
			subCmd.Stdin = os.Stdin
			subCmd.Stdout = os.Stdout
			subCmd.Stderr = os.Stderr
			return subCmd.Run()
		},
	}
	cmd.Flags().StringVar(&flagShell, "shell", "", "shell to use (sh, bash, zsh, fish, pwsh, powershell, cmd)")
	return cmd
}

// collectActiveEnvVars gathers env vars from all currently active tools.
func collectActiveEnvVars() map[string]string {
	_, reg := loadCatalogAndRegistry()
	result := make(map[string]string)
	for _, t := range reg.All() {
		activeVer, ok := currentVersion(t.Name())
		if !ok {
			continue
		}
		installPath := t.InstallDir(Cfg().Core.InstallDir, activeVer)
		for k, v := range t.EnvVars(installPath) {
			result[k] = v
		}
	}
	return result
}

// currentVersion reads the current/<tool> symlink to determine the
// active version. Returns ("", false) when no active version exists.
func currentVersion(toolName string) (string, bool) {
	return activeVersion(toolName)
}

// activeVersion reads the current/<tool> symlink to determine the
// active version. Returns ("", false) when no active version exists.
func activeVersion(toolName string) (string, bool) {
	return util.ActiveVersion(Cfg().Core.XMDevDir, toolName)
}

// activeInstallPath returns the install path of the currently active
// version of a tool, or "" if no active version exists.
func activeInstallPath(toolName string) string {
	return util.ActiveInstallPath(Cfg().Core.XMDevDir, toolName)
}

// findShellBinary locates the shell executable on the system.
func findShellBinary(shellType string) (string, error) {
	switch shellType {
	case "bash", "zsh", "sh", "fish":
		return exec.LookPath(shellType)
	case "pwsh":
		return exec.LookPath("pwsh")
	case "powershell":
		return exec.LookPath("powershell")
	case "cmd":
		return exec.LookPath("cmd")
	default:
		// Try to find it directly
		p, err := exec.LookPath(shellType)
		if err == nil {
			return p, nil
		}
		// Fallback: on Unix use $SHELL, on Windows use cmd
		if runtime.GOOS != "windows" {
			if sh := os.Getenv("SHELL"); sh != "" {
				return sh, nil
			}
			return "/bin/sh", nil
		}
		return "", fmt.Errorf("unknown shell %q", shellType)
	}
}

// appendEnv adds or replaces an env var in the environment list.
func appendEnv(env []string, key, value string) []string {
	prefix := key + "="
	for i, e := range env {
		if strings.HasPrefix(e, prefix) {
			env[i] = prefix + value
			return env
		}
	}
	return append(env, prefix+value)
}
