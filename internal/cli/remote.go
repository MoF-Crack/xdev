package cli

import (
	"context"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/xmdemt/xdev/internal/orchestrator"
)

var (
	remoteImage   string
	remoteSSHHost string
	remoteSSHPort int
	remoteSSHUser string
	remoteSSHKey  string
	remoteAdapter string
)

// newRemoteCmd handles `xdev remote create|list|connect|destroy`.
func newRemoteCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "remote",
		Short: "Manage remote development environments",
		Long: `remote creates and manages development environments on
local Docker or remote SSH hosts.

Examples:
  xdev remote create --adapter docker --image ubuntu:22.04
  xdev remote create --adapter ssh --host dev.example.com --user ubuntu
  xdev remote list
  xdev remote connect <id>
  xdev remote destroy <id>`,
	}

	cmd.PersistentFlags().StringVar(&remoteAdapter, "adapter", "docker", "remote adapter (docker | ssh)")
	cmd.PersistentFlags().StringVar(&remoteImage, "image", "", "container image (for docker adapter)")
	cmd.PersistentFlags().StringVar(&remoteSSHHost, "host", "", "SSH host (for ssh adapter)")
	cmd.PersistentFlags().IntVar(&remoteSSHPort, "port", 22, "SSH port")
	cmd.PersistentFlags().StringVar(&remoteSSHUser, "user", "root", "SSH user")
	cmd.PersistentFlags().StringVar(&remoteSSHKey, "ssh-key", "", "path to SSH private key")

	cmd.AddCommand(
		newRemoteCreateCmd(),
		newRemoteListCmd(),
		newRemoteConnectCmd(),
		newRemoteDestroyCmd(),
	)

	return cmd
}

func newRemoteCreateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "create",
		Short: "Create a remote development environment",
		RunE: func(cmd *cobra.Command, args []string) error {
			adapter, err := createAdapter()
			if err != nil {
				return err
			}

			spec := &orchestrator.EnvironmentSpec{
				Image: remoteImage,
				Env:   make(map[string]string),
				SSHTarget: orchestrator.SSHTarget{
					Host:    remoteSSHHost,
					Port:    remoteSSHPort,
					User:    remoteSSHUser,
					KeyPath: remoteSSHKey,
				},
			}

			inst, err := adapter.Create(cmd.Context(), spec)
			if err != nil {
				return fmt.Errorf("create remote environment: %w", err)
			}

			// Persist instance info
			if err := orchestrator.SaveInstance(Cfg().Core.XMDevDir, inst); err != nil {
				fmt.Fprintf(os.Stderr, "warning: could not save instance info: %v\n", err)
			}

			fmt.Printf("✔ remote environment created\n")
			fmt.Printf("  ID:     %s\n", inst.ID)
			fmt.Printf("  Status: %s\n", inst.Status)
			if len(inst.Addresses) > 0 {
				fmt.Printf("  Addr:   %s\n", inst.Addresses[0])
			}
			return nil
		},
	}
}

func newRemoteListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List remote environments",
		RunE: func(cmd *cobra.Command, args []string) error {
			instances, err := orchestrator.ListContainers(cmd.Context())
			if err != nil {
				// Docker might not be available; show empty list
				fmt.Println("No remote environments found.")
				return nil
			}
			if len(instances) == 0 {
				fmt.Println("No remote environments found.")
				return nil
			}
			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "ID\tSTATUS\tNAME")
			for _, inst := range instances {
				name := inst.Metadata["name"]
				fmt.Fprintf(w, "%s\t%s\t%s\n", shortID(inst.ID), inst.Status, name)
			}
			w.Flush()
			return nil
		},
	}
}

func newRemoteConnectCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "connect <id>",
		Short: "Connect to a remote environment (interactive TTY)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			adapter, err := createAdapter()
			if err != nil {
				return err
			}
			// Interactive shell with a real TTY (Spec 032).
			if ic, ok := adapter.(interface {
				ConnectInteractive(ctx context.Context, id string, shell []string) error
			}); ok {
				if err := ic.ConnectInteractive(cmd.Context(), args[0], []string{"/bin/bash"}); err != nil {
					return fmt.Errorf("connect to %s: %w", args[0], err)
				}
				return nil
			}
			// Fallback: captured exec for adapters without a TTY path.
			result, err := adapter.Exec(cmd.Context(), args[0], []string{"/bin/bash"})
			if err != nil {
				return fmt.Errorf("connect to %s: %w", args[0], err)
			}
			forwardExecResult(result)
			return nil
		},
	}
}

func newRemoteDestroyCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "destroy <id>",
		Short: "Destroy a remote environment",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			adapter, err := createAdapter()
			if err != nil {
				return err
			}
			if err := adapter.Destroy(cmd.Context(), args[0]); err != nil {
				return fmt.Errorf("destroy %s: %w", args[0], err)
			}
			fmt.Printf("✔ remote environment %s destroyed\n", args[0])
			return nil
		},
	}
}

// createAdapter instantiates the appropriate remote adapter.
func createAdapter() (orchestrator.Adapter, error) {
	switch remoteAdapter {
	case "docker":
		return orchestrator.NewDockerAdapter()
	case "ssh":
		target := orchestrator.SSHTarget{
			Host:    remoteSSHHost,
			Port:    remoteSSHPort,
			User:    remoteSSHUser,
			KeyPath: remoteSSHKey,
		}
		return orchestrator.NewSSHAdapter(target)
	default:
		return nil, fmt.Errorf("unsupported adapter %q (use docker or ssh)", remoteAdapter)
	}
}

// shortID returns the first 12 characters of a container ID.
func shortID(id string) string {
	if len(id) > 12 {
		return id[:12]
	}
	return id
}
