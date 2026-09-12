// app_dependencies.go implements the tool dependency check API
// (Spec 023 T-B1 / F5): for a tool's manifest `depends` declarations,
// report platform / presence / version satisfaction so the frontend
// detail page can show dependency badges.
//
// 判定链（每个依赖）：
//  1. 平台：dep.Platforms 非空且不含当前 GOOS/"any" → 不满足（平台原因）；
//  2. 存在性：xdev 已装（current/<tool> 生效）优先；否则 exec.LookPath
//     （跳过 xdev shim/install 目录——那是 xdev 自身的软链，不算外部安装）；
//  3. 版本：dep.MinVersion 非空且检测到可解析版本 → CompareSemver 比较；
//     版本未知/不可解析 → 宽松判定"存在即满足"（记录原因，不误报）。
//
// 注意：getVersion 会派生子进程（2s 超时），本 API 用于详情页按需调用，
// 不在列表/扫描路径中使用。
package gui

import (
	"os/exec"
	"runtime"
	"strings"

	"github.com/xmdemt/xdev/internal/manifest"
	"github.com/xmdemt/xdev/internal/util"
)

// DependencyStatus 单个依赖约束的检查结果。
type DependencyStatus struct {
	Tool            string `json:"tool"`            // 依赖的工具名（如 "node"）
	RequiredVersion string `json:"requiredVersion"` // manifest 声明的最低版本（可能为空=仅存在性）
	CurrentVersion  string `json:"currentVersion"`  // 检测到的当前版本（未找到时为空）
	Platform        string `json:"platform"`        // 限定的平台（逗号连接，可能为空）
	InstallPath     string `json:"installPath"`     // 依赖适用的安装路径（如 "npm"，信息性字段）
	Satisfied       bool   `json:"satisfied"`
	Reason          string `json:"reason"`           // 兼容字段，保留空串（前端用 reasonCode 翻译）
	ReasonCode      string `json:"reasonCode"`       // 机器可读原因码：platform/not_found/version_low/version_unknown
	ReasonParams    map[string]string `json:"reasonParams"` // 原因参数（如 {"tool":"node","installed":"20","required":"22"}）
}

// CheckToolDependencies 返回某工具 manifest 声明的全部依赖检查结果。
// 工具不存在或无 depends 声明时返回 nil（前端显示为空）。
func (a *App) CheckToolDependencies(toolName string) []DependencyStatus {
	tool := a.toolManifest(toolName)
	if tool == nil || len(tool.Depends) == 0 {
		return nil
	}
	statuses := make([]DependencyStatus, 0, len(tool.Depends))
	for _, d := range tool.Depends {
		statuses = append(statuses, a.checkDependency(d))
	}
	return statuses
}

// toolManifest 获取 toolName 的 manifest：优先 catalog（a.cat），
// 回退到 registry 插件的 Manifest()（测试桩 / Lua 插件场景）。
func (a *App) toolManifest(toolName string) *manifest.Tool {
	// 票 24（P1-16 / P2-07）：经 getCat() 读 cat（RLock），避免与
	// reloadCatalog 的写锁换指针竞争。
	if cat := a.getCat(); cat != nil {
		if t, ok := cat.Tool(toolName); ok {
			return t
		}
	}
	if a.registry != nil {
		if p, ok := a.registry.Get(toolName); ok {
			return p.Manifest()
		}
	}
	return nil
}

// checkDependency 检查单个依赖约束（判定链见文件头注释）。
func (a *App) checkDependency(d manifest.Dependency) DependencyStatus {
	st := DependencyStatus{
		Tool:            d.Tool,
		RequiredVersion: d.MinVersion,
		Platform:        strings.Join(d.Platforms, ","),
		InstallPath:     d.InstallPath,
	}

	// 1. 平台检查：Platforms 非空且不含当前 GOOS / "any" → 不满足。
	if len(d.Platforms) > 0 && !platformMatches(d.Platforms) {
		st.ReasonCode = "platform"
		st.ReasonParams = map[string]string{"needed": strings.Join(d.Platforms, "/"), "current": runtime.GOOS}
		return st
	}

	// 2. 存在性检查：xdev 已装优先，否则 LookPath（跳过 xdev shim/install 目录）。
	ver, found := a.findDependencyTool(d.Tool)
	st.CurrentVersion = ver
	if !found {
		st.ReasonCode = "not_found"
		st.ReasonParams = map[string]string{"tool": d.Tool}
		return st
	}

	// 3. 版本检查：MinVersion 非空时才比较（加载时已清空非法 MinVersion）。
	if d.MinVersion != "" {
		if ver == "" || !looksLikeVersion(ver) {
			// 版本未知/不可解析 → 存在即满足（宽松，记录原因）。
			st.Satisfied = true
			st.ReasonCode = "version_unknown"
			return st
		}
		if util.CompareSemver(ver, d.MinVersion) < 0 {
			st.ReasonCode = "version_low"
			st.ReasonParams = map[string]string{"installed": ver, "required": d.MinVersion}
			return st
		}
	}
	st.Satisfied = true
	return st
}

// findDependencyTool 定位依赖工具并返回其版本：
//  1. xdev 已装（current/<tool> 生效版本）优先——版本号即激活版本；
//  2. 否则 exec.LookPath，但跳过 xdev 管理的目录（shim/install——
//     与 detectPackageManager 的判定一致：xdev 软链不算外部安装）；
//  3. 都没有 → 未找到。
//
// 返回 ("", true) 表示"存在但版本未知"（如 PATH 命中但取版本失败）。
func (a *App) findDependencyTool(toolName string) (string, bool) {
	// 1. xdev 已装优先。
	if ver, installed := a.currentVersion(toolName); installed {
		return ver, true
	}

	// 2. LookPath（跳过 xdev 管理目录）。
	cfg := a.getCfg()
	path, err := exec.LookPath(toolName)
	if err != nil {
		return "", false
	}
	if (cfg.Core.ShimDir != "" && isInsideDir(path, cfg.Core.ShimDir)) ||
		(cfg.Core.InstallDir != "" && isInsideDir(path, cfg.Core.InstallDir)) {
		// 路径在 xdev 管理目录内但未激活（current 缺失）→ 视为未找到。
		return "", false
	}

	// 3. 尝试取版本（慢路径：getVersion 有 2s 超时，详情页按需调用可接受）。
	ver := getVersion(toolName, path)
	return ver, true
}

// platformMatches 报告当前平台是否在允许列表（"any" 恒匹配）。
func platformMatches(platforms []string) bool {
	for _, p := range platforms {
		if p == "any" || p == runtime.GOOS {
			return true
		}
	}
	return false
}

// looksLikeVersion 宽松判断版本串是否可比较（数字开头，容忍 "v" 前缀）。
// CompareSemver 对垃圾串隐式返回 0 比较，需先做轻量预检避免误报"版本过低"。
// 至少要求首段为纯数字（如 "22.0.0"、"v1.2.3"），拒绝 "22oops"、"r29" 等非 semver 串。
func looksLikeVersion(s string) bool {
	s = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(s), "v"))
	if s == "" {
		return false
	}
	// 首段（到第一个 "." 或末尾）必须全为数字。
	end := strings.IndexByte(s, '.')
	first := s
	if end > 0 {
		first = s[:end]
	}
	for _, c := range first {
		if c < '0' || c > '9' {
			return false
		}
	}
	return len(first) > 0
}
