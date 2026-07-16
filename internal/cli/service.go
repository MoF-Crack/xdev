// service.go defines the `xdev service` command group for managing
// long-running development services (databases, caches, web servers).
//
// Subcommands: list, start, stop, status, logs, restart.
// The logs subcommand supports --follow (-f) for tailing, with proper
// context cancellation, log rotation handling, and stat error counting.
package cli

import (
	"fmt"
	"io"
	"os"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/xmdemt/xdev/internal/service"
)

// newServiceCmd handles `xdev service list|start|stop|status`.
func newServiceCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "service",
		Short: "Manage development services (databases, caches, web servers)",
		Long: `service manages long-running development services installed via xdev.

Services are installed through manifests and can be started, stopped,
and monitored from the CLI.

Examples:
  xdev service list           # List available services
  xdev service start mysql    # Start the MySQL service
  xdev service stop redis     # Stop the Redis service
  xdev service status         # Show status of all services`,
	}

	cmd.AddCommand(
		newServiceListCmd(),
		newServiceStartCmd(),
		newServiceStopCmd(),
		newServiceStatusCmd(),
		newServiceLogsCmd(),
		newServiceRestartCmd(),
	)

	return cmd
}

func newServiceListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List available services",
		RunE: func(cmd *cobra.Command, args []string) error {
			mgr := service.NewManager(Cfg())
			services := mgr.List()
			if len(services) == 0 {
				fmt.Println("No services installed. Install a service manifest first.")
				return nil
			}
			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "NAME\tTYPE\tVERSION\tPORT\tSTATUS")
			for _, s := range services {
				port := "-"
				if s.Port > 0 {
					port = fmt.Sprintf("%d", s.Port)
				}
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", s.Name, s.Type, s.Version, port, s.Status)
			}
			w.Flush()
			return nil
		},
	}
}

func newServiceStartCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "start <name>",
		Short: "Start a service",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			mgr := service.NewManager(Cfg())
			if err := mgr.Start(args[0]); err != nil {
				return err
			}
			fmt.Printf("✔ service %q started\n", args[0])
			return nil
		},
	}
}

func newServiceStopCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stop <name>",
		Short: "Stop a service",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			mgr := service.NewManager(Cfg())
			if err := mgr.Stop(args[0]); err != nil {
				return err
			}
			fmt.Printf("✔ service %q stopped\n", args[0])
			return nil
		},
	}
}

func newServiceStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status [name]",
		Short: "Show service status",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			mgr := service.NewManager(Cfg())
			if len(args) == 1 {
				svc, err := mgr.Status(args[0])
				if err != nil {
					return err
				}
				printServiceDetail(svc)
				return nil
			}
			// Show all services
			services := mgr.List()
			if len(services) == 0 {
				fmt.Println("No services installed.")
				return nil
			}
			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "NAME\tTYPE\tVERSION\tPORT\tSTATUS\tPID")
			for _, s := range services {
				port := "-"
				if s.Port > 0 {
					port = fmt.Sprintf("%d", s.Port)
				}
				pid := "-"
				if s.Pid > 0 {
					pid = fmt.Sprintf("%d", s.Pid)
				}
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n", s.Name, s.Type, s.Version, port, s.Status, pid)
			}
			w.Flush()
			return nil
		},
	}
}

func printServiceDetail(svc *service.Service) {
	fmt.Printf("Service: %s\n", svc.Name)
	fmt.Printf("  Type:    %s\n", svc.Type)
	fmt.Printf("  Version: %s\n", svc.Version)
	fmt.Printf("  Status:  %s\n", svc.Status)
	if svc.Port > 0 {
		fmt.Printf("  Port:    %d\n", svc.Port)
	}
	if svc.Pid > 0 {
		fmt.Printf("  PID:     %d\n", svc.Pid)
	}
	fmt.Printf("  DataDir: %s\n", svc.DataDir)
}

func newServiceLogsCmd() *cobra.Command {
	var flagFollow bool
	cmd := &cobra.Command{
		Use:   "logs <name>",
		Short: "Show service logs",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			mgr := service.NewManager(Cfg())
			name := args[0]
			content, err := mgr.ReadLogs(name)
			if err != nil {
				return err
			}
			fmt.Print(content)
			if !flagFollow {
				return nil
			}
			logPath := mgr.LogPath(name)
				offset := int64(len(content))
				statErrCount := 0
				for {
					// Check for context cancellation (Ctrl+C)
					select {
					case <-cmd.Context().Done():
						return nil
					default:
					}
					time.Sleep(500 * time.Millisecond)
					fi, err := os.Stat(logPath)
					if err != nil {
						statErrCount++
						if statErrCount > 10 {
							return fmt.Errorf("log file %q no longer accessible", logPath)
						}
						continue
					}
					statErrCount = 0
					// Handle log rotation/truncation
					if fi.Size() < offset {
						offset = 0
					}
					if fi.Size() > offset {
						f, err := os.Open(logPath)
						if err != nil {
							continue
						}
						if _, err := f.Seek(offset, 0); err != nil {
							f.Close()
							continue
						}
						io.Copy(os.Stdout, f)
						f.Close()
						offset = fi.Size()
					}
				}
		},
	}
	cmd.Flags().BoolVarP(&flagFollow, "follow", "f", false, "Follow log output")
	return cmd
}

func newServiceRestartCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "restart <name>",
		Short: "Restart a service",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			mgr := service.NewManager(Cfg())
			if err := mgr.Restart(args[0]); err != nil {
				return err
			}
			fmt.Printf("✔ service %q restarted\n", args[0])
			return nil
		},
	}
}
