package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

func newCompletionCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "completion [shell]",
		Short: "Generate shell completion script",
		Long: `Generate shell completion script for xdev.

Supported shells: bash, zsh, fish, powershell

Usage:
  xdev completion bash > /etc/bash_completion.d/xdev
  xdev completion zsh > "${fpath[1]}/_xdev"
  xdev completion fish > ~/.config/fish/completions/xdev.fish
  xdev completion powershell | Out-String | Invoke-Expression
`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			shell := args[0]
			switch shell {
			case "bash":
				return cmd.Root().GenBashCompletion(os.Stdout)
			case "zsh":
				return cmd.Root().GenZshCompletion(os.Stdout)
			case "fish":
				return cmd.Root().GenFishCompletion(os.Stdout, true)
			case "powershell":
				return cmd.Root().GenPowerShellCompletionWithDesc(os.Stdout)
			default:
				return fmt.Errorf("unsupported shell %q (use: bash, zsh, fish, powershell)", shell)
			}
		},
	}
	return cmd
}
