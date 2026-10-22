// isolate.go defines the `xdev isolate` command for environment isolation.
//
// Supports three isolation modes:
//   - none: no isolation (default)
//   - container: Docker-based isolation
//   - nix: Nix-style isolation
//
// Subcommands: setup, teardown, exec.
// The exec subcommand runs a command in the isolated environment and
// propagates the child process exit code via shim.ExitCodeError
// (rather than calling os.Exit directly, which would skip deferred cleanup).
package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/xmdemt/xdev/internal/isolator"
	"github.com/xmdemt/xdev/internal/shim"
)

var isolateMode string

// newIsolateCmd handles `xdev isolate setup|teardown|exec`.
func newIsolateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "isolate",
		Short: "Manage environment isolation",
		Long: `isolate creates an isolated development environment using
one of three modes:

  none       - No isolation (default), modify current process env
  container  - Docker container isolation via devcontainer.json
  nix        - Nix flake-based isolation

Examples:
  xdev isolate --mode container setup
  xdev isolate exec -- go test ./...
  xdev isolate teardown`,
	}

	cmd.PersistentFlags().StringVar(&isolateMode, "mode", "none", "isolation mode (none | container | nix)")

	cmd.AddCommand(
		newIsolateSetupCmd(),
		newIsolateTeardownCmd(),
		newIsolateExecCmd(),
	)

	return cmd
}

func newIsolateSetupCmd() *cobra.Command {
	// Ticket 35 (C-07): --force is the explicit opt-in for overwriting files
	// the user already owns (flake.nix / devcontainer.json). The default is
	// "refuse" so a setup never clobbers hand-written config.
	var force bool
	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Create an isolated environment",
		RunE: func(cmd *cobra.Command, args []string) error {
			iso, err := isolator.NewIsolator(isolator.IsolationMode(isolateMode), Cfg().Core.XMDevDir)
			if err != nil {
				return err
			}
			spec := buildEnvSpec()
			spec.Force = force
			if err := iso.Setup(cmd.Context(), spec); err != nil {
				return err
			}
			fmt.Printf("✔ isolated environment created (mode: %s)\n", iso.Mode())
			return nil
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "overwrite existing flake.nix / devcontainer.json (ticket 35 / C-07)")
	return cmd
}

func newIsolateTeardownCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "teardown",
		Short: "Destroy the isolated environment",
		RunE: func(cmd *cobra.Command, args []string) error {
			iso, err := isolator.NewIsolator(isolator.IsolationMode(isolateMode), Cfg().Core.XMDevDir)
			if err != nil {
				return err
			}
			if err := iso.Teardown(cmd.Context()); err != nil {
				return err
			}
			fmt.Printf("✔ isolated environment destroyed (mode: %s)\n", iso.Mode())
			return nil
		},
	}
}

func newIsolateExecCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "exec -- <command> [args...]",
		Short: "Execute a command in the isolated environment",
		Long: `Execute a command inside the current isolation environment.
Use -- to separate xdev flags from the command to run.

Examples:
  xdev isolate exec -- go test ./...
  xdev isolate --mode container exec -- python main.py
  xdev isolate --mode nix exec -- cargo build`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return fmt.Errorf("specify a command to execute (use -- to separate flags)")
			}
			iso, err := isolator.NewIsolator(isolator.IsolationMode(isolateMode), Cfg().Core.XMDevDir)
			if err != nil {
				return err
			}
			result, err := iso.Exec(cmd.Context(), args)
			if err != nil {
				return err
			}
			forwardExecResult(result)
			if result.ExitCode != 0 {
				return &shim.ExitCodeError{Code: result.ExitCode}
			}
			return nil
		},
	}
}

// buildEnvSpec constructs an EnvironmentSpec from the current xdev config.
func buildEnvSpec() *isolator.EnvironmentSpec {
	cfg := Cfg()
	dir, _ := os.Getwd()
	spec := &isolator.EnvironmentSpec{
		Tools:      make(map[string]string),
		Env:        make(map[string]string),
		WorkingDir: dir,
	}
	// Collect active tool versions
	_, reg := loadCatalogAndRegistry()
	for _, t := range reg.All() {
		ver, ok := currentVersion(t.Name())
		if ok {
			spec.Tools[t.Name()] = ver
		}
	}
	// Add shim dir to PATH
	spec.Env["PATH"] = cfg.Core.ShimDir + string(os.PathListSeparator) + os.Getenv("PATH")
	return spec
}
