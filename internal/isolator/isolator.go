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

	// Force lets Setup overwrite files that already exist (C-07 / P1-34):
	// a hand-written flake.nix or devcontainer.json is the user's, and both
	// Setup and the following Teardown used to clobber it. It is carried on
	// the spec (not as new Isolator API surface) so the CLI `isolate setup
	// --force` flag and the GUI can both opt in; the zero value is the safe
	// default (refuse).
	Force bool `json:"force,omitempty"`
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

// NewIsolator creates an Isolator for the given mode. xmdeDir is the
// xdev home directory; isolators persist their session (container ID /
// flake dir) under <xmdeDir>/isolate/ so setup/exec share state across
// processes (Spec 032).
func NewIsolator(mode IsolationMode, xmdeDir string) (Isolator, error) {
	switch mode {
	case ModeNone, "":
		return newNoneIsolator(), nil
	case ModeContainer:
		c, err := newContainerIsolator()
		if err != nil {
			return nil, err
		}
		c.xmdeDir = xmdeDir
		c.loadSaved()
		return c, nil
	case ModeNix:
		n, err := newNixIsolator()
		if err != nil {
			return nil, err
		}
		n.xmdeDir = xmdeDir
		n.loadSaved()
		return n, nil
	default:
		return nil, fmt.Errorf("unsupported isolation mode %q", mode)
	}
}
