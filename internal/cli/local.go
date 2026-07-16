package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/xmdemt/xdev/internal/project"
)

// newLocalCmd handles `xdev local [tool@version]`.
// It sets or displays project-level tool versions in .xdev.toml.
func newLocalCmd() *cobra.Command {
	var flagUnset bool
	var flagShow bool
	cmd := &cobra.Command{
		Use:   "local [tool@version]",
		Short: "Set or show project-level tool versions",
		Long: `local manages the .xdev.toml file in the current directory.

Without arguments, it shows the current project configuration.
With a tool@version argument, it sets the version for that tool.

Examples:
  xdev local node@20          # Set node to version 20
  xdev local python@^3.12     # Set python to any 3.12.x
  xdev local --unset node     # Remove node from project config
  xdev local --show           # Show current project config`,
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, err := os.Getwd()
			if err != nil {
				return err
			}

			if flagShow || len(args) == 0 {
				return showLocal(dir)
			}

			if flagUnset {
				for _, arg := range args {
					name, _ := splitToolVer(arg)
					if err := project.UnsetTool(dir, name); err != nil {
						return err
					}
					fmt.Printf("✔ removed %s from .xdev.toml\n", name)
				}
				return nil
			}

			// Set tool versions
			for _, arg := range args {
				name, ver := splitToolVer(arg)
				if ver == "" {
					// If no version specified, use the currently active version
					ver = currentGlobalVersion(name)
					if ver == "" {
						return fmt.Errorf("no active version for %s; specify a version (e.g. %s@20)", name, name)
					}
				}
				if err := project.SetTool(dir, name, ver); err != nil {
					return err
				}
				fmt.Printf("✔ set %s = %q in .xdev.toml\n", name, ver)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&flagUnset, "unset", false, "remove a tool from the project config")
	cmd.Flags().BoolVar(&flagShow, "show", false, "show the current project configuration")
	return cmd
}

// showLocal displays the project-level configuration for the current directory.
func showLocal(dir string) error {
	projDir, cfg := project.FindConfig(dir)
	if cfg == nil {
		fmt.Println("No .xdev.toml found in this directory or any parent.")
		return nil
	}
	if projDir != dir {
		fmt.Printf("(using .xdev.toml from %s)\n", projDir)
	}
	fmt.Println("Tools:")
	fmt.Print(project.FormatTools(cfg))
	if len(cfg.Env) > 0 {
		fmt.Println("Environment:")
		for k, v := range cfg.Env {
			fmt.Printf("  %-20s %s\n", k, v)
		}
	}
	if len(cfg.Tasks) > 0 {
		fmt.Println("Tasks:")
		for name, task := range cfg.Tasks {
			fmt.Printf("  %-20s %s\n", name, task.Desc)
		}
	}
	return nil
}

// currentGlobalVersion returns the currently active global version for a tool.
func currentGlobalVersion(toolName string) string {
	ver, _ := activeVersion(toolName)
	return ver
}
