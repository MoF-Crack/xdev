// Package isolator provides environment isolation for xdev.
//
// Three modes are supported:
//   - "none":      no isolation, modify current process env (default)
//   - "container": Docker-based isolation via devcontainer.json
//   - "nix":       Nix flake-based isolation
package isolator

import (
	"context"
	"fmt"

	"github.com/xmdemt/xdev/internal/util"
)

// IsolationMode represents the type of environment isolation.
type IsolationMode string

const (
	ModeNone      IsolationMode = "none"
	ModeContainer IsolationMode = "container"
	ModeNix       IsolationMode = "nix"
)

// EnvironmentSpec describes the desired isolated environment.
type EnvironmentSpec struct {
	Tools      map[string]string `json:"tools"`      // tool name -> version
	Env        map[string]string `json:"env"`        // env var name -> value
	WorkingDir string            `json:"workingDir"` // working directory inside the environment
}

// ExecResult holds the output of a command executed in an isolated environment.
type ExecResult = util.ExecResult

// Isolator is the interface for environment isolation backends.
type Isolator interface {
	// Mode returns the isolation mode identifier.
	Mode() IsolationMode
	// Setup prepares the isolated environment.
	Setup(ctx context.Context, spec *EnvironmentSpec) error
	// Teardown destroys the isolated environment.
	Teardown(ctx context.Context) error
	// Exec runs a command inside the isolated environment.
	Exec(ctx context.Context, cmd []string) (*ExecResult, error)
}

// NewIsolator creates an Isolator for the given mode.
// Returns an error if the requested mode is not available
// (e.g. Docker not installed for container mode).
func NewIsolator(mode IsolationMode) (Isolator, error) {
	switch mode {
	case ModeNone, "":
		return newNoneIsolator(), nil
	case ModeContainer:
		return newContainerIsolator()
	case ModeNix:
		return newNixIsolator()
	default:
		return nil, fmt.Errorf("unsupported isolation mode %q", mode)
	}
}
