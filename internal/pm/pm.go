// Package pm 描述 xdev 支持的包管理器（npm / pip / scoop / brew / choco ...），
// 并从数据文件加载它们的定义。
//
// # 为什么单独一个包（2026-10-25 架构收口）
//
// 修复前这 13 个定义硬编码在 internal/gui/app_local_detect.go 的
// packageManagerDefs() 里，其中 5 个带**可执行命令**：
//
//	scoop:   irm get.scoop.sh | iex
//	brew:    /bin/bash -c "$(curl -fsSL .../install.sh)"
//	choco:   Set-ExecutionPolicy Bypass -Scope Process -Force; iex (...)
//	yarn:    corepack enable yarn / npm install -g yarn
//	pnpm:    corepack enable pnpm / npm install -g pnpm
//
// 这些字符串会被 powershell -Command / bash -c 直接执行。把它们放在
// gui 包里有两个问题：
//
//  1. **不在签名覆盖范围内**。manifests/*.yaml 有 Ed25519 签名链
//     （manifests/*.yaml.sig），而这些可执行字符串是 Go 代码常量，
//     改一行就能改掉用户机器上会被执行的命令，签名链完全看不到。
//  2. **CLI/TUI 完全用不到**。GetPackageManagers 只在 gui 里，
//     `xdev` 命令行没有对应能力。
//
// 现在定义搬到 manifests/package-managers.yaml（随 manifest 一起分发、
// 一起签名），本包负责加载与内置回退。
//
// # 内置回退
//
// 没有外部官方文件时使用二进制内置定义；无密钥开发构建也可明确忽略与内置一致的未签副本。
// 其他缺签、坏签和损坏的官方文件均报错。自定义条目放入 local/package-managers.yaml，
// 第三方条目放入 third-party/package-managers.yaml；运行前须确认具体定义摘要。
package pm

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// FileName 是包管理器定义的数据文件名（与 manifests/*.yaml 同目录）。
const FileName = "package-managers.yaml"

// InstallMethod 描述附属工具的一种安装方式。
// 例如 pipx 可通过 pip install pipx 或 uv tool install pipx 安装。
type InstallMethod struct {
	Name      string `yaml:"name"`       // 方法标识（如 "pip" / "uv"），前端用于选项对话框
	Command   string `yaml:"command"`    // 执行命令（如 "pip install pipx"）
	DetectCmd string `yaml:"detect_cmd"` // 检测该方法前置工具是否可用的命令名
}

// Def 是一个包管理器的定义。
//
// 字段与历史 gui.pmDef 一一对应（只是从 Go 结构体搬到了 YAML），
// 避免迁移时改变任何既有语义。
type Def struct {
	Origin      Origin `yaml:"-" json:"-"`
	Name        string `yaml:"name"`
	DisplayName string `yaml:"display_name"`
	Description string `yaml:"description"`

	// DetectCmd / DetectArgs 用于检测该包管理器是否存在。
	DetectCmd  string   `yaml:"detect_cmd"`
	DetectArgs []string `yaml:"detect_args"`

	// ToolName 是 xdev 工具名（空 = 不能通过 xdev 安装）。
	ToolName string `yaml:"tool_name"`

	// InstallHint 是「不可自动安装」时给用户的提示。
	InstallHint string `yaml:"install_hint"`

	// InstallCommand 非空时，安装走 script install 路径
	// （后台执行官方安装命令，而非 xdev download→extract→activate 管线）。
	//
	// 安全提示：这个字符串会被直接执行。它现在位于签名覆盖的数据文件里，
	// 而不再是 Go 代码常量。
	InstallCommand string `yaml:"install_command"`
	RequiresAdmin  bool   `yaml:"requires_admin"`
	ExecutionEnv   string `yaml:"execution_env"` // "powershell" | "bash"
	Platform       string `yaml:"platform"`      // "windows" | "darwin" | "linux" | "any" | "unix"

	// EnableCommand 非空时，该工具是「可启用附属工具」
	// （如 yarn/pnpm 通过 corepack enable 启用）。
	EnableCommand string `yaml:"enable_command"`
	// ParentTool 是父工具名（如 "node"），用于检测父工具是否已安装。
	ParentTool string `yaml:"parent_tool"`

	// InstallMethods 列出可选安装方式。空 = 单一方式或不可安装。
	InstallMethods []InstallMethod `yaml:"install_methods"`
}

