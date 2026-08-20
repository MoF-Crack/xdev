// app_project.go implements project configuration, task running, service
// management, isolation, and remote instance methods for the GUI.
//
// # Isolation & Remote
//
//   - GetIsolateStatus: check environment isolation status
//   - IsolateExec: run a command in an isolated environment
//   - ListRemoteInstances / CreateRemoteInstance: Docker/SSH remote management
package gui

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/xmdemt/xdev/internal/isolator"
	"github.com/xmdemt/xdev/internal/util"
	"github.com/xmdemt/xdev/internal/orchestrator"
	wailsRuntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// ========== Isolate 隔离 ==========

// IsolateStatus 隔离状态
type IsolateStatus struct {
	AvailableModes  []string `json:"availableModes"`
	ActiveMode      string   `json:"activeMode"`
	ContainerID     string   `json:"containerId"`
	DockerAvailable bool     `json:"dockerAvailable"`
}

// GetIsolateStatus 获取隔离状态
func (a *App) GetIsolateStatus() *IsolateStatus {
	a.isoMu.Lock()
	defer a.isoMu.Unlock()
	activeMode := "none"
	if a.isoInstance != nil {
		activeMode = string(a.isoMode)
	}
	status := &IsolateStatus{
		AvailableModes:  []string{"none", "container", "nix"},
		ActiveMode:      activeMode,
		DockerAvailable: false,
	}
	if _, err := exec.LookPath("docker"); err == nil {
		status.DockerAvailable = true
	}
	return status
}

// IsolateSetup 设置隔离环境
func (a *App) IsolateSetup(mode string) (string, error) {
	iso, err := isolator.NewIsolator(isolator.IsolationMode(mode))
	if err != nil {
		return "", err
	}
	spec := &isolator.EnvironmentSpec{
		Tools: make(map[string]string),
		Env:   make(map[string]string),
	}
	if err := iso.Setup(context.Background(), spec); err != nil {
		return "", err
	}
	a.isoMu.Lock()
	a.isoMode = isolator.IsolationMode(mode)
	a.isoInstance = iso
	a.isoMu.Unlock()
	return fmt.Sprintf("Isolation environment set up with mode: %s", mode), nil
}

// IsolateExec 在隔离环境中执行命令。
// 命令必须通过 allowlist：仅允许已安装工具的 shim 名或常见只读探测命令
// （node/python/go 等 --version），拒绝任意 shell / 解释器入口。
func (a *App) IsolateExec(cmd []string) (string, error) {
	if err := validateIsolateCommand(cmd); err != nil {
		return "", err
	}
	a.isoMu.Lock()
	iso := a.isoInstance
	a.isoMu.Unlock()
	if iso == nil {
		return "", fmt.Errorf("no isolation environment active; run IsolateSetup first")
	}
	result, err := iso.Exec(context.Background(), cmd)
	if err != nil {
		return "", err
	}
	output := result.Stdout
	if result.Stderr != "" {
		output += "\n" + result.Stderr
	}
	return output, nil
}

// IsolateTeardown 销毁隔离环境
func (a *App) IsolateTeardown() error {
	a.isoMu.Lock()
	iso := a.isoInstance
	a.isoInstance = nil
	a.isoMode = isolator.ModeNone
	a.isoMu.Unlock()
	if iso == nil {
		return nil
	}
	return iso.Teardown(context.Background())
}

// ========== Remote 远程开发 ==========

// RemoteInstanceInfo 远程环境实例信息
type RemoteInstanceInfo struct {
	ID        string   `json:"id"`
	Status    string   `json:"status"`
	Addresses []string `json:"addresses"`
	Adapter   string   `json:"adapter"`
}

// RemoteCreateResult 创建远程环境的结果
type RemoteCreateResult struct {
	ID      string `json:"id"`
	Message string `json:"message"`
}

// ListRemoteInstances 列出远程环境实例
func (a *App) ListRemoteInstances() []RemoteInstanceInfo {
	var out []RemoteInstanceInfo
	// List Docker containers
	instances, err := orchestrator.ListContainers(context.Background())
	if err == nil {
		for _, inst := range instances {
			out = append(out, RemoteInstanceInfo{
				ID:        inst.ID,
				Status:    inst.Status,
				Addresses: inst.Addresses,
				Adapter:   "docker",
			})
		}
	}
	// Also list SSH instances from marker files
	cfg := a.getCfg()
	instancesDir := filepath.Join(cfg.Core.XMDevDir, "instances")
	if entries, err := os.ReadDir(instancesDir); err == nil {
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
				continue
			}
			data, err := os.ReadFile(filepath.Join(instancesDir, e.Name()))
			if err != nil {
				continue
			}
			var inst orchestrator.Instance
			if err := json.Unmarshal(data, &inst); err != nil {
				continue
			}
			adapterType := inst.Metadata["adapter"]
			if adapterType == "" {
				adapterType = "ssh"
			}
			// Skip if already added from Docker listing
			alreadyListed := false
			for _, existing := range out {
				if existing.ID == inst.ID {
					alreadyListed = true
					break
				}
			}
			if !alreadyListed {
				out = append(out, RemoteInstanceInfo{
					ID:        inst.ID,
					Status:    inst.Status,
					Addresses: inst.Addresses,
					Adapter:   adapterType,
				})
			}
		}
	}
	if out == nil {
		out = []RemoteInstanceInfo{}
	}
	return out
}

