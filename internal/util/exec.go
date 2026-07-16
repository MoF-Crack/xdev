package util

import (
	"context"
	"os/exec"
	"strings"
)

// ExecResult holds the output of a subprocess execution.
type ExecResult struct {
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
	ExitCode int    `json:"exitCode"`
}

// ExitCode extracts the exit code from an error returned by exec.Run.
// Returns 0 if err is nil, 1 for non-ExitError errors.
func ExitCode(err error) int {
	if err == nil {
		return 0
	}
	if exitErr, ok := err.(*exec.ExitError); ok {
		return exitErr.ExitCode()
	}
	return 1
}

// RunCommand executes a command with the given context, capturing
// stdout and stderr. It automatically hides the console window on
// Windows.
func RunCommand(ctx context.Context, name string, args ...string) (ExecResult, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	HideWindow(cmd)
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return ExecResult{
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		ExitCode: ExitCode(err),
	}, err
}
