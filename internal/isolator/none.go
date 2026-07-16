package isolator

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/xmdemt/xdev/internal/util"
)

// noneIsolator runs commands in the current process environment
// with PATH and env vars modified in-place. This is the default
// mode and matches the existing xdev behavior.
type noneIsolator struct {
	origEnv map[string]string
	spec    *EnvironmentSpec
}

func newNoneIsolator() *noneIsolator {
	return &noneIsolator{}
}

func (n *noneIsolator) Mode() IsolationMode { return ModeNone }

func (n *noneIsolator) Setup(ctx context.Context, spec *EnvironmentSpec) error {
	n.spec = spec
	// Save original env vars so we can restore on teardown
	n.origEnv = make(map[string]string)
	for k := range spec.Env {
		n.origEnv[k] = os.Getenv(k)
	}
	// Apply env vars to current process
	for k, v := range spec.Env {
		os.Setenv(k, v)
	}
	// Prepend tool bin dirs to PATH if tools are specified
	if len(spec.Tools) > 0 {
		// Tools are resolved via xdev's normal PATH injection;
		// the none isolator relies on the existing shim mechanism.
	}
	return nil
}

func (n *noneIsolator) Teardown(ctx context.Context) error {
	if n.origEnv == nil {
		return nil
	}
	// Restore original env vars
	for k, v := range n.origEnv {
		if v == "" {
			os.Unsetenv(k)
		} else {
			os.Setenv(k, v)
		}
	}
	n.origEnv = nil
	n.spec = nil
	return nil
}

func (n *noneIsolator) Exec(ctx context.Context, cmd []string) (*ExecResult, error) {
	if len(cmd) == 0 {
		return nil, fmt.Errorf("empty command")
	}

	name := cmd[0]
	args := cmd[1:]

	// If the command is a known tool name, resolve via PATH
	resolved, err := exec.LookPath(name)
	if err != nil {
		resolved = name
	}

	c := exec.CommandContext(ctx, resolved, args...)
	util.HideWindow(c)

	var stdout, stderr strings.Builder
	c.Stdout = &stdout
	c.Stderr = &stderr
	c.Env = os.Environ()

	if n.spec != nil && n.spec.WorkingDir != "" {
		c.Dir = n.spec.WorkingDir
	}

	err = c.Run()
	result := &ExecResult{
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		ExitCode: util.ExitCode(err),
	}
	return result, nil
}
