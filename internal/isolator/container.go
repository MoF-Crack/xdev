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
// 丢弃——零值的 persistedState 会被调用方解读成「没有容器」，正是容器静默泄漏
// 的根因。注意这里不清除、不改名损坏文件：quarantine 交给 Teardown
// （唯一真正消费该状态的路径）做，NewIsolator/Exec 只读不写。
//
// 第十轮 P2-3：**不再把错误缓存在结构体上**。修复前 loadSaved 把 stateErr 存进
// 字段，之后 Setup/Teardown/Exec 一直拿着这个构造时的判断，于是「构造时损坏、
// 之后被修复」的状态仍会被当成损坏（审计现场 container 侧把**有效文件**
// quarantine 并报错，而 nix 侧每次重新 loadState 因而正常成功 —— 两侧不对称）。
// 现在 loadSaved 只填充句柄，所有判断都由各操作在**当下**重新 loadState 得出。
func (c *containerIsolator) loadSaved() {
	s, err := loadState(c.xmdeDir, string(ModeContainer))
	if err != nil {
		// 损坏状态不在这里处理：不填充任何句柄，把「读不出来」留给真正消费
		// 状态的路径（Setup/Teardown/Exec）在当下重新判定。
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
	//
	// 第十轮 P2-3：这里的判断必须在**当下**重新 loadState，而不是用构造时的缓存
	// （见 loadSaved 的注释）：旧会话可能在构造之后被修复或再次损坏。
	if _, stateErr := loadState(c.xmdeDir, string(ModeContainer)); stateErr != nil {
		log.Printf("isolate setup (container): previous state was corrupt (%v); %s",
			stateErr, quarantineState(c.xmdeDir, string(ModeContainer)))
	}
	// 第十轮 P2-2：state 落盘失败必须**报错并回滚**。修复前 saveState 无返回值，
	// 这里静默通过：docker run 已经真的把容器启动了，磁盘上却没有它的句柄，下一次
	// Teardown 读到空状态 → `containerID == ""` → return nil → CLI 打印
	// `✔ isolated environment destroyed`，而容器/端口/磁盘永久泄漏。
	if err := saveState(c.xmdeDir, string(ModeContainer), persistedState{
		ContainerID:  c.containerID,
		Image:        c.image,
		ProjectDir:   c.projectDir,
		WrittenFiles: c.writtenFiles,
	}); err != nil {
		return c.rollbackFailedSetup(ctx, err)
	}
	return nil
}

// rollbackFailedSetup 在 state 无法落盘时尽力回收本次 Setup 刚启动的容器，
// 并返回一个说明「两件事」的错误：state 没落盘 + 回滚是否成功。
//
// 回滚失败的提示必须给出可执行的补救命令：容器句柄此刻只存在于错误信息里，
// 用户必须能看到它才能 `docker rm -f`。devcontainer.json 不删（C-07：它可能
// 是用户提交进版本库的文件，Setup 的存在性门已保证不覆盖用户文件）。
func (c *containerIsolator) rollbackFailedSetup(ctx context.Context, cause error) error {
	containerID := c.containerID
	stderr, rmErr := removeContainer(ctx, containerID)
	if rmErr != nil {
		if dockerMissingContainer(stderr) {
			// 容器已经不在了（极少见：并发 teardown 抢先一步）——回滚目的已达成。
			log.Printf("isolate setup (container): container %s was already gone while rolling back: %v", containerID, rmErr)
		} else {
			return fmt.Errorf("container isolation setup could not persist its state (%w); "+
				"rolling back the container also failed (%v%s) — the container is still running: "+
				"remove it manually with `docker rm -f %s`", cause, rmErr, dockerStderrSuffix(stderr), containerID)
		}
	}
	c.containerID = ""
	return fmt.Errorf("container isolation setup could not persist its state, so the container was rolled back "+
		"(removed) to avoid a permanent leak: %w", cause)
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
// 损坏文件保留为 <mode>.json.corrupt-<时间戳>（证据不丢，同时清空 statePath 让
// 后续 setup 可推进），并返回带可操作指引的错误。
//
// 第十轮 P2-3：**每次 Teardown 重新读 state**，不再使用构造时（loadSaved）的
// 快照，也不再缓存 stateErr。两个后果都被修掉：
//
//   - 并发：本进程构造 isolator（读到 old-container）→ 另一个进程 setup 写入
//     `{"containerId":"new-container"}` → 本进程旧实现删 old 后**无条件**
//     clearState，把 new 的 state 一并删除 → new 容器失管。现在重新读取，
//     且只在「持久化 ID == 本次删除的 ID」时才 clearState。
//   - 修复后状态：state 在构造之后被修复为有效时，旧实现仍按缓存的 stateErr
//     把**有效文件** quarantine 并报错（审计现场 `quarantine_files=1`），而
//     nix 侧每次重新 loadState 因而正常成功 —— 两侧行为不对称。现在对称。
//
// 快照只作为回退：state 文件不存在（首次运行/已被清掉）时才用内存里的句柄。
func (c *containerIsolator) Teardown(ctx context.Context) error {
	// 先看磁盘上的**当前**状态。损坏文件是泄漏容器的唯一线索：quarantine 保留
	// 证据 + 返回可操作错误（A-02），绝不静默成功。
	state, stateErr := loadState(c.xmdeDir, string(ModeContainer))
	if stateErr != nil {
		hint := quarantineState(c.xmdeDir, string(ModeContainer))
		return fmt.Errorf("cannot tear down container isolation: %w; %s",
			stateErr, corruptStateHint(ModeContainer, hint))
	}

	containerID := c.containerID
	if containerID == "" {
		// 快照里没有句柄（构造时 state 损坏、之后被修复成有效内容）：采用磁盘上
		// **当前**的状态。这正是审计指出的不对称点——nix 侧每次重新 loadState，
		// container 侧旧实现却抱着构造时的 stateErr 把有效文件 quarantine 掉。
		containerID = state.ContainerID
	}
	if containerID == "" {
		return nil
	}

	// Stop and remove the container
	stderr, err := removeContainer(ctx, containerID)
	if err != nil {
		// 第十轮 P3-2：容器已被用户手工 `docker rm` 掉时 docker 返回非零
		//（"Error response from daemon: No such container: <id>"）。语义上环境
		// 已经销毁，旧实现把任何非零都当失败 → Teardown 永远失败、state 永不
		// 清除（审计现场 `state_kept=true`），每次 teardown 都报错。这里视为成功。
		// 其它失败（权限、daemon 挂了）照旧报错并**保留 state**：句柄不能丢，
		// 否则容器真的泄漏。
		if !dockerMissingContainer(stderr) {
			return fmt.Errorf("docker rm: %w%s", err, dockerStderrSuffix(stderr))
		}
		log.Printf("isolate teardown (container): container %s no longer exists (already removed); treating teardown as successful", containerID)
	}
	c.containerID = ""
	// 第十轮 P2-3：只清除**仍然属于本次会话**的 state。guard 读的是磁盘上此刻
	// 的内容：若另一个进程在这中间 setup 了新会话，它的句柄必须原样保留。
	clearState(c.xmdeDir, string(ModeContainer), func(s persistedState) bool {
		return s.ContainerID == containerID
	})
	return nil
}

// removeContainer 执行 `docker rm -f <id>`，并返回 CLI 的 stderr 文本。
//
// 必须显式捕获 stderr：exec.Cmd 在 Stderr 为 nil 时把子进程的标准错误接到
// os.DevNull，ExitError.Stderr 因此为空（它只在 Output/CombinedOutput 路径被
// 填充），拿不到 docker 的错误文本就无法区分「容器已不存在」与「daemon 挂了」。
// 顺带把这段文本带进错误信息，比裸的 "exit status 1" 有用得多。
func removeContainer(ctx context.Context, containerID string) (string, error) {
	cmd := exec.CommandContext(ctx, "docker", "rm", "-f", containerID)
	util.HideWindow(cmd)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stderr.String(), err
}

// dockerMissingContainer 判断 `docker rm` 的失败是不是「容器本来就不存在」。
//
// docker CLI 把这个 daemon 错误打印在 stderr 上并返回退出码 1（Windows / Linux
// 一致，措辞大小写可能随版本变化），所以按子串、大小写不敏感匹配。刻意不做
// 「所有 rm 失败都当成功」的宽松处理：permission denied / cannot connect to the
// Docker daemon 这类真实故障必须继续报错并保留 state，否则容器真的泄漏。
func dockerMissingContainer(stderr string) bool {
	return strings.Contains(strings.ToLower(stderr), "no such container")
}

// dockerStderrSuffix 把捕获到的 docker stderr 附到错误信息尾部（空则不加）。
func dockerStderrSuffix(stderr string) string {
	if msg := strings.TrimSpace(stderr); msg != "" {
		return ": " + msg
	}
	return ""
}

func (c *containerIsolator) Exec(ctx context.Context, cmd []string) (*ExecResult, error) {
	// 第九轮 A-02：状态损坏时不能笼统说「未 setup」——用户明明 setup 过，真实
	// 原因是句柄读不出来，误导性的提示会把人引向重跑 setup（然后覆盖掉线索）。
	// 这里**只读**：不改名损坏文件（quarantine 是 Teardown 的动作）。若 Exec
	// 把坏文件挪走，紧随其后的 teardown 就会「文件不存在 → 静默成功」，容器
	// 又悄悄泄漏一次。
	//
	// 第十轮 P2-3：与 Teardown 一样在**当下**重新 loadState（不再用构造时的
	// stateErr 缓存），所以「构造时损坏、之后被修复」的 state 现在能正常 exec。
	if _, stateErr := loadState(c.xmdeDir, string(ModeContainer)); stateErr != nil {
		return nil, fmt.Errorf("cannot exec in container isolation: %w; %s",
			stateErr, corruptStateHint(ModeContainer, ""))
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
