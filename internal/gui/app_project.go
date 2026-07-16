// app_project.go implements project configuration, task running, service
// management, plugin listing, AI assistant, isolation, and remote instance
// methods for the GUI.
//
// # Project Config
//
//   - GetProjectConfig: read .xdev.toml from a project directory
//   - SetProjectTool: add/update a tool version in .xdev.toml
//   - GetLockfile: read the .xdev.lock for a project
//
// # Tasks
//
//   - RunProjectTask: execute a named task from .xdev.toml [tasks] section
//
// # Services
//
//   - ListServices: list available development services
//   - StartService / StopService: service lifecycle management
//
// # Plugins & AI
//
//   - ListPlugins: list installed Lua plugins
//   - GetAIStatus: check Ollama connectivity
//   - AISuggest: get AI-recommended tool versions
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

	"github.com/xmdemt/xdev/internal/ai"
	"github.com/xmdemt/xdev/internal/installer"
	"github.com/xmdemt/xdev/internal/isolator"
	"github.com/xmdemt/xdev/internal/lockfile"
	"github.com/xmdemt/xdev/internal/orchestrator"
	"github.com/xmdemt/xdev/internal/plugin"
	"github.com/xmdemt/xdev/internal/project"
	"github.com/xmdemt/xdev/internal/service"
	"github.com/xmdemt/xdev/internal/task"
)

// ========== 项目配置 ==========

// ProjectConfigInfo 前端展示用
type ProjectConfigInfo struct {
	Dir       string              `json:"dir"`
	Tools     map[string]string   `json:"tools"`
	Env       map[string]string   `json:"env"`
	Tasks     map[string]TaskInfo `json:"tasks"`
	HasConfig bool                `json:"hasConfig"`
}

// TaskInfo 任务信息
type TaskInfo struct {
	Run  string `json:"run"`
	Desc string `json:"desc"`
	Dir  string `json:"dir"`
}

// GetProjectConfig 获取当前目录的项目配置
func (a *App) GetProjectConfig() *ProjectConfigInfo {
	dir, cfg := project.FindConfig(".")
	if cfg == nil {
		return &ProjectConfigInfo{HasConfig: false}
	}
	info := &ProjectConfigInfo{
		Dir:       dir,
		Tools:     cfg.Tools,
		Env:       cfg.Env,
		HasConfig: true,
	}
	info.Tasks = make(map[string]TaskInfo)
	for name, t := range cfg.Tasks {
		info.Tasks[name] = TaskInfo{Run: t.Run, Desc: t.Desc, Dir: t.Dir}
	}
	return info
}

// SetProjectTool 设置项目级工具版本
func (a *App) SetProjectTool(tool, version string) error {
	dir, err := os.Getwd()
	if err != nil {
		return err
	}
	return project.SetTool(dir, tool, version)
}

// UnsetProjectTool 移除项目级工具版本
func (a *App) UnsetProjectTool(tool string) error {
	dir, err := os.Getwd()
	if err != nil {
		return err
	}
	return project.UnsetTool(dir, tool)
}

// ========== 任务运行器 ==========

// RunProjectTask 运行项目任务
func (a *App) RunProjectTask(taskName string) error {
	projDir, proj := project.FindConfig(".")
	if proj == nil {
		return fmt.Errorf("no .xdev.toml found")
	}
	runner := task.NewRunner(proj, projDir)
	return runner.Run(taskName)
}

// ========== 插件管理 ==========

// PluginInfo 插件信息
type PluginInfo struct {
	Name        string `json:"name"`
	DisplayName string `json:"displayName"`
	Category    string `json:"category"`
	Path        string `json:"path"`
	IsLua       bool   `json:"isLua"`
	Type        string `json:"type"`
}

// ListPlugins 列出所有插件（内置 + Lua）
func (a *App) ListPlugins() []PluginInfo {
	var result []PluginInfo
	registered := make(map[string]bool)
	// 内置插件
	if a.registry != nil {
		for _, p := range a.registry.All() {
			registered[p.Name()] = true
			pluginType := "builtin"
			switch p.(type) {
			case *plugin.VfoxPlugin:
				pluginType = "vfox"
			case *plugin.LuaPlugin:
				pluginType = "lua"
			}
			result = append(result, PluginInfo{
				Name:        p.Name(),
				DisplayName: p.DisplayName(),
				Category:    p.Category(),
				IsLua:       pluginType != "builtin",
				Type:        pluginType,
			})
		}
	}
	// Lua 插件
	cfg := a.getCfg()
	if cfg != nil {
		pluginDir := filepath.Join(cfg.Core.XMDevDir, "plugins")
		entries, err := os.ReadDir(pluginDir)
		if err == nil {
			for _, e := range entries {
				if !e.IsDir() && strings.HasSuffix(e.Name(), ".lua") {
					name := strings.TrimSuffix(e.Name(), ".lua")
					if registered[name] {
						continue
					}
					result = append(result, PluginInfo{
						Name:  name,
						Path:  filepath.Join(pluginDir, e.Name()),
						IsLua: true,
						Type:  "lua",
					})
				}
			}
		}
	}
	return result
}

