package isolator

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/xmdemt/xdev/internal/util"
)

// containerIsolator creates a Docker container based on devcontainer.json
// and executes commands inside it.
type containerIsolator struct {
	containerID string
	image       string
	spec        *EnvironmentSpec
	projectDir  string
	xmdeDir     string
}

func newContainerIsolator() (*containerIsolator, error) {
	if _, err := exec.LookPath("docker"); err != nil {
		return nil, fmt.Errorf("docker not found: install Docker Desktop or Docker Engine to use container isolation")
	}
	// Verify Docker daemon is running
	cmd := exec.Command("docker", "info")
	util.HideWindow(cmd)
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("docker daemon is not running: start Docker and try again")
	}
	return &containerIsolator{}, nil
}

// loadSaved restores a previously persisted container session.
func (c *containerIsolator) loadSaved() {
	s := loadState(c.xmdeDir, string(ModeContainer))
	if s.ContainerID != "" {
		c.containerID = s.ContainerID
		c.image = s.Image
		c.projectDir = s.ProjectDir
	}
}

func (c *containerIsolator) Mode() IsolationMode { return ModeContainer }

func (c *containerIsolator) Setup(ctx context.Context, spec *EnvironmentSpec) error {
	c.spec = spec

	// Determine project directory
	if spec.WorkingDir != "" {
		c.projectDir = spec.WorkingDir
	} else {
		dir, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("get working dir: %w", err)
		}
		c.projectDir = dir
	}

	// Generate devcontainer.json
	if err := c.generateDevcontainer(); err != nil {
		return fmt.Errorf("generate devcontainer.json: %w", err)
	}

	// Determine image
	c.image = "mcr.microsoft.com/devcontainers/base:ubuntu"
	if img, ok := spec.Env["DEVCONTAINER_IMAGE"]; ok {
		c.image = img
	}

	// Build and start container
	args := []string{
		"run", "-d",
		"--name", containerName(c.projectDir),
		"-v", c.projectDir + ":/workspace",
		"-w", "/workspace",
	}
	// Add env vars
	for k, v := range spec.Env {
		args = append(args, "-e", fmt.Sprintf("%s=%s", k, v))
	}
	// Add port forwards for tools
	// Security (N-14): insert "--" before the image name to prevent
	// an image name starting with "-" from being interpreted as a
	// docker flag (argument injection).
	args = append(args, "--", c.image, "sleep", "infinity")

	cmd := exec.CommandContext(ctx, "docker", args...)
	util.HideWindow(cmd)
	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("docker run: %w", err)
	}
	c.containerID = strings.TrimSpace(string(out))
	saveState(c.xmdeDir, string(ModeContainer), persistedState{
		ContainerID: c.containerID,
		Image:       c.image,
		ProjectDir:  c.projectDir,
	})
	return nil
}

func (c *containerIsolator) Teardown(ctx context.Context) error {
	if c.containerID == "" {
		return nil
	}
	// Stop and remove the container
	cmd := exec.CommandContext(ctx, "docker", "rm", "-f", c.containerID)
	util.HideWindow(cmd)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker rm: %w", err)
	}
	c.containerID = ""
	clearState(c.xmdeDir, string(ModeContainer))
	return nil
}

func (c *containerIsolator) Exec(ctx context.Context, cmd []string) (*ExecResult, error) {
	if c.containerID == "" {
		return nil, fmt.Errorf("container not set up; run 'xdev isolate setup' first")
	}
	if len(cmd) == 0 {
		return nil, fmt.Errorf("empty command")
	}

	args := append([]string{"exec", c.containerID}, cmd...)
	result, _ := util.RunCommand(ctx, "docker", args...)
	return &result, nil
}

// generateDevcontainer writes a devcontainer.json to the project's
// .devcontainer directory.
func (c *containerIsolator) generateDevcontainer() error {
	dcDir := filepath.Join(c.projectDir, ".devcontainer")
	if err := os.MkdirAll(dcDir, 0o755); err != nil {
		return err
	}

	image := "mcr.microsoft.com/devcontainers/base:ubuntu"
	if c.spec != nil {
		if img, ok := c.spec.Env["DEVCONTAINER_IMAGE"]; ok {
			image = img
		}
	}

	dc := map[string]interface{}{
		"name":            "xdev-isolated",
		"image":           image,
		"workspaceFolder": "/workspace",
		"workspaceMount":  fmt.Sprintf("source=%s,target=/workspace,type=bind", c.projectDir),
	}

	data, err := json.MarshalIndent(dc, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dcDir, "devcontainer.json"), data, 0o644)
}

// containerName generates a deterministic container name from the project dir.
func containerName(projectDir string) string {
	name := filepath.Base(projectDir)
	if name == "" || name == "." {
		name = "xdev"
	}
	return "xdev-" + name
}