// CreateRemoteInstance 创建远程开发环境
func (a *App) CreateRemoteInstance(adapter, image, sshHost string, sshPort int, sshUser, sshKeyPath, sshPassword string) (*RemoteCreateResult, error) {
	adapterImpl, err := orchestrator.NewAdapter(orchestrator.AdapterType(adapter))
	if err != nil {
		return nil, err
	}
	spec := &orchestrator.EnvironmentSpec{
		Image: image,
		Tools: make(map[string]string),
		Env:   make(map[string]string),
	}
	// For SSH adapter, populate SSH target
	if adapter == "ssh" {
		spec.SSHTarget = orchestrator.SSHTarget{
			Host:     sshHost,
			Port:     sshPort,
			User:     sshUser,
			KeyPath:  sshKeyPath,
			Password: sshPassword,
		}
		if spec.SSHTarget.User == "" {
			spec.SSHTarget.User = "root"
		}
		if spec.SSHTarget.Port == 0 {
			spec.SSHTarget.Port = 22
		}
	}
	inst, err := adapterImpl.Create(context.Background(), spec)
	if err != nil {
		return nil, err
	}
	return &RemoteCreateResult{
		ID:      inst.ID,
		Message: fmt.Sprintf("Remote environment created: %s (%s)", inst.ID, adapter),
	}, nil
}

// SSHTestResult 远程连接测试结果
type SSHTestResult struct {
	Success   bool   `json:"success"`
	Message   string `json:"message"`
	LatencyMs int64  `json:"latencyMs"`
}

// TestSSHConnection 测试 SSH 连接是否可用。
func (a *App) TestSSHConnection(host string, port int, user, keyPath, password string) *SSHTestResult {
	if host == "" {
		return &SSHTestResult{Success: false, Message: "host is required"}
	}
	if port == 0 {
		port = 22
	}
	if user == "" {
		user = "root"
	}
	target := orchestrator.SSHTarget{
		Host:     host,
		Port:     port,
		User:     user,
		KeyPath:  keyPath,
		Password: password,
	}
	adapter, err := orchestrator.NewSSHAdapter(target)
	if err != nil {
		return &SSHTestResult{Success: false, Message: err.Error()}
	}
	start := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := adapter.TestConnection(ctx); err != nil {
		return &SSHTestResult{Success: false, Message: err.Error(), LatencyMs: time.Since(start).Milliseconds()}
	}
	return &SSHTestResult{Success: true, Message: "SSH connection OK", LatencyMs: time.Since(start).Milliseconds()}
}

// BrowseFile 打开文件选择对话框，返回用户选择的文件路径（取消返回空字符串）。
func (a *App) BrowseFile(title, filters string) string {
	if a.ctx == nil {
		return ""
	}
	opts := wailsRuntime.OpenDialogOptions{
		Title: title,
	}
	if filters != "" {
		opts.Filters = []wailsRuntime.FileFilter{
			{DisplayName: "Key Files", Pattern: filters},
		}
	}
	path, err := wailsRuntime.OpenFileDialog(a.ctx, opts)
	if err != nil {
		logWarn("[BrowseFile] file dialog failed: %v", err)
		return ""
	}
	return path
}

// DestroyRemoteInstance 销毁远程环境
func (a *App) DestroyRemoteInstance(adapter, id string) error {
	adapterImpl, err := orchestrator.NewAdapter(orchestrator.AdapterType(adapter))
	if err != nil {
		return err
	}
	return adapterImpl.Destroy(context.Background(), id)
}

// ExecRemoteInstance 在远程环境中执行命令（与 IsolateExec 共用命令白名单）。
func (a *App) ExecRemoteInstance(adapter, id string, cmd []string) (string, error) {
	if err := validateIsolateCommand(cmd); err != nil {
		return "", err
	}
	adapterImpl, err := orchestrator.NewAdapter(orchestrator.AdapterType(adapter))
	if err != nil {
		return "", err
	}
	result, err := adapterImpl.Exec(context.Background(), id, cmd)
	if err != nil {
		return "", err
	}
	output := result.Stdout
	if result.Stderr != "" {
		output += "\n" + result.Stderr
	}
	return output, nil
}

// ========== cd hook ==========