// CreatePlugin 创建 Lua 插件模板
func (a *App) CreatePlugin(name string) error {
	cfg := a.getCfg()
	if cfg == nil {
		return fmt.Errorf("config not loaded")
	}
	pluginDir := filepath.Join(cfg.Core.XMDevDir, "plugins")
	os.MkdirAll(pluginDir, 0o755)
	path := filepath.Join(pluginDir, name+".lua")
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("plugin file already exists: %s", path)
	}
	template := fmt.Sprintf(`-- XDev Lua Plugin: %s
PLUGIN = {
    name = "%s",
    display_name = "%s",
    category = "build",
    shims = {"%s"},

    -- 列出可用版本（必须实现）
    available = function()
        return {"1.0.0"}
    end,

    -- 返回下载信息（必须实现）
    archive = function(version, os, arch)
        return {
            url = "https://example.com/%s/" .. version .. "/%s-" .. os .. "-" .. arch .. ".tar.gz",
            format = "tar.gz",
            strip = 1
        }
    end,

    -- 返回环境变量（可选）
    env_vars = function(install_path)
        return {}
    end
}
`, name, name, name, name, name, name)
	return os.WriteFile(path, []byte(template), 0o644)
}

// ========== 服务编排 ==========

// ServiceInfo 服务信息
type ServiceInfo struct {
	Name    string `json:"name"`
	Type    string `json:"type"`
	Version string `json:"version"`
	Status  string `json:"status"`
	Port    int    `json:"port"`
}

// ListServices 列出可用服务
func (a *App) ListServices() []ServiceInfo {
	cfg := a.getCfg()
	if cfg == nil {
		return nil
	}
	mgr := service.NewManager(cfg)
	svcs := mgr.List()
	var result []ServiceInfo
	for _, s := range svcs {
		result = append(result, ServiceInfo{
			Name:    s.Name,
			Type:    s.Type,
			Version: s.Version,
			Status:  s.Status,
			Port:    s.Port,
		})
	}
	return result
}

// StartService 启动服务
func (a *App) StartService(name string) error {
	cfg := a.getCfg()
	if cfg == nil {
		return fmt.Errorf("config not loaded")
	}
	mgr := service.NewManager(cfg)
	return mgr.Start(name)
}

// StopService 停止服务
func (a *App) StopService(name string) error {
	cfg := a.getCfg()
	if cfg == nil {
		return fmt.Errorf("config not loaded")
	}
	mgr := service.NewManager(cfg)
	return mgr.Stop(name)
}

// ========== AI 助手 ==========

// AIStatus AI 服务状态
type AIStatus struct {
	Available bool   `json:"available"`
	URL       string `json:"url"`
	Model     string `json:"model"`
}

// GetAIStatus 获取 AI 服务状态
func (a *App) GetAIStatus() *AIStatus {
	cfg := a.getCfg()
	assistant := ai.NewAssistant(&ai.AIConfig{OllamaURL: cfg.AI.OllamaURL, Model: cfg.AI.Model, Timeout: cfg.AI.Timeout})
	return &AIStatus{
		Available: assistant.IsAvailable(),
		URL:       assistant.GetURL(),
		Model:     assistant.GetModel(),
	}
}

// AISuggest AI 推荐版本
func (a *App) AISuggest(tool string) string {
	cfg := a.getCfg()
	assistant := ai.NewAssistant(&ai.AIConfig{OllamaURL: cfg.AI.OllamaURL, Model: cfg.AI.Model, Timeout: cfg.AI.Timeout})
	// Get current active version from registry
	currentVer := ""
	if a.registry != nil {
		if _, ok := a.registry.Get(tool); ok {
			if ver, hasActive := a.currentVersion(tool); hasActive {
				currentVer = ver
			}
		}
	}
	suggestions, err := assistant.SuggestVersions(tool, currentVer)
	if err != nil {
		return fmt.Sprintf("AI suggestion failed: %v", err)
	}
	return strings.Join(suggestions, ", ")
}

