package orchestrator

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

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
	args := append([]string{"exec", id}, cmd...)
	result, _ := util.RunCommand(ctx, "docker", args...)
	return &result, nil
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

// SaveInstance persists an instance ID to the xdev home for later lookup.
func SaveInstance(xmdeDir string, inst *Instance) error {
	dir := xmdeDir + string(os.PathSeparator) + "remote"
	os.MkdirAll(dir, 0o755)
	// Write a simple marker file with the container ID
	marker := dir + string(os.PathSeparator) + inst.ID
	return os.WriteFile(marker, []byte(inst.Status), 0o644)
}
