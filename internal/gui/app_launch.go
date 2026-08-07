// app_launch.go implements GUI tool launch and CLI command query APIs
// (Spec 023 T-C1 / F8):
//
//   - LaunchTool: 启动已安装工具的 GUI 主二进制（form_factor 含 gui 时可用）。
//     解析 current/<tool> 指向的安装目录，定位 GUIBinary 并以该目录为工作
//     目录异步启动（cmd.Start + Process.Release——GUI 进程在 xdev 退出后
//     继续运行，且不隐藏窗口）。
//   - ToolCommand: 返回工具的 CLI 启动命令（shims[0]），供前端「复制命令」
//     按钮使用；未安装或无 shims 时返回空串。
package gui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/xmdemt/xdev/internal/util"
)

// LaunchTool 启动已安装工具的 GUI 主二进制。
//
// 失败场景（中文错误）：
//   - 工具未安装（current/<tool> 不存在）
//   - 无 manifest / 无 gui 形态 / 未声明 gui_binary / gui_binary 非法或缺失
func (a *App) LaunchTool(toolName string) error {
	p, err := a.mustGetPlugin(toolName)
	if err != nil {
		return err
	}

	// 1. 解析激活版本的安装路径（junction/symlink 目标）。
	installPath := util.ActiveInstallPath(a.getCfg().Core.XMDevDir, toolName)
	if installPath == "" {
		return fmt.Errorf("工具 %s 未安装（current/%s 不存在）", toolName, toolName)
	}

	// 2. manifest（nil 安全）：必须含 gui 形态且声明 gui_binary。
	m := p.Manifest()
	if m == nil {
		return fmt.Errorf("工具 %s 无 manifest，无法确定 GUI 入口", toolName)
	}
	hasGUI := false
	for _, f := range m.EffectiveFormFactor() {
		if f == "gui" {
			hasGUI = true
			break
		}
	}
	if !hasGUI {
		return fmt.Errorf("工具 %s 无 gui 形态，无法启动", toolName)
	}
	guiBinary, err := safeLaunchBinary(m.GUIBinary)
	if err != nil {
		return fmt.Errorf("工具 %s 未声明有效的 gui_binary: %w", toolName, err)
	}

	// 3. 拼接并校验存在性（精确路径，不做 shim 定位）。
	exePath := filepath.Join(installPath, guiBinary)
	if fi, statErr := os.Stat(exePath); statErr != nil || fi.IsDir() {
		return fmt.Errorf("GUI 二进制不存在: %s", exePath)
	}

	// 4. 启动：Dir=installPath，Env=os.Environ()+插件环境变量覆盖
	//    （与 runInstaller 相同的注入模式：GOROOT/JAVA_HOME 等）。
	//    不调用 util.HideWindow——GUI 应用必须显示窗口。
	cmd := exec.Command(exePath)
	cmd.Dir = installPath
	env := os.Environ()
	for k, v := range p.EnvVars(installPath) {
		env = append(env, k+"="+v)
	}
	cmd.Env = env
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("启动 %s 失败: %w", exePath, err)
	}
	// 后台 Wait 防止 Unix 僵尸进程：子进程退出时回收资源。
	// Windows 上 Process.Release 即可，但 Wait 更跨平台安全。
	go func() { _ = cmd.Wait() }()
	return nil
}

// ToolCommand 返回工具的 CLI 启动命令（shims[0]）。
//
// 工具未安装时返回空串（shim 仅激活后存在，未安装的 shim 名无意义）；
// 无 shims 的纯 GUI 工具无法猜测命令，同样返回空串。
func (a *App) ToolCommand(toolName string) string {
	p, err := a.mustGetPlugin(toolName)
	if err != nil {
		return ""
	}
	if _, installed := a.currentVersion(toolName); !installed {
		return ""
	}
	if shims := p.Shims(); len(shims) > 0 {
		return shims[0]
	}
	return ""
}

// safeLaunchBinary 规范化 gui_binary 相对路径并拒绝越界（与 installer 的
// safeGUIBinary 语义一致）：
//   - 绝对路径 / 根化路径（"/x" 或 "\x"）→ 拒绝；
//   - filepath.Clean 归一化（"a/../b/app.exe" → "b/app.exe"）；
//   - Clean 后仍以 ".." 开头（如 "bin/../../outside.exe" → "../outside.exe"）
//     → 拒绝；
//   - 含 volume name（如 "C:\x"）→ 拒绝（仅允许相对路径）。
func safeLaunchBinary(guiBinary string) (string, error) {
	g := strings.TrimSpace(guiBinary)
	if g == "" {
		return "", fmt.Errorf("gui_binary 为空")
	}
	g = filepath.FromSlash(g)
	if filepath.IsAbs(g) || strings.HasPrefix(g, string(filepath.Separator)) {
		return "", fmt.Errorf("路径必须是相对路径: %q", guiBinary)
	}
	if vol := filepath.VolumeName(g); vol != "" {
		return "", fmt.Errorf("路径包含卷名 %q: %s", vol, guiBinary)
	}
	g = filepath.Clean(g)
	if g == ".." || strings.HasPrefix(g, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("路径越界: %q", guiBinary)
	}
	return g, nil
}
