package orchestrator

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/xmdemt/xdev/internal/util"
)

// SSHAdapter connects to remote machines via the system SSH client.
// It uses the native ssh command rather than a Go SSH library to
// avoid pulling in golang.org/x/crypto/ssh as a hard dependency.
type SSHAdapter struct {
	target SSHTarget
}

// NewSSHAdapter creates an SSH adapter. Returns an error if the
// SSH client is not available on the system.
func NewSSHAdapter(target SSHTarget) (*SSHAdapter, error) {
	if target.Host == "" {
		return nil, fmt.Errorf("SSH target host is required")
	}
	if target.User == "" {
		target.User = os.Getenv("USER")
		if target.User == "" {
			target.User = os.Getenv("USERNAME")
		}
		if target.User == "" {
			target.User = "root"
		}
	}
	if target.Port == 0 {
		target.Port = 22
	}
	// Check that ssh is available
	sshBin := sshBinary()
	if _, err := exec.LookPath(sshBin); err != nil {
		return nil, fmt.Errorf("ssh client not found: install OpenSSH or set PATH")
	}
	return &SSHAdapter{target: target}, nil
}

func (s *SSHAdapter) Name() string { return "ssh" }

func (s *SSHAdapter) Create(ctx context.Context, spec *EnvironmentSpec) (*Instance, error) {
	// For SSH, "create" means verify the connection works
	args := s.sshArgs("echo", "ok")
	cmd := exec.CommandContext(ctx, sshBinary(), args...)
	util.HideWindow(cmd)
	if out, err := cmd.Output(); err != nil {
		return nil, fmt.Errorf("ssh connect test failed: %w", err)
	} else if strings.TrimSpace(string(out)) != "ok" {
		return nil, fmt.Errorf("ssh connect test returned unexpected output: %s", strings.TrimSpace(string(out)))
	}

	addr := fmt.Sprintf("%s:%d", s.target.Host, s.target.Port)
	return &Instance{
		ID:        s.instanceID(),
		Status:    "running",
		Addresses: []string{addr},
		Metadata: map[string]string{
			"user":    s.target.User,
			"adapter": "ssh",
		},
	}, nil
}

func (s *SSHAdapter) Start(ctx context.Context, id string) error {
	// For SSH, "start" means verify the connection
	args := s.sshArgs("echo", "ok")
	cmd := exec.CommandContext(ctx, sshBinary(), args...)
	util.HideWindow(cmd)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ssh start: %w", err)
	}
	return nil
}

func (s *SSHAdapter) Stop(ctx context.Context, id string) error {
	// For SSH, "stop" is a no-op (the remote machine stays running)
	return nil
}

func (s *SSHAdapter) Destroy(ctx context.Context, id string) error {
	// For SSH, "destroy" is a no-op (we don't own the remote machine)
	return nil
}

func (s *SSHAdapter) Exec(ctx context.Context, id string, cmd []string) (*ExecResult, error) {
	if len(cmd) == 0 {
		return nil, fmt.Errorf("empty command")
	}

	sshArgs := s.sshArgs(cmd...)
	result, _ := util.RunCommand(ctx, sshBinary(), sshArgs...)
	return &result, nil
}

// sshArgs builds the SSH command-line arguments.
func (s *SSHAdapter) sshArgs(cmd ...string) []string {
	args := []string{}
	if s.target.Port != 22 {
		args = append(args, "-p", fmt.Sprintf("%d", s.target.Port))
	}
	if s.target.KeyPath != "" {
		args = append(args, "-i", s.target.KeyPath)
	}
	// Host key verification strategy:
	// - If KnownHosts is specified, use that file with strict checking
	// - Otherwise, use the default known_hosts file with accept-new
	//   (auto-accept on first connection, reject on key change)
	// - Never use UserKnownHostsFile=/dev/null or StrictHostKeyChecking=no
	//   as they disable all MITM protection.
	if s.target.KnownHosts != "" {
		args = append(args, "-o", "UserKnownHostsFile="+s.target.KnownHosts)
		args = append(args, "-o", "StrictHostKeyChecking=yes")
	} else {
		args = append(args, "-o", "StrictHostKeyChecking=accept-new")
	}
	// Connect target
	target := fmt.Sprintf("%s@%s", s.target.User, s.target.Host)
	args = append(args, target)
	// Append the command to run
	args = append(args, cmd...)
	return args
}

// instanceID generates a stable ID from the SSH target.
func (s *SSHAdapter) instanceID() string {
	return fmt.Sprintf("ssh-%s-%s-%d", s.target.User, s.target.Host, s.target.Port)
}

// sshBinary returns the name of the SSH executable.
func sshBinary() string {
	if runtime.GOOS == "windows" {
		return "ssh.exe"
	}
	return "ssh"
}

// IsSSHAvailable checks whether the SSH adapter can be used.
func IsSSHAvailable() bool {
	_, err := exec.LookPath(sshBinary())
	return err == nil
}

// defaultSSHKeys returns common SSH key file paths.
func defaultSSHKeys(home string) []string {
	return []string{
		filepath.Join(home, ".ssh", "id_ed25519"),
		filepath.Join(home, ".ssh", "id_rsa"),
		filepath.Join(home, ".ssh", "id_ecdsa"),
		filepath.Join(home, ".ssh", "id_dsa"),
	}
}

// FindDefaultSSHKey attempts to locate a usable SSH private key.
func FindDefaultSSHKey() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	for _, key := range defaultSSHKeys(home) {
		if _, err := os.Stat(key); err == nil {
			return key
		}
	}
	return ""
}

// Ensure SSHAdapter satisfies Adapter interface.
var _ Adapter = (*SSHAdapter)(nil)
