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
	if err := validateSSHTarget(target); err != nil {
		return nil, err
	}
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

// runSSH executes the ssh client. When a password is configured it routes
// through sshpass (must be installed); otherwise it uses the system ssh
// directly. Returns an error if sshpass is required but unavailable.
func (s *SSHAdapter) runSSH(ctx context.Context, args ...string) ([]byte, error) {
	if s.target.Password != "" {
		if _, err := exec.LookPath("sshpass"); err != nil {
			return nil, fmt.Errorf("密码登录需要 sshpass（未在 PATH 中找到）。请安装 sshpass 或改用密钥登录")
		}
		// Prefer env-file style (-e) so the password is not visible in
		// the process argument list (ps / Task Manager).
		full := append([]string{"-e", "SSHPASS", sshBinary()}, args...)
		cmd := exec.CommandContext(ctx, "sshpass", full...)
		cmd.Env = append(os.Environ(), "SSHPASS="+s.target.Password)
		util.HideWindow(cmd)
		return cmd.Output()
	}
	cmd := exec.CommandContext(ctx, sshBinary(), args...)
	util.HideWindow(cmd)
	return cmd.Output()
}

// TestConnection verifies the SSH connection by running `echo ok`.
func (s *SSHAdapter) TestConnection(ctx context.Context) error {
	out, err := s.runSSH(ctx, s.sshArgs("echo", "ok")...)
	if err != nil {
		return fmt.Errorf("ssh connect test failed: %w", err)
	}
	if strings.TrimSpace(string(out)) != "ok" {
		return fmt.Errorf("ssh connect test returned unexpected output: %s", strings.TrimSpace(string(out)))
	}
	return nil
}

func (s *SSHAdapter) Create(ctx context.Context, spec *EnvironmentSpec) (*Instance, error) {
	// For SSH, "create" means verify the connection works
	if err := s.TestConnection(ctx); err != nil {
		return nil, err
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
	return s.TestConnection(ctx)
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
	// Password path: run through sshpass via runSSH.
	if s.target.Password != "" {
		out, err := s.runSSH(ctx, sshArgs...)
		return &ExecResult{Stdout: string(out), Stderr: "", ExitCode: 0}, err
	}
	result, _ := util.RunCommand(ctx, sshBinary(), sshArgs...)
	return &result, nil
}

// ConnectInteractive runs `ssh -t` with stdin/stdout/stderr wired to
// the terminal (Spec 032). Password auth still uses sshpass.
func (s *SSHAdapter) ConnectInteractive(ctx context.Context, id string, shell []string) error {
	args := append([]string{"-t"}, s.sshArgs(shell...)...)
	var c *exec.Cmd
	if s.target.Password != "" {
		c = exec.CommandContext(ctx, "sshpass", append([]string{"-p", s.target.Password, sshBinary()}, args...)...)
	} else {
		c = exec.CommandContext(ctx, sshBinary(), args...)
	}
	c.Stdin = os.Stdin
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	if err := c.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			os.Exit(exitErr.ExitCode())
		}
		return err
	}
	return nil
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
func sshFieldLooksUnsafe(name, value string) error {
	if value == "" {
		return nil
	}
	if strings.ContainsAny(value, " \t\n\r;&|`$<>\"'") || strings.HasPrefix(value, "-") {
		return fmt.Errorf("SSH %s contains unsafe characters", name)
	}
	return nil
}

func validateSSHTarget(target SSHTarget) error {
	if err := sshFieldLooksUnsafe("host", target.Host); err != nil {
		return err
	}
	if err := sshFieldLooksUnsafe("user", target.User); err != nil {
		return err
	}
	if target.Port < 0 || target.Port > 65535 {
		return fmt.Errorf("SSH port %d is out of range", target.Port)
	}
	if target.KeyPath != "" {
		if strings.ContainsAny(target.KeyPath, "\n\r;&|`$<>\"'") {
			return fmt.Errorf("SSH key path contains unsafe characters")
		}
	}
	if target.KnownHosts != "" {
		if strings.ContainsAny(target.KnownHosts, "\n\r;&|`$<>\"'") {
			return fmt.Errorf("SSH known_hosts path contains unsafe characters")
		}
	}
	return nil
}

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
