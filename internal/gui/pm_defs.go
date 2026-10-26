package gui

import (
	"github.com/xmdemt/xdev/internal/config"
	"github.com/xmdemt/xdev/internal/pm"
)

// pmDef 是 gui 内部的包管理器定义。
//
// # 与 pm.Def 的关系（2026-10-25 数据化）
//
// 修复前这 13 个定义硬编码在本包的 packageManagerDefs() 里（含 5 个
// 会被直接执行的命令字符串），签名链覆盖不到、CLI 也用不上。
// 现在数据源搬到 manifests/package-managers.yaml（internal/pm 负责加载），
// 本类型保留为**gui 侧视图**。
//
// 为什么不直接用 pm.Def：gui 的 92 处引用都用 camelCase 字段
// （name / displayName / detectCmd ...），而 pm.Def 是导出的 Go 字段。
// 把 92 处全改成 pm.Def 字段名会制造一次高风险的大范围机械改动，
// 而收益仅是少一个转换函数。这里选择保留内部类型 + 一次性转换。
type pmDef struct {
	name, displayName, description string
	detectCmd                      string
	detectArgs                     []string
	toolName                       string // xdev tool name (empty = not installable via xdev)
	installHint                    string // hint when not installable
	// Spec 025: 包管理器自动安装支持。
	// installCommand 非空时，InstallPackageManager 走 script install 路径
	// （后台执行官方安装命令，而非 xdev download→extract→activate 管线）。
	installCommand string // 官方安装命令（空 = 不可自动安装，保持现有行为）
	requiresAdmin  bool   // 是否需要管理员权限（Windows 下检测，非管理员时提示）
	executionEnv   string // "powershell" | "bash" — 执行环境
	platform       string // "windows" | "darwin" | "linux" | "any" — 限定平台
	// Spec 026: 附属工具动态安装支持。
	enableCommand string // 启用命令（如 "corepack enable yarn"），空 = 非附属工具
	parentTool    string // 父工具名（如 "node"），用于检测父工具是否已安装
	// installMethods 列出可选安装方式（如 pipx 可通过 pip 或 uv 安装）。
	installMethods []installMethod // 空 = 单一安装方式或不可安装
}

// installMethod 描述附属工具的一种安装方式（Spec 026）。
type installMethod struct {
	name      string // 方法标识（如 "pip" / "uv"），前端用于选项对话框
	command   string // 执行命令（如 "pip install pipx"）
	detectCmd string // 检测该方法前置工具是否可用的命令名（如 "pip" / "uv"）
}

// fromPM 把 pm.Def 转成 gui 的 pmDef。
func fromPM(d pm.Def) pmDef {
	methods := make([]installMethod, 0, len(d.InstallMethods))
	for _, m := range d.InstallMethods {
		methods = append(methods, installMethod{
			name:      m.Name,
			command:   m.Command,
			detectCmd: m.DetectCmd,
		})
	}
	return pmDef{
		name:           d.Name,
		displayName:    d.DisplayName,
		description:    d.Description,
		detectCmd:      d.DetectCmd,
		detectArgs:     d.DetectArgs,
		toolName:       d.ToolName,
		installHint:    d.InstallHint,
		installCommand: d.InstallCommand,
		requiresAdmin:  d.RequiresAdmin,
		executionEnv:   d.ExecutionEnv,
		platform:       d.Platform,
		enableCommand:  d.EnableCommand,
		parentTool:     d.ParentTool,
		installMethods: methods,
	}
}

// packageManagerDefs 返回包管理器定义列表（GetPackageManagers 共享）。
//
// 2026-10-25：数据源改为 manifests/package-managers.yaml（经 internal/pm
// 加载，缺失或损坏时回退到内置定义）。函数签名保持不变，因此既有
// 调用点无需改动。
//
// 为什么这里自己 config.Load 而不是取 App.cfg：本函数是**包级**函数
// （无接收者），而 92 处调用点中有几处在 App 方法外。config.Load 内部
// 有缓存，额外开销可忽略。
func packageManagerDefs() []pmDef {
	dir := ""
	if cfg, err := config.Load(""); err == nil {
		dir = cfg.Core.ManifestDir
	}
	return packageManagerDefsFrom(dir)
}

// packageManagerDefsFrom 按指定 manifest 目录加载定义（测试用）。
func packageManagerDefsFrom(manifestDir string) []pmDef {
	defs := pm.LoadOrDefault(manifestDir)
	out := make([]pmDef, 0, len(defs))
	for _, d := range defs {
		out = append(out, fromPM(d))
	}
	return out
}