// ActivateHook 激活 cd hook。shell 必须是已知 shell 名，防止任意参数注入。
func (a *App) ActivateHook(shell string) error {
	switch strings.ToLower(strings.TrimSpace(shell)) {
	case "powershell", "pwsh", "cmd", "bash", "zsh", "fish", "nu":
	default:
		return fmt.Errorf("unsupported shell %q", shell)
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	cmd := exec.Command(exe, "activate", shell)
	util.HideWindow(cmd)
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// DeactivateHook 停用 cd hook
func (a *App) DeactivateHook() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	cmd := exec.Command(exe, "deactivate")
	util.HideWindow(cmd)
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// isolateDeniedBins are interpreters / shells that must never be invoked
// via IsolateExec / ExecRemoteInstance, even if they appear as a shim.
var isolateDeniedBins = map[string]struct{}{
	"cmd": {}, "cmd.exe": {}, "powershell": {}, "powershell.exe": {},
	"pwsh": {}, "pwsh.exe": {}, "bash": {}, "sh": {}, "zsh": {},
	"fish": {}, "csh": {}, "tcsh": {}, "ksh": {},
	"wsl": {}, "wsl.exe": {}, "cmd.com": {},
	"python": {}, "python.exe": {}, "python3": {}, "pythonw.exe": {},
	"py": {}, "py.exe": {}, "ruby": {}, "ruby.exe": {},
	"perl": {}, "perl.exe": {}, "php": {}, "php.exe": {},
	"lua": {}, "lua.exe": {}, "node": {}, "node.exe": {},
	"deno": {}, "deno.exe": {}, "bun": {}, "bun.exe": {},
	"osascript": {}, "osascript.exe": {},
}

// isolateSafeVersionBins are well-known toolchain binaries that may be
// invoked only with a single --version / -version / version argument.
var isolateSafeVersionBins = map[string]struct{}{
	"node": {}, "node.exe": {}, "python": {}, "python.exe": {},
	"python3": {}, "go": {}, "go.exe": {}, "java": {}, "java.exe": {},
	"rustc": {}, "rustc.exe": {}, "cargo": {}, "cargo.exe": {},
	"dotnet": {}, "dotnet.exe": {}, "php": {}, "php.exe": {},
	"ruby": {}, "ruby.exe": {}, "kotlin": {}, "kotlinc": {},
	"scala": {}, "swift": {}, "dart": {}, "flutter": {},
	"gcc": {}, "g++": {}, "clang": {}, "clang++": {},
	"cmake": {}, "cmake.exe": {}, "make": {}, "ninja": {},
	"git": {}, "git.exe": {}, "mvn": {}, "mvn.exe": {},
	"gradle": {}, "gradle.exe": {},
}

var isolateSafeVersionArgs = map[string]struct{}{
	"--version": {}, "-version": {}, "version": {}, "-v": {},
	"--help": {}, "-help": {}, "help": {}, "-h": {},
}

func isolateCommandBase(name string) string {
	return strings.ToLower(filepath.Base(strings.TrimSpace(name)))
}

func isolateArgLooksUnsafe(arg string) bool {
	if arg == "" {
		return true
	}
	if strings.ContainsAny(arg, ";&|`$<>\n\r") {
		return true
	}
	if strings.Contains(arg, "..") {
		return true
	}
	return false
}

// validateIsolateCommand rejects empty commands, path-qualified binaries,
// shell metacharacters, and any interpreter/shell that is not a registered
// xdev shim (except a small --version allowlist used by the GUI probe).
func validateIsolateCommand(cmd []string) error {
	if len(cmd) == 0 {
		return fmt.Errorf("empty command")
	}
	name := strings.TrimSpace(cmd[0])
	if name == "" {
		return fmt.Errorf("empty command")
	}
	if strings.ContainsAny(name, `/\`) || filepath.IsAbs(name) || filepath.VolumeName(name) != "" {
		return fmt.Errorf("command must be a bare executable name, not a path")
	}
	base := isolateCommandBase(name)
	if base == "" || base == "." || base == ".." {
		return fmt.Errorf("invalid command name")
	}
	for _, arg := range cmd[1:] {
		if isolateArgLooksUnsafe(arg) {
			return fmt.Errorf("command argument contains unsafe characters")
		}
	}

	for _, arg := range cmd[1:] {
		if _, ok := isolateSafeVersionArgs[arg]; !ok {
			return fmt.Errorf("command argument %q is not allowed (only version/help probes)", arg)
		}
	}

	if _, denied := isolateDeniedBins[base]; denied {
		if len(cmd) == 2 {
			if _, ok := isolateSafeVersionArgs[cmd[1]]; ok {
				if _, allowed := isolateSafeVersionBins[base]; allowed {
					return nil
				}
			}
		}
		return fmt.Errorf("command %q is not allowed in isolate/remote exec", base)
	}

	// Common toolchain probes (g++/clang++ etc.) may contain characters
	// that SafeName rejects; they are still limited to version/help args.
	if _, ok := isolateSafeVersionBins[base]; ok {
		return nil
	}

	// Allow registered xdev shims as a bare name. Extra arguments are
	// already restricted to version/help.
	if err := util.ValidateShimName(base); err != nil {
		return fmt.Errorf("command %q is not a valid tool name: %w", base, err)
	}
	return nil
}
