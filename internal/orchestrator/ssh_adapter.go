package orchestrator

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/xmdemt/xdev/internal/shim"
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
	target = normalizeTarget(target)
	// Check that ssh is available
	sshBin := sshBinary()
	if _, err := exec.LookPath(sshBin); err != nil {
		return nil, fmt.Errorf("ssh client not found: install OpenSSH or set PATH")
	}
	return &SSHAdapter{target: target}, nil
}

// normalizeTarget applies adapter-side defaults to an SSH target: the user
// three-step chain ("" → $USER → $USERNAME → root) and port 0 → 22. It is
// shared by NewSSHAdapter and Create's spec-consumption path (ticket 07) so
// a zero-user target can never be rendered as "@host". Explicit values
// coming from CLI flags or GUI prefill always win — normalization only
// fills empties, so the two defaulting layers do not conflict.
func normalizeTarget(t SSHTarget) SSHTarget {
	if t.User == "" {
		t.User = os.Getenv("USER")
		if t.User == "" {
			t.User = os.Getenv("USERNAME")
		}
		if t.User == "" {
			t.User = "root"
		}
	}
	if t.Port == 0 {
		t.Port = 22
	}
	return t
}

func (s *SSHAdapter) Name() string { return "ssh" }

// sshPassArgv builds the complete sshpass argv for a wrapped ssh invocation.
// sshpass reads the password from the SSHPASS environment variable with -e
// (supported across the whole 1.06→1.10 line; note 1.10's optional
// -e[env_var] suffix must be glued to the flag, so the bare "-e" form is the
// portable choice — a space-separated "SSHPASS" word would be parsed as the
// command to run, making password login fail 100% of the time).
//
// This is the ONLY place the sshpass argv prefix may be constructed
// (audit-fixes ticket 02 / X-01): runSSH and ConnectInteractive must both
// route through it, never hand-copying the {"-e", sshBin, ...} literal.
func sshPassArgv(sshBin string, args []string) []string {
	return append([]string{"-e", sshBin}, args...)
}

// runSSH executes the ssh client. When a password is configured it routes
// through sshpass (must be installed); otherwise it uses the system ssh
// directly. Returns an error if sshpass is required but unavailable.
// Both stdout and stderr are captured (ticket 02: sshpass failures such as
// "Failed to run command" land on stderr and Exec must surface them).
func (s *SSHAdapter) runSSH(ctx context.Context, args ...string) ([]byte, []byte, error) {
	if s.target.Password != "" {
		if _, err := exec.LookPath("sshpass"); err != nil {
			return nil, nil, fmt.Errorf("密码登录需要 sshpass（未在 PATH 中找到）。请安装 sshpass 或改用密钥登录")
		}
		// Prefer env-file style (-e) so the password is not visible in
		// the process argument list (ps / Task Manager).
		cmd := exec.CommandContext(ctx, "sshpass", sshPassArgv(sshBinary(), args)...)
		cmd.Env = append(os.Environ(), "SSHPASS="+s.target.Password)
		util.HideWindow(cmd)
		var outBuf, errBuf bytes.Buffer
		cmd.Stdout = &outBuf
		cmd.Stderr = &errBuf
		err := cmd.Run()
		return outBuf.Bytes(), errBuf.Bytes(), err
	}
	cmd := exec.CommandContext(ctx, sshBinary(), args...)
	util.HideWindow(cmd)
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	err := cmd.Run()
	return outBuf.Bytes(), errBuf.Bytes(), err
}

// TestConnection verifies the SSH connection by running `echo ok`.
func (s *SSHAdapter) TestConnection(ctx context.Context) error {
	out, _, err := s.runSSH(ctx, s.sshArgs("echo", "ok")...)
	if err != nil {
		return fmt.Errorf("ssh connect test failed: %w", err)
	}
	if strings.TrimSpace(string(out)) != "ok" {
		return fmt.Errorf("ssh connect test returned unexpected output: %s", strings.TrimSpace(string(out)))
	}
	return nil
}

func (s *SSHAdapter) Create(ctx context.Context, spec *EnvironmentSpec) (*Instance, error) {
	// Ticket 07 (audit-fixes / P0-10): consume the spec's SSHTarget when the
	// adapter was created zero-valued via NewAdapter(AdapterSSH). Without
	// this, targets passed through EnvironmentSpec were silently dropped:
	// the GUI could pass "test connection" (via NewSSHAdapter) yet "create"
	// (via NewAdapter + spec) always failed against the empty target.
	// The Host guard keeps an explicitly constructed target (NewSSHAdapter /
	// CLI flag / GUI prefill) intact — the spec never overwrites it.
	if spec != nil && spec.SSHTarget.Host != "" && s.target.Host == "" {
		t := normalizeTarget(spec.SSHTarget)
		if err := validateSSHTarget(t); err != nil {
			return nil, err
		}
		s.target = t
	}

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
	// Password path: run through sshpass via runSSH. Ticket 02 (P1-32):
	// ExitCode must reflect the real exit status and stderr must be
	// surfaced — previously both were hard-coded to 0/"" even when the
	// remote command failed. The error propagates alongside the result.
	if s.target.Password != "" {
		stdout, stderr, err := s.runSSH(ctx, sshArgs...)
		return &ExecResult{
			Stdout:   string(stdout),
			Stderr:   string(stderr),
			ExitCode: util.ExitCode(err),
		}, err
	}
	// Key path (ticket 02 / N-05): the RunCommand error must propagate.
	// Previously it was discarded (`result, _ :=`), so a failed key-based
	// remote command looked like "success with empty output" to callers.
	result, err := util.RunCommand(ctx, sshBinary(), sshArgs...)
	return &result, err
}

// ConnectInteractive runs `ssh -t` with stdin/stdout/stderr wired to
// the terminal (Spec 032). Password auth still uses sshpass.
func (s *SSHAdapter) ConnectInteractive(ctx context.Context, id string, shell []string) error {
	args := append([]string{"-t"}, s.sshArgs(shell...)...)
	var c *exec.Cmd
	if s.target.Password != "" {
		if _, err := exec.LookPath("sshpass"); err != nil {
			return fmt.Errorf("密码登录需要 sshpass（未在 PATH 中找到）。请安装 sshpass 或改用密钥登录")
		}
		// Security: pass the password via the SSHPASS environment variable
		// (sshpass -e) instead of -p, so it never appears in the process
		// argument list (ps / Task Manager) — mirrors runSSH, via the same
		// sshPassArgv single source (ticket 02 / X-01).
		c = exec.CommandContext(ctx, "sshpass", sshPassArgv(sshBinary(), args)...)
		c.Env = append(os.Environ(), "SSHPASS="+s.target.Password)
	} else {
		c = exec.CommandContext(ctx, sshBinary(), args...)
	}
	c.Stdin = os.Stdin
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	if err := c.Run(); err != nil {
		// 第七轮审计 F-52：与 DockerAdapter.ConnectInteractive 同源修复 ——
		// 库函数内不得 os.Exit（会跳过 deferred 清理，并使 cli/remote.go 的
		// %w 包装与 main.go 的 exitCodeForError 收口失效）。返回
		// *shim.ExitCodeError，让真实退出码沿错误链传播。
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return &shim.ExitCodeError{Code: exitErr.ExitCode()}
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
	// Security (L-1 fix): path separators and dots are also rejected — the
	// host/user feed the instance-id filename (SaveInstance) and must not
	// traverse out of <home>/remote/.
	if strings.ContainsAny(value, " \t\n\r;&|`$<>\"'/\\") || strings.Contains(value, "..") || strings.HasPrefix(value, "-") {
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