// AIDiagnose AI 诊断
func (a *App) AIDiagnose(problem string) string {
	cfg := a.getCfg()
	assistant := ai.NewAssistant(&ai.AIConfig{OllamaURL: cfg.AI.OllamaURL, Model: cfg.AI.Model, Timeout: cfg.AI.Timeout})
	if problem == "" {
		problem = "xdev environment diagnosis"
	}
	result, err := assistant.DiagnoseIssue(problem)
	if err != nil {
		return fmt.Sprintf("AI diagnosis failed: %v", err)
	}
	return result
}

// AIChat AI 单轮问答
func (a *App) AIChat(message string) string {
	cfg := a.getCfg()
	assistant := ai.NewAssistant(&ai.AIConfig{OllamaURL: cfg.AI.OllamaURL, Model: cfg.AI.Model, Timeout: cfg.AI.Timeout})
	result, err := assistant.Generate(message)
	if err != nil {
		return fmt.Sprintf("AI chat failed: %v", err)
	}
	return result
}

// ========== Lockfile ==========

// LockfileToolInfo lockfile 中单个工具的信息
type LockfileToolInfo struct {
	Version     string `json:"version"`
	Resolved    string `json:"resolved"`
	SHA256      string `json:"sha256"`
	URL         string `json:"url"`
	InstallPath string `json:"installPath"`
}

// LockfileInfo lockfile 信息
type LockfileInfo struct {
	Dir   string                      `json:"dir"`
	Found bool                        `json:"found"`
	Tools map[string]LockfileToolInfo `json:"tools"`
}

// GetLockfile 获取当前目录的 lockfile
func (a *App) GetLockfile() *LockfileInfo {
	dir := lockfile.FindConfigDir(".")
	if dir == "" {
		return &LockfileInfo{Found: false}
	}
	lf, err := lockfile.Load(dir)
	if err != nil || lf == nil {
		return &LockfileInfo{Dir: dir, Found: false}
	}
	info := &LockfileInfo{
		Dir:   dir,
		Found: true,
		Tools: make(map[string]LockfileToolInfo),
	}
	for name, t := range lf.Tools {
		info.Tools[name] = LockfileToolInfo{
			Version:     t.Version,
			Resolved:    t.Resolved,
			SHA256:      t.SHA256,
			URL:         t.URL,
			InstallPath: t.InstallPath,
		}
	}
	return info
}

// InstallFromLockfile 从 lockfile 安装所有工具
func (a *App) InstallFromLockfile() (string, error) {
	dir := lockfile.FindConfigDir(".")
	if dir == "" {
		return "", fmt.Errorf("no .xdev.lock found")
	}
	lf, err := lockfile.Load(dir)
	if err != nil || lf == nil || len(lf.Tools) == 0 {
		return "", fmt.Errorf("no locked tools found")
	}
	var results []string
	cfg := a.getCfg()
	ins := a.newInstaller(cfg)

	var items []installer.BatchInstallItem
	for toolName, locked := range lf.Tools {
		items = append(items, installer.BatchInstallItem{
			ToolName: toolName,
			Version:  locked.Resolved,
		})
	}

	batchResults := ins.InstallMany(context.Background(), items)
	for _, r := range batchResults {
		switch r.Status {
		case "unsupported":
			results = append(results, fmt.Sprintf("⚠ %s: unsupported tool", r.Tool))
		case "already_installed":
			results = append(results, fmt.Sprintf("✔ %s@%s: already installed", r.Tool, r.Version))
		case "installed":
			results = append(results, fmt.Sprintf("✔ %s@%s installed to %s", r.Tool, r.Version, r.InstallPath))
		case "failed":
			results = append(results, fmt.Sprintf("⚠ %s@%s: %v", r.Tool, r.Version, r.Error))
		}
	}
	return strings.Join(results, "\n"), nil
}

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

// IsolateExec 在隔离环境中执行命令
func (a *App) IsolateExec(cmd []string) (string, error) {
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
func (a *App) CreateRemoteInstance(adapter, image, sshHost string, sshPort int, sshUser, sshKeyPath string) (*RemoteCreateResult, error) {
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
			Host:    sshHost,
			Port:    sshPort,
			User:    sshUser,
			KeyPath: sshKeyPath,
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

// DestroyRemoteInstance 销毁远程环境
func (a *App) DestroyRemoteInstance(adapter, id string) error {
	adapterImpl, err := orchestrator.NewAdapter(orchestrator.AdapterType(adapter))
	if err != nil {
		return err
	}
	return adapterImpl.Destroy(context.Background(), id)
}

// ExecRemoteInstance 在远程环境中执行命令
func (a *App) ExecRemoteInstance(adapter, id string, cmd []string) (string, error) {
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

// ActivateHook 激活 cd hook
func (a *App) ActivateHook(shell string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	cmd := exec.Command(exe, "activate", shell)
	cmd.Stdout = os.Stdout
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
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
