package cli

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/xmdemt/xdev/internal/project"
	"github.com/xmdemt/xdev/internal/task"
)

// newTaskCmd handles `xdev task list` and `xdev task run <name>`.
func newTaskCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "task",
		Short: "Manage and run project tasks",
		Long: `task lists and runs named commands defined in .xdev.toml.

Tasks are defined in the [tasks] section:
  [tasks.build]
  run = "go build ./..."
  desc = "Build the project"

  [tasks.test]
  run = "go test ./..."
  desc = "Run tests"`,
	}

	cmd.AddCommand(
		newTaskListCmd(),
		newTaskRunCmd(),
	)

	return cmd
}

func newTaskListCmd() *cobra.Command {
	var flagJSON bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List available tasks",
		RunE: func(cmd *cobra.Command, args []string) error {
			_, proj := findProjectConfig()
			if proj == nil || len(proj.Tasks) == 0 {
				if flagJSON {
					fmt.Println("[]")
					return nil
				}
				fmt.Println("No tasks defined. Add [tasks] to .xdev.toml")
				return nil
			}
			if flagJSON {
				return printTasksJSON(proj.Tasks)
			}
			fmt.Println("Tasks:")
			fmt.Print(task.FormatTaskList(proj.Tasks))
			return nil
		},
	}
	cmd.Flags().BoolVar(&flagJSON, "json", false, "Output tasks as JSON")
	return cmd
}

// taskJSONEntry is the JSON representation of a single task for
// `xdev task list --json`.
type taskJSONEntry struct {
	Name      string   `json:"name"`
	Run       string   `json:"run"`
	Desc      string   `json:"desc,omitempty"`
	Dir       string   `json:"dir,omitempty"`
	Sources   []string `json:"sources,omitempty"`
	DependsOn []string `json:"depends_on,omitempty"`
}

func printTasksJSON(tasks map[string]project.Task) error {
	entries := make([]taskJSONEntry, 0, len(tasks))
	for name, t := range tasks {
		entries = append(entries, taskJSONEntry{
			Name:      name,
			Run:       t.Run,
			Desc:      t.Desc,
			Dir:       t.Dir,
			Sources:   t.Sources,
			DependsOn: t.DependsOn,
		})
	}
	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(data))
	return nil
}

func newTaskRunCmd() *cobra.Command {
	var flagDryRun bool
	cmd := &cobra.Command{
		Use:   "run <task-name>",
		Short: "Run a named task",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTaskE(args[0], flagDryRun)
		},
	}
	cmd.Flags().BoolVar(&flagDryRun, "dry-run", false, "Print the command without executing it")
	return cmd
}

// newRunCmd is the shorthand `xdev run <name>`.
func newRunCmd() *cobra.Command {
	var flagDryRun bool
	cmd := &cobra.Command{
		Use:   "run <task-name>",
		Short: "Run a named task (shorthand for 'task run')",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTaskE(args[0], flagDryRun)
		},
	}
	cmd.Flags().BoolVar(&flagDryRun, "dry-run", false, "Print the command without executing it")
	return cmd
}

// runTaskE is the shared RunE for `xdev task run` and `xdev run`.
// When dryRun is true, the interpolated command is printed and the
// task is not executed.
func runTaskE(name string, dryRun bool) error {
	projDir, proj := findProjectConfig()
	if proj == nil {
		return fmt.Errorf("no .xdev.toml found")
	}
	runner := task.NewRunner(proj, projDir).
		SetConfig(Cfg()).
		SetDryRun(dryRun)
	if err := runner.Run(name); err != nil {
		return fmt.Errorf("task %q failed: %w", name, err)
	}
	return nil
}

// findProjectConfig is a helper to find the project config.
func findProjectConfig() (string, *project.ProjectConfig) {
	return project.FindConfig(".")
}
