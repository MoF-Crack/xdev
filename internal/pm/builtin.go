package pm

// builtinDefs 是内置的包管理器定义，与 manifests/package-managers.yaml
// 内容保持一致。
//
// # 为什么要有内置副本
//
// 数据文件可能缺失（老部署、用户误删）或损坏（手改 YAML 写错）。
// 包管理器检测是只读能力，不该因为一个文件问题就整个不可用。
//
// # 两份数据的一致性
//
// 由 TestBuiltinMatchesYAML 守卫：它会读 manifests/package-managers.yaml
// 与 Builtin() 逐字段比对，任何一边改了另一边没跟上就会失败。
// 这样「内置回退」不会悄悄落后于数据文件。
func builtinDefs() []Def {
	return []Def{
		{
			Name: "npx", DisplayName: "NPX",
			Description: "Node 包执行器 - 运行 npm 包",
			DetectCmd:   "npx", DetectArgs: []string{"--version"},
			ToolName: "node", InstallHint: "随 Node.js 自动安装",
		},
		{
			Name: "npm", DisplayName: "NPM",
			Description: "Node 包管理器",
			DetectCmd:   "npm", DetectArgs: []string{"--version"},
			ToolName: "node", InstallHint: "随 Node.js 自动安装",
		},
		{
			Name: "yarn", DisplayName: "Yarn",
			Description: "快速、可靠的依赖管理工具",
			DetectCmd:   "yarn", DetectArgs: []string{"--version"},
			ToolName:    "node",
			InstallHint: "需先安装 Node.js，然后通过 corepack enable 或 npm install -g yarn 安装",
			EnableCommand: "corepack enable yarn",
			ParentTool:    "node",
			InstallMethods: []InstallMethod{
				{Name: "corepack", Command: "corepack enable yarn", DetectCmd: "corepack"},
				{Name: "npm", Command: "npm install -g yarn", DetectCmd: "npm"},
			},
		},
		{
			Name: "pnpm", DisplayName: "PNPM",
			Description: "快速、节省磁盘的包管理器",
			DetectCmd:   "pnpm", DetectArgs: []string{"--version"},
			ToolName:    "node",
			InstallHint: "需先安装 Node.js，然后通过 corepack enable 或 npm install -g pnpm 安装",
			EnableCommand: "corepack enable pnpm",
			ParentTool:    "node",
			InstallMethods: []InstallMethod{
				{Name: "corepack", Command: "corepack enable pnpm", DetectCmd: "corepack"},
				{Name: "npm", Command: "npm install -g pnpm", DetectCmd: "npm"},
			},
		},
		{
			Name: "uvx", DisplayName: "UVX",
			Description: "通过 UV 运行 Python 工具",
			DetectCmd:   "uvx", DetectArgs: []string{"--version"},
			ToolName: "uv",
		},
		{
			Name: "uv", DisplayName: "UV",
			Description: "极速 Python 包安装器",
			DetectCmd:   "uv", DetectArgs: []string{"--version"},
			ToolName: "uv",
		},
		{
			Name: "pip", DisplayName: "PIP",
			Description: "Python 包安装器",
			DetectCmd:   "pip", DetectArgs: []string{"--version"},
			ToolName: "python", InstallHint: "随 Python 自动安装",
		},
		{
			Name: "pipx", DisplayName: "PIPX",
			Description: "在隔离环境中安装运行 Python 应用",
			DetectCmd:   "pipx", DetectArgs: []string{"--version"},
			InstallHint: "通过 pip install pipx 或 uv tool install pipx 安装",
			InstallMethods: []InstallMethod{
				{Name: "pip", Command: "pip install pipx", DetectCmd: "pip"},
				{Name: "uv", Command: "uv tool install pipx", DetectCmd: "uv"},
			},
		},
		{
			Name: "cargo", DisplayName: "Cargo",
			Description: "Rust 包管理器",
			DetectCmd:   "cargo", DetectArgs: []string{"--version"},
			ToolName: "rust", InstallHint: "随 Rust 自动安装",
		},
		{
			Name: "maven", DisplayName: "Maven",
			Description: "Java 构建自动化工具",
			DetectCmd:   "mvn", DetectArgs: []string{"--version"},
			ToolName: "maven", InstallHint: "随 Maven 自动安装",
		},
		{
			Name: "brew", DisplayName: "Homebrew",
			Description: "macOS/Linux 包管理器",
			DetectCmd:   "brew", DetectArgs: []string{"--version"},
			InstallHint:    "访问 https://brew.sh 安装",
			InstallCommand: `/bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"`,
			RequiresAdmin:  false,
			ExecutionEnv:   "bash",
			Platform:       "unix", // darwin/linux/WSL，非 windows
		},
		{
			Name: "scoop", DisplayName: "Scoop",
			Description: "Windows 命令行安装器",
			DetectCmd:   "scoop", DetectArgs: []string{"--version"},
			InstallHint:    "访问 https://scoop.sh 安装",
			InstallCommand: "irm get.scoop.sh | iex",
			RequiresAdmin:  false,
			ExecutionEnv:   "powershell",
			Platform:       "windows",
		},
		{
			Name: "choco", DisplayName: "Chocolatey",
			Description: "Windows 包管理器",
			DetectCmd:   "choco", DetectArgs: []string{"--version"},
			InstallHint:    "访问 https://chocolatey.org 安装",
			InstallCommand: `Set-ExecutionPolicy Bypass -Scope Process -Force; iex ((New-Object System.Net.WebClient).DownloadString('https://community.chocolatey.org/install.ps1'))`,
			RequiresAdmin:  true,
			ExecutionEnv:   "powershell",
			Platform:       "windows",
		},
	}
}

// Builtin 返回内置定义的副本（调用方可以随意修改，不影响下次调用）。
func Builtin() []Def {
	src := builtinDefs()
	out := make([]Def, len(src))
	copy(out, src)
	return out
}
