package isolator

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
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

	// stateErr records that the persisted session could not be read
	// (第九轮 A-02). A corrupt state file yields a zero-valued
	// persistedState, and a zero value is NOT "no container": it is
	// "unknown". Teardown must surface this instead of returning nil.
	stateErr error

	// writtenFiles records path → SHA-256 of the file generateDevcontainer
	// wrote (C-07). Teardown deliberately deletes no files (see below), but
	// the state stays truthful about what Setup created.
	writtenFiles map[string]string
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
//
// 第九轮 A-02：状态文件损坏时 loadState 返回错误，这里**必须**记下来而不是
// 丢弃——零值的 persistedState 会被 Teardown 解读成「没有容器」，正是容器
// 静默泄漏的根因。注意这里不清除、不改名损坏文件：quarantine 交给 Teardown
// （唯一真正消费该状态的路径）做，NewIsolator/Exec 只读不写。
func (c *containerIsolator) loadSaved() {
	s, err := loadState(c.xmdeDir, string(ModeContainer))
	if err != nil {
		c.stateErr = err
		return
	}
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
	// Add env vars.
	//
	// 第七轮审计 F-50：**不要**把宿主的 PATH 透传进容器。spec.Env["PATH"] 由
	// cli/isolate.go 组装为「宿主 shim 目录 + 宿主 PATH」，那条语义只对 `none`
	// 模式成立（noneIsolator 就地 os.Setenv）。容器里该值指向**宿主**路径，而
	// 挂载只有 `-v <projectDir>:/workspace` —— 于是：
	//   ① Windows 主机的分号分隔 PATH 在 Linux 容器里是**单个无效条目**，
	//      镜像自带的 `sleep` 无法经 PATH 解析 → docker run 失败
	//      （runc: executable file not found in $PATH），setup 直接不可用；
	//   ② 即使 setup 侥幸通过，容器内的命令解析用宿主 PATH，xdev 管理的工具
	//      （shim 目录）在容器里根本不存在 → 「隔离环境可用 xdev 工具」从未实现。
	// 容器应使用**镜像自身的** PATH；其它环境变量（HOME、工具专用变量等）照常透传。
	for k, v := range spec.Env {
		if strings.EqualFold(k, "PATH") {
			log.Printf("isolate: not injecting host PATH into the container (F-50); the image's own PATH is used")
			continue
		}
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
	// 第九轮 A-02：本次 Setup 的新句柄即将覆盖旧的持久化会话。若旧会话是损坏的，
	// 它的容器句柄将随覆盖**永久消失**，所以先把损坏文件改名留证（与 Teardown
	// 同一策略），再写新状态。否则用户连 `docker ps -a` 的线索都没有了。
	if c.stateErr != nil {
		log.Printf("isolate setup (container): previous state was corrupt (%v); %s",
			c.stateErr, quarantineState(c.xmdeDir, string(ModeContainer)))
		c.stateErr = nil
	}
	saveState(c.xmdeDir, string(ModeContainer), persistedState{
		ContainerID:  c.containerID,
		Image:        c.image,
		ProjectDir:   c.projectDir,
		WrittenFiles: c.writtenFiles,
	})
	return nil
}

// Teardown stops and removes the container. Per the C-07 review it deletes
// **no files**: `.devcontainer/devcontainer.json` is left in place on purpose
// (users commit it to version control, and the pre-existing content was never
// ours to remove). Setup's existence gate — not a deletion — is what protects
// a user-authored devcontainer.json.
//
// 第九轮 A-02：状态损坏时**不得**静默返回 nil。旧实现开头就是
// `if c.containerID == "" { return nil }`，而损坏的 state 恰好让 containerID
// 为空 —— 于是 CLI 打印 `✔ isolated environment destroyed`，state 里记录的
// 容器却仍在运行（审计现场句柄 `deadbeefcafe`），容器/端口/磁盘永久泄漏。
// 现在先检查 stateErr：损坏文件保留为 <mode>.json.corrupt-<时间戳>（证据不丢，
// 同时清空 statePath 让后续 setup 可推进），并返回带可操作指引的错误。
func (c *containerIsolator) Teardown(ctx context.Context) error {
	if c.stateErr != nil {
		hint := quarantineState(c.xmdeDir, string(ModeContainer))
		return fmt.Errorf("cannot tear down container isolation: %w; %s",
			c.stateErr, corruptStateHint(ModeContainer, hint))
	}
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
	// 第九轮 A-02：状态损坏时不能笼统说「未 setup」——用户明明 setup 过，真实
	// 原因是句柄读不出来，误导性的提示会把人引向重跑 setup（然后覆盖掉线索）。
	// 这里**只读**：不改名损坏文件（quarantine 是 Teardown 的动作）。若 Exec
	// 把坏文件挪走，紧随其后的 teardown 就会「文件不存在 → 静默成功」，容器
	// 又悄悄泄漏一次。
	if c.stateErr != nil {
		return nil, fmt.Errorf("cannot exec in container isolation: %w; %s",
			c.stateErr, corruptStateHint(ModeContainer, ""))
	}
	if c.containerID == "" {
		return nil, fmt.Errorf("container not set up; run 'xdev isolate setup' first")
	}
	if len(cmd) == 0 {
		return nil, fmt.Errorf("empty command")
	}

	// Security (M-6 fix): insert "--" after the container id so a
	// command element starting with "-" is never parsed as a docker
	// exec option (mirrors the N-14 guard on docker run).
	args := append([]string{"exec", "--", c.containerID}, cmd...)
	result, _ := util.RunCommand(ctx, "docker", args...)
	return &result, nil
}

// generateDevcontainer writes a devcontainer.json to the project's
// .devcontainer directory.
func (c *containerIsolator) generateDevcontainer() error {
	dcDir := filepath.Join(c.projectDir, ".devcontainer")
	dcPath := filepath.Join(dcDir, "devcontainer.json")

	// C-07 / P1-34 existence gate: this runs before MkdirAll and before any
	// docker work, so Setup aborts without touching an existing user file
	// unless --force was passed.
	if err := ensureOverwritable(dcPath, c.spec != nil && c.spec.Force); err != nil {
		return err
	}
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
	if err := os.WriteFile(dcPath, data, 0o644); err != nil {
		return err
	}
	// C-07: record what we wrote (see the struct comment: Teardown removes no
	// files, this keeps the persisted session an accurate inventory).
	c.writtenFiles = recordWrittenFile(c.writtenFiles, dcPath, data)
	return nil
}

// containerName generates a deterministic container name from the project dir.
func containerName(projectDir string) string {
	name := filepath.Base(projectDir)
	if name == "" || name == "." {
		name = "xdev"
	}
	return "xdev-" + name
}
