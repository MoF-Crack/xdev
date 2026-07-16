package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/xmdemt/xdev/internal/version"
)

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the current xdev version",
		Long:  "Print the current xdev version and build date.",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Printf("xdev %s", version.Version)
			if version.BuildDate != "" {
				fmt.Printf(" (built %s)", version.BuildDate)
			}
			fmt.Println()
			return nil
		},
	}
}