// file 是数据文件的顶层结构。
type file struct {
	// Comment 允许在 YAML 里写说明（yaml.v3 会忽略未知键，这里显式留位）。
	PackageManagers []Def `yaml:"package_managers"`
}

func parse(data []byte, path string) ([]Def, error) {
	var f file
	if err := yaml.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if len(f.PackageManagers) == 0 {
		return nil, fmt.Errorf("%s: no package_managers entries", path)
	}
	if err := validate(f.PackageManagers); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return f.PackageManagers, nil
}

var validID = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]*$`)

// validate 做最基本的完整性校验。
//
// 只查「结构性缺陷」（空名、重复名、命令与执行环境不匹配），
// 不查业务语义 —— 那是调用方的事。
func validate(defs []Def) error {
	seen := make(map[string]bool, len(defs))
	for i := range defs {
		d := &defs[i]
		d.Name = strings.TrimSpace(d.Name)
		if d.Name == "" {
			return fmt.Errorf("entry #%d has empty name", i)
		}
		id := strings.ToLower(d.Name)
		if !validID.MatchString(d.Name) || seen[id] {
			return fmt.Errorf("invalid or duplicate package manager name %q", d.Name)
		}
		seen[id] = true
		switch d.Platform {
		case "", "any", "windows", "linux", "darwin", "unix":
		default:
			return fmt.Errorf("unsupported PM platform %q", d.Platform)
		}
		switch d.ExecutionEnv {
		case "", "powershell", "bash":
		default:
			return fmt.Errorf("unsupported execution_env %q", d.ExecutionEnv)
		}
		methods := map[string]bool{}
		for _, m := range d.InstallMethods {
			if !validID.MatchString(m.Name) || methods[m.Name] || strings.TrimSpace(m.Command) == "" {
				return fmt.Errorf("invalid or duplicate PM method %q", m.Name)
			}
			methods[m.Name] = true
		}

		// 有安装命令就必须有执行环境 —— 否则不知道用哪个 shell 跑。
		if d.InstallCommand != "" && d.ExecutionEnv == "" {
			return fmt.Errorf("%q has install_command but no execution_env", d.Name)
		}
		// enable_command 是 shell 命令，但走的是 installMethods 的同一执行路径，
		// 这里只要求附属工具必须有 parent_tool（否则无法判断父工具是否已装）。
		if d.EnableCommand != "" && d.ParentTool == "" {
			return fmt.Errorf("%q has enable_command but no parent_tool", d.Name)
		}
	}
	return nil
}

// Find 按 name 查找定义。
func Find(defs []Def, name string) (Def, bool) {
	name = strings.ToLower(strings.TrimSpace(name))
	for _, d := range defs {
		if strings.ToLower(d.Name) == name {
			return d, true
		}
	}
	return Def{}, false
}

// FindByToolName 按 xdev 工具名反查附属包管理器（pip → python）。
//
// 只返回**第一个**匹配项：一个工具可能有多个附属 PM（python 有 pip/pipx），
// 但调用方通常只想要「这个工具自带的那个」。
func FindByToolName(defs []Def, toolName string) (Def, bool) {
	toolName = strings.ToLower(strings.TrimSpace(toolName))
	for _, d := range defs {
		if strings.ToLower(d.ToolName) == toolName {
			return d, true
		}
	}
	return Def{}, false
}

// Names 返回所有定义的名字（排序，便于稳定输出）。
func Names(defs []Def) []string {
	out := make([]string, 0, len(defs))
	for _, d := range defs {
		out = append(out, d.Name)
	}
	sort.Strings(out)
	return out
}
