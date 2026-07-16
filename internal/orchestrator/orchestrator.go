// Package orchestrator manages remote development environments
// through pluggable adapters (Docker, SSH, etc.).
package orchestrator

import (
	"context"
	"fmt"

	"github.com/xmdemt/xdev/internal/util"
)

// Instance represents a running remote development environment.
type Instance struct {
	ID        string            `json:"id"`
	Status    string            `json:"status"`    // "running" | "stopped" | "creating"
	Addresses []string          `json:"addresses"` // IP/hostname endpoints
	Metadata  map[string]string `json:"metadata"`
}

// Adapter is the interface for remote environment backends.
type Adapter interface {
	// Name returns the adapter identifier (e.g. "docker", "ssh").
	Name() string
	// Create provisions a new remote environment.
	Create(ctx context.Context, spec *EnvironmentSpec) (*Instance, error)
	// Start resumes a stopped environment.
	Start(ctx context.Context, id string) error
	// Stop suspends a running environment.
	Stop(ctx context.Context, id string) error
	// Destroy permanently removes an environment.
	Destroy(ctx context.Context, id string) error
	// Exec runs a command inside a running environment.
	Exec(ctx context.Context, id string, cmd []string) (*ExecResult, error)
}

// EnvironmentSpec describes the desired remote environment configuration.
type EnvironmentSpec struct {
	Image      string            `json:"image"`      // container image or VM template
	Tools      map[string]string `json:"tools"`      // tool name -> version
	Env        map[string]string `json:"env"`        // env vars
	WorkingDir string            `json:"workingDir"` // working directory
	Ports      []int             `json:"ports"`      // ports to expose
	Resources  ResourceSpec      `json:"resources"`  // resource limits
	SSHTarget  SSHTarget         `json:"sshTarget"`  // SSH connection details (for SSH adapter)
}

// ResourceSpec defines resource constraints for the environment.
type ResourceSpec struct {
	CPUs   float64 `json:"cpus"`   // number of CPUs
	Memory string  `json:"memory"` // e.g. "4g"
	Disk   string  `json:"disk"`   // e.g. "20g"
}

// SSHTarget defines connection details for the SSH adapter.
type SSHTarget struct {
	Host       string `json:"host"`
	Port       int    `json:"port"`
	User       string `json:"user"`
	KeyPath    string `json:"keyPath"`
	KnownHosts string `json:"knownHosts"`
}

// ExecResult holds the output of a remote command execution.
type ExecResult = util.ExecResult

// AdapterType identifies the type of remote adapter.
type AdapterType string

const (
	AdapterDocker AdapterType = "docker"
	AdapterSSH    AdapterType = "ssh"
)

// NewAdapter creates an Adapter for the given type.
func NewAdapter(t AdapterType) (Adapter, error) {
	switch t {
	case AdapterDocker, "":
		return &DockerAdapter{}, nil
	case AdapterSSH:
		return &SSHAdapter{}, nil
	default:
		return nil, fmt.Errorf("unsupported adapter type %q", t)
	}
}
