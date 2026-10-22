package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/xmdemt/xdev/internal/shim"
	"github.com/xmdemt/xdev/internal/util"
)

// DockerAdapter manages development environments using local Docker.
type DockerAdapter struct{}

// NewDockerAdapter creates a Docker adapter. Returns an error if
// Docker is not available on the system.
func NewDockerAdapter() (*DockerAdapter, error) {
	if _, err := exec.LookPath("docker"); err != nil {
		return nil, fmt.Errorf("docker not found: install Docker to use the docker adapter")
	}
	// Verify daemon is reachable
	cmd := exec.Command("docker", "info")
	util.HideWindow(cmd)
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("docker daemon is not running: start Docker and try again")
	}
	return &DockerAdapter{}, nil
}

func (d *DockerAdapter) Name() string { return "docker" }

func (d *DockerAdapter) Create(ctx context.Context, spec *EnvironmentSpec) (*Instance, error) {
	image := spec.Image
	if image == "" {
		image = "mcr.microsoft.com/devcontainers/base:ubuntu"
	}

	args := []string{"run", "-d", "--label", "xdev-managed"}
	// Expose ports
	for _, p := range spec.Ports {
		args = append(args, "-p", fmt.Sprintf("%d:%d", p, p))
	}
	// Set env vars
	for k, v := range spec.Env {
		args = append(args, "-e", fmt.Sprintf("%s=%s", k, v))
	}
	// Resource limits
	if spec.Resources.CPUs > 0 {
		args = append(args, "--cpus", fmt.Sprintf("%.1f", spec.Resources.CPUs))
	}
	if spec.Resources.Memory != "" {
		args = append(args, "-m", spec.Resources.Memory)
	}
	// Working directory
	if spec.WorkingDir != "" {
		args = append(args, "-w", spec.WorkingDir)
	}
	// Keep container running
	// Security (N-14): insert "--" before the image name to prevent
	// an image name starting with "-" from being interpreted as a
	// docker flag (argument injection).
	args = append(args, "--", image, "sleep", "infinity")

	cmd := exec.CommandContext(ctx, "docker", args...)
	util.HideWindow(cmd)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("docker run: %w", err)
	}
	containerID := strings.TrimSpace(string(out))

	// Get container IP address
	ip := d.getContainerIP(ctx, containerID)

	return &Instance{
		ID:        containerID,
		Status:    "running",
		Addresses: []string{ip},
		Metadata: map[string]string{
			"image":   image,
			"adapter": "docker",
		},
	}, nil
}

func (d *DockerAdapter) Start(ctx context.Context, id string) error {
	cmd := exec.CommandContext(ctx, "docker", "start", id)
	util.HideWindow(cmd)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker start %s: %w", id, err)
	}
	return nil
}

func (d *DockerAdapter) Stop(ctx context.Context, id string) error {
	cmd := exec.CommandContext(ctx, "docker", "stop", id)
	util.HideWindow(cmd)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker stop %s: %w", id, err)
	}
	return nil
}

func (d *DockerAdapter) Destroy(ctx context.Context, id string) error {
	cmd := exec.CommandContext(ctx, "docker", "rm", "-f", id)
	util.HideWindow(cmd)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker rm %s: %w", id, err)
	}
	return nil
}

func (d *DockerAdapter) Exec(ctx context.Context, id string, cmd []string) (*ExecResult, error) {
	if len(cmd) == 0 {
		return nil, fmt.Errorf("empty command")
	}
	// Security (M-6 fix): insert "--" after the container id so a command
	// element starting with "-" is never parsed as a docker exec option
	// (mirrors the N-14 guard on docker run).
	args := append([]string{"exec", "--", id}, cmd...)
	result, _ := util.RunCommand(ctx, "docker", args...)
	return &result, nil
}

// ConnectInteractive runs `docker exec -it` with stdin/stdout/stderr
// wired to the terminal so the user gets a real shell (Spec 032).
//
// 第七轮审计 F-52：本函数**不得**调用 os.Exit —— 它是在库函数里终止进程，
// 违反本仓库自定约定（internal/cli/isolate.go:9-11：「用 shim.ExitCodeError
// 传播子进程退出码，而不是 os.Exit（那会跳过 deferred 清理）」），并使
// cli/remote.go 的 `fmt.Errorf("connect to %s: %w", ...)` 包装与 main.go 的
// exitCodeForError 统一收口成为死代码。改为返回 *shim.ExitCodeError，
// 由 main.go 的 errors.As 提取真实退出码（用户 `exit 3` 应得到退出码 3）。
//
// 同时补上 M-6/N-14 的 `--` 参数注入守卫：此前只有两条 Exec 腿加了 `--`，
// 这条 connect 腿漏了，容器 id 以 "-" 开头时会被 docker 当作 exec 选项解析。
func (d *DockerAdapter) ConnectInteractive(ctx context.Context, id string, shell []string) error {
	args := append([]string{"exec", "-it", "--", id}, shell...)
	c := exec.CommandContext(ctx, "docker", args...)
	c.Stdin = os.Stdin
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	if err := c.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return &shim.ExitCodeError{Code: exitErr.ExitCode()}
		}
		return err
	}
	return nil
}

// getContainerIP returns the IP address of a running container.
func (d *DockerAdapter) getContainerIP(ctx context.Context, containerID string) string {
	cmd := exec.CommandContext(ctx, "docker", "inspect",
		"-f", "{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}",
		containerID)
	util.HideWindow(cmd)
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// ListContainers returns IDs of all xdev-managed Docker containers.
func ListContainers(ctx context.Context) ([]Instance, error) {
	cmd := exec.CommandContext(ctx, "docker", "ps", "-a",
		"--filter", "label=xdev-managed",
		"--format", "{{.ID}}\t{{.Status}}\t{{.Names}}")
	util.HideWindow(cmd)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("docker ps: %w", err)
	}
	var instances []Instance
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 3)
		inst := Instance{
			ID:     parts[0],
			Status: "running",
		}
		if len(parts) > 1 {
			if strings.HasPrefix(parts[1], "Exited") {
				inst.Status = "stopped"
			}
		}
		if len(parts) > 2 {
			inst.Metadata = map[string]string{"name": parts[2]}
		}
		instances = append(instances, inst)
	}
	return instances, nil
}

// IsDockerAvailable checks whether Docker is installed and running.
func IsDockerAvailable() bool {
	_, err := exec.LookPath("docker")
	if err != nil {
		return false
	}
	cmd := exec.Command("docker", "info")
	util.HideWindow(cmd)
	return cmd.Run() == nil
}

// Ensure DockerAdapter satisfies Adapter interface.
var _ Adapter = (*DockerAdapter)(nil)

// SaveInstance — the persisted instance store — moved to instance_store.go
// (ticket 35 / C-08): it is shared by every adapter, not docker-specific, and
// now writes <home>/instances/<id>.json instead of a bare <home>/remote/<ID>
// status marker.
