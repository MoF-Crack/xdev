> **文档类型**：长期参考文档
> **状态**：Living
> **来源**：docs/architecture.md
> **P4B 迁移目标**：原位保留
> **替代文档**：无
> **治理记录**：P4A 元数据治理，2026-07-16

# XDev 架构文档

> 版本: 0.3.1 | 最后更新: 2026-08-20

## 1. 项目概览

XDev 是一个跨平台开发环境版本管理器，用 Go 1.25 编写。它提供三种交互模式：

- **CLI** — 基于 Cobra 的命令行界面
- **TUI** — 基于 Bubble Tea 的终端用户界面
- **GUI** — 基于 Wails v2 + Vue 3 的桌面图形界面

核心设计理念：**单一二进制多入口**（Shim Dispatch）—— xdev 二进制同时充当 CLI 和所有托管工具的 shim。

## 2. 目录结构

```
xmdevenvmt/
├── cmd/xmdevenvmt/          # 入口点 (main.go, main_bindings.go)
├── internal/                 # 内部包（不可外部导入）
│   ├── cli/                  # CLI 命令定义 (Cobra)
│   ├── config/               # 配置加载/保存/迁移
│   ├── downloader/           # HTTP 下载器 (带进度/断点续传)
│   ├── env/                  # 环境变量操作 (Windows 注册表/Shell RC)
│   ├── extractor/            # 归档解压 (zip/tar.gz/7z/zstd)
│   ├── gui/                  # GUI 后端 (Wails App adapter)
│   ├── hostscan/             # Host-scan：PATH 上全部实例（跳过 xdev shim）
│   ├── installer/            # 安装/激活/卸载核心工作流
│   ├── integration/          # L2 集成测试
│   ├── isolator/             # 环境隔离 (none/container/nix)
│   ├── lockfile/             # .xdev.lock 锁文件管理
│   ├── manifest/             # 工具清单 YAML 加载/缓存
│   ├── mirror/               # 镜像源配置与选择
│   ├── orchestrator/         # 远程编排 (Docker/SSH)
│   ├── plugin/               # 插件接口 + Generic/Lua/Vfox 实现
│   ├── plugins/java/         # Java 插件 (自定义 ArchiveFor)
│   ├── project/              # .xdev.toml 项目配置
│   ├── remote/               # 远程版本发现
│   ├── resolver/             # 版本规范解析 (^, ~, >=)
│   ├── service/              # 长运行服务管理 (数据库/缓存等)
│   ├── shim/                 # Shim 调度与安装
│   ├── signer/               # Ed25519 清单签名验证
│   ├── task/                 # 项目任务运行器
│   ├── tui/                  # TUI 界面 (Bubble Tea)
│   ├── update/               # 自更新机制
│   ├── util/                 # 通用工具函数
│   └── version/              # 版本常量
├── manifests/                # 工具清单 YAML 文件
│   ├── index.yaml            # 分类索引
│   ├── python.yaml           # Python 清单
│   ├── go.yaml               # Go 清单
│   └── ...                   # 40+ 工具清单
├── frontend/                 # Vue 3 前端 (Wails GUI)
│   └── src/
│       ├── App.vue           # 根组件
│       ├── AppContent.vue    # 主布局（shell + 安装/PM/环境编排）
│       └── SetupWizard.vue   # 首次运行向导
├── docs/                     # 文档
├── build.sh                  # Unix 构建脚本
├── build.ps1                 # Windows 构建脚本
└── wails.json                # Wails 配置
```

## 3. 包依赖图

### 层级架构

```
┌─────────────────────────────────────────────┐
│  cmd/xmdevenvmt  (入口点)                    │
├─────────────────────────────────────────────┤
│  cli  │  gui  │  tui                        │  ← 表示层
├─────────────────────────────────────────────┤
│  installer  │  service  │  task  │  update   │  ← 业务逻辑层
├─────────────────────────────────────────────┤
│  plugin  │  manifest  │  config  │  lockfile │  ← 核心抽象层
├─────────────────────────────────────────────┤
│  downloader │ extractor │ env │ shim │ hostscan │ mirror│  ← 基础设施层
├─────────────────────────────────────────────┤
│  util  │  version  │  resolver  │  signer    │  ← 工具层
└─────────────────────────────────────────────┘
```

### 关键依赖关系

| 包 | 依赖的内部包 | 角色 |
|---|---|---|
| `cli` | config, env, gui, installer, isolator, lockfile, manifest, orchestrator, plugin, project, remote, service, shim, signer, task, tui, update, util, version | 命令路由与编排 |
| `installer` | config, downloader, env, extractor, manifest, mirror, plugin, shim, signer, util, version | 安装工作流核心 |
| `gui` | config, downloader, env, extractor, installer, isolator, lockfile, manifest, orchestrator, plugin, project, remote, discovery, hostscan, shim, update, util, version | Wails desktop adapter：catalog / install-session / host-scan / prefs（约 120 个导出方法，现有绑定不增） |
| `hostscan` | — | PATH 全量扫描（跳过 shimDir）；GUI 探测委托它 |
| `plugin` | manifest, resolver | 插件抽象 |
| `config` | env, util | 配置管理 |
| `shim` | config, project, util | Shim 调度 |

### 叶子包（无内部依赖）

`e2e`, `extractor`, `hostscan`, `i18n`, `integration`, `lockfile`, `manifest`, `project`, `resolver`, `signer`, `util`, `version`

## 4. 启动流程

### CLI 启动

```
main.go:init()
  └── manifest.InitEmbedded(manifestFS)  // 嵌入清单 YAML

main.go:main()
  ├── 检查 os.Args[0] basename
  │   ├── "xdev-gui*" → runGUI()
  │   └── 其他 → CLI 路径
  └── cli.NewRootCommand()
      ├── shim.MaybeExec(basename)        // Shim 调度 (优先!)
      │   ├── 是 xdev/xdev-gui → 返回, 继续 CLI
      │   └── 是工具别名 → 查找并执行真实工具, os.Exit
      ├── cobra.OnInitialize → initContext()
      │   └── config.Load(xmdeDir)       // 加载配置
      └── 注册 30+ 子命令
          └── 无子命令 → 启动 TUI
```

### GUI 启动

```
main.go:runGUI()
  ├── config.Load("")
  ├── manifest.LoadCatalogCached()
  ├── plugin.RegisterAll()
  └── gui.Run(app, webviewDir)
      └── wails.Run()
          ├── App.startup(ctx)            // 存储 Wails 上下文
          └── App.domReady()              // WindowShow(), 刷新缓存
```

## 5. 核心工作流

### 5.1 安装流程 (`xdev install go@1.22.0`)

```
CLI: newInstallCmd()
  │
  ├── splitToolVer("go@1.22.0") → ("go", "1.22.0")
  ├── loadCatalogAndRegistry() → (catalog, registry)
  ├── registry.Get("go") → plugin.Plugin
  │
  └── installer.New(cfg, reg).InstallWithProgress(ctx, p, "1.22.0")
      │
      ├── 1. p.ResolveVersion("1.22.0")    // 版本解析
      ├── 2. p.ArchiveFor(ver)             // 选择归档 (OS/Arch 匹配)
      ├── 3. 计算安装路径 install/go/1.22.0
      ├── 4. 检查是否已安装 (os.Stat)
      ├── 5. downloadWithProgress()         // 下载 (含镜像/SHA256/断点续传)
      ├── 6. extractor.Extract()            // 解压 (zip/tar.gz/7z)
      ├── 7. p.PostInstall()               // 后安装钩子
      ├── 8. Activate(p, installPath, ver)  // 激活
      │   ├── 创建 current/go junction → install/go/1.22.0
      │   ├── 创建 shims/go.exe (复制 xdev 二进制)
      │   ├── setUserEnvVars({GOROOT, GOPATH})  // Windows 注册表
      │   └── BroadcastEnvironmentChange()      // WM_SETTINGCHANGE
      └── 9. updateLockfile()              // 更新 .xdev.lock
```

### 5.2 Shim 调度流程

当用户执行 `go build ./...` 时，实际执行的是 `shims/go.exe`（xdev 二进制的副本）：

```
shims/go.exe build ./...
  │
  ├── main.go → cli.NewRootCommand()
  │   └── shim.MaybeExec("go")
  │       └── shim.Lookup("go", args)
  │           ├── 1. 检查 .xdev.toml 项目级版本覆盖
  │           ├── 2. 查找 current/go/bin/go.exe
  │           └── 3. 跨工具扫描 (shim 反向索引)
  │
  └── exec.Command(realGo, "build", "./...").Run()
      └── 传播子进程退出码 via ExitCodeError
```

### 5.3 Shell Hook 流程

```
用户 cd /path/to/project
  │
  ├── bash: alias cd → _xdev_original_cd → _xdev_cd_hook
  │   └── eval "$(xdev hook --shell bash)"
  │
  └── xdev hook --shell bash
      ├── 查找最近 .xdev.toml
      ├── 解析工具版本和路径
      └── 输出: export PATH=...; export GOROOT=...
```

## 6. 核心接口

### 6.1 Plugin 接口

```go
type Plugin interface {
    // 元数据
    Name() string                           // "go"
    DisplayName() string                    // "Go"
    Description() string                    // "The Go programming language..."
    Homepage() string                       // "https://go.dev"
    Category() string                       // "language"
    Manifest() *manifest.Tool               // 关联的 YAML 清单

    // 版本解析
    ResolveVersion(spec string) (string, error)  // "^1.22" → "1.22.5"
    DefaultVersion() string                       // "1.23.2"

    // 归档选择
    ArchiveFor(version string) (*manifest.Archive, error)  // OS/Arch 匹配

    // 安装路径
    InstallDir(root, version string) string  // "E:/XDev/install/go/1.22.0"

    // Shim 和环境变量
    Shims() []string                        // ["go", "gofmt", "godoc"]
    EnvVars(installPath string) map[string]string  // {"GOROOT": "E:/XDev/install/go/1.22.0"}

    // 生命周期钩子
    PostInstall(ctx context.Context, installPath string) error
    Deactivate(installPath string) error
}
```

### 6.2 插件实现

| 类型 | 文件 | 特点 |
|------|------|------|
| **Generic** | `plugin/generic.go` | 行为完全由 YAML 清单驱动；PostInstall/Deactivate 为空操作 |
| **Java** | `plugins/java/java.go` | 嵌入 Generic 但覆盖 ArchiveFor；动态生成 Oracle/Adoptium URL |
| **LuaPlugin** | `plugin/lua_plugin.go` | 通过 Lua 函数 (available/archive/env_vars) 定义行为 |
| **VfoxPlugin** | `plugin/vfox_compat.go` | 兼容 version-fox 格式的 Lua 插件 |

## 7. 配置系统

### 7.1 配置文件结构 (`config.yaml`)

```yaml
core:
  xmdevenv_dir: "E:\\XDev"           # 根目录
  download_dir: "E:\\XDev\\downloads"  # 下载缓存
  install_dir: "E:\\XDev\\install"     # 工具安装
  shim_dir: "E:\\XDev\\shims"          # Shim 目录
  cache_dir: "E:\\XDev\\cache"         # 清单缓存
  env_dir: "E:\\XDev\\env"             # 环境片段
  manifest_dir: "E:\\XDev\\manifests"  # 清单文件
path:
  mode: "shim"                         # shim | direct | auto
  inject_to_shell_rc: true
  inject_to_windows_path: true
proxy:
  http: ""
  https: ""
  no_proxy: ""
  enabled: false
mirror:                                 # 镜像源
  python: "https://mirrors.aliyun.com/python"
  node: "https://npmmirror.com/mirrors/node"
ai:
  ollama_url: ""
  model: ""
  timeout: ""
```

### 7.2 Home 目录解析优先级

1. `--xmdevenv-dir` CLI 标志
2. `$XMDEV_HOME` 环境变量
3. `last_selection.json` (用户之前的选择)
4. **Windows 智能回退**: 扫描 D:-J: 盘，选择空闲空间 ≥ 20 GiB 的最大盘
5. 现有安装扫描
6. 平台默认值 (Windows: `~/.xdev`, macOS: `~/Library/XDev`, Linux: `~/.local/share/xdev`)

## 8. 清单系统

### 8.1 清单 YAML 结构

```yaml
name: go
display_name: Go
description: The Go programming language toolchain.
homepage: https://go.dev/
category: language
shims: [go, gofmt, godoc]
env_vars:
  GOROOT: "{{install}}"           # 模板变量，安装时替换
  GOPATH: "{{install}}\\..\\gopath"
latest: "1.23.2"
versions:
  - version: "1.23.2"
    stable: true
    release: "2024-10-08"
    archives:
      - os: windows
        arch: amd64
        format: zip
        url: https://go.dev/dl/go1.23.2.windows-amd64.zip
        mirror_url: https://mirrors.aliyun.com/golang/...
        sha256: "0000..."          # 全零 = 占位符，跳过验证
        strip: 0                   # 解压时去除的前导路径层数
remote:
  type: github
  url: "golang/go"
  pattern: "go(\\d+\\.\\d+\\.\\d+)$"
```

### 8.2 清单加载流程

```
manifest.LoadCatalogCached()
  ├── 嵌入 FS 存在? → loadFromEmbedded()
  ├── 磁盘目录存在? → loadFromDir()
  └── 回退到仓库相对路径 manifests/
```

## 9. 安全机制

### 9.1 Zip Slip / Tar Slip 防护

- `extractZip` / `extractTarFromReader`: 每个文件路径写入前检查 `isSafePath()`
- `extract7z`: 解压后 `validateExtraction()` 验证所有文件在目标目录内
- 失败时自动 `os.RemoveAll(dst)` 清理
- Windows 大小写不敏感路径比较 (`EqualFold`)
- 检测硬链接、设备文件、命名管道等特殊文件

### 9.2 注册表环境变量白名单

`SetUserEnvVar`/`DeleteUserEnvVar`/`SetEnvVar`/`DeleteEnvVar` 仅允许写入预定义的变量名（JAVA_HOME, GOROOT, GOPATH, NODE_HOME 等），拒绝任意变量名。白名单统一由 `util.IsAllowedEnvVarName` 管理，`util.IsProtectedSystemEnvVar` 保护系统关键变量不被删除/修改。

### 9.3 Service exec 路径验证

`parseServiceMarker` 验证 `exec` 和 `datadir` 字段不包含 `..` 路径穿越。

### 9.4 Lua 沙箱

- 禁止 `io` 和 `os` 库
- `package.loaders` 限制为仅预加载搜索器，阻止加载任意文件或 C 模块

### 9.5 清单签名验证（分层信任模型）

**内置 manifest**（随二进制发布）：强制 Ed25519 签名验证。公钥通过 `-ldflags` 注入二进制，下载时验证 `.yaml.sig` 签名。签名失败或缺失时拒绝下载。

**第三方 manifest**（`xdev manifest add` 或 `xdev lock` 中的 tools）：使用 TOFU（Trust On First Use）模型。首次下载时记录 SHA-256 到 sidecar 文件，后续下载时比对。签名验证为可选——有 `.yaml.sig` 则验证，无则走 TOFU。

**代码路径**：`internal/downloader/downloader.go:108-114` 中 `opts.IsThirdParty` 标志决定拒绝还是降级。`internal/installer/installer.go:290-317` 检测 manifest 来源（内置 vs 第三方目录）并设置标志。

### 9.6 新增安全机制（本次会话）

| 机制 | 文件 | 说明 |
|------|------|------|
| SSRF 防护 | `util/url_safety.go` | `IsSafeExternalURL` 强制 HTTPS + 拒绝内网 IP |
| HMAC 常量时间比较 | `util/hmac_key.go:81` | 改用 `crypto/hmac.Equal` |
| 标准库 HMAC | `util/hmac_key.go:86` | 改用 `crypto/hmac` 替代自定义实现 |
| RequiredPlugin 依赖检查 | `installer.go:224-236` | 安装时验证依赖已安装 |
| RequiredPlugin 反向保护 | `installer.go:751-761` | 卸载时检查是否被其他工具依赖 |
| 配置完整性回退 | `config.go:227-237` | HMAC 校验失败时回退敏感字段到安全默认值 |

## 10. 测试架构

### 10.1 测试层级

| 层级 | 构建标签 | 位置 | 范围 |
|------|----------|------|------|
| L1 单元测试 | (默认) | `*_test.go` | 单个函数/方法，使用 fake/mock |
| L2 集成测试 | `integration` | `internal/integration/` | 驱动真实 xdev 二进制，隔离临时目录 |
| L3 E2E 测试 | `online` | `internal/e2e/` | 真实网络下载，验证完整用户流程 |

### 10.2 测试隔离

- L2 测试使用 `t.TempDir()` + `--xmdevenv-dir` + `--no-path-fix`
- `--no-path-fix` 同时阻止注册表环境变量写入和 PATH 修改
- L3 测试在下载失败时 `t.Skipf` 而非 `t.Fatal`

## 11. 构建系统

### 11.1 构建命令

```bash
# Unix
./build.sh

# Windows PowerShell
.\build.ps1              # 完整构建
.\build.ps1 -SkipGUI    # 仅 CLI
.\build.ps1 -SkipFrontend  # 跳过前端编译
```

### 11.2 Ldflags

```
-s -w                                    # 剥离调试信息
-X github.com/xmdemt/xdev/internal/version.BuildDate=<timestamp>
```

### 11.3 输出

- `build/bin/xdev.exe` — CLI 二进制 (约 20MB)
- `build/bin/xdev-gui.exe` — GUI 二进制 (约 17MB，嵌入前端资源)

## 12. GUI 架构

### 12.1 前端技术栈

- **Vue 3** + Composition API
- **Naive UI** 组件库 (自定义深色主题)
- **Wails v2** 桥接层

### 12.2 后端导出方法 (65+)

| 分类 | 方法 | 文件 |
|------|------|------|
| 工具查询 | GetCategories, GetToolsByCategory, GetInstalledVersions, GetAvailableVersions | app_tool_query.go |
| 安装管理 | InstallTool, UseVersion, UninstallVersion, PauseDownload, ResumeDownload, CancelDownload | app_install.go |
| 本地检测 | ScanLocalEnvironments, ScanLocalTools, MigrateLocalTool, MigrateAllLocalTools | app_local_detect.go, app_migration.go |
| 配置 | GetConfig, GetMirrorConfig, SetMirrorConfig, GetPathMode, SetPathMode, GetProxyConfig, SetProxyConfig | app_config.go |
| 环境变量 | GetEnvVars, SetEnvVar, DeleteEnvVar, GetAllEnvVars | app_env_vars.go |
| 项目 | GetProjectConfig, SetProjectTool, RunProjectTask, GetLockfile | app_project.go |
| 服务/插件/AI | ListServices, StartService, StopService, ListPlugins, GetAIStatus, AISuggest | app_project.go |
| 隔离/远程 | GetIsolateStatus, IsolateExec, ListRemoteInstances, CreateRemoteInstance | app_project.go |
| 收藏 | GetFavorites, AddFavorite, RemoveFavorite | app_favorites.go |
| 自定义工具 | GetCustomTools, AddCustomTool, RemoveCustomTool, InstallCustomTool | app_custom_tools.go |
| 配置文件 | ExportProfile, ImportProfile | app_profile.go |
| 初始化 | IsInitialized, InitConfig, GetDrives, SaveLastSelection | app_migration.go |

## 13. 新增核心接口（本次会话）

### 13.1 批量安装接口

```go
type BatchInstallItem struct {
    ToolName string
    Version  string // 空则使用 DefaultVersion
}

type BatchInstallResult struct {
    Tool        string
    Version     string
    InstallPath string
    Status      string // "installed" | "already_installed" | "unsupported" | "failed"
    Error       error
}

func (i *Installer) InstallMany(ctx context.Context, items []BatchInstallItem) []BatchInstallResult
```

**工作流程**：遍历 items → `reg.Get(toolName)`（不存在→unsupported）→ 检查 installPath 已存在（已装→跳过）→ `InstallWithProgress`（安装→成功/失败）。

**调用方**：`installFromLocal`（.xdev.toml 批量安装）、`installLocked`（.lock 文件批量安装）、`profile import`（配置导入）。

### 13.2 依赖检查接口

```go
// Plugin 接口方法
RequiredPlugin() []string // 返回依赖的工具名列表
```

**安装时检查**：`InstallWithProgress` 入口处调用 `p.RequiredPlugin()` → 对每个依赖调用 `reg.Get` 并检查 installPath 是否存在。不满足则返回 error，安装中止。

**卸载时检查**：`Uninstall` 入口遍历 `reg.All()` → 检查是否有其他工具依赖当前工具且已安装。被依赖则返回 error，卸载中止。

**manifest YAML 声明示例**：
```yaml
name: flutter
required_plugins: [java, android-sdk-tools]
```

### 13.3 Lockfile 命令接口

```go
func newLockCmd() *cobra.Command // internal/cli/lock.go
```

| 子命令/标志 | 行为 |
|-----------|------|
| `xdev lock` | 读取 .xdev.toml → 解析版本 → 写入 .xdev.lock |
| `xdev lock go python` | 仅重新生成指定工具条目 |
| `xdev lock --check` | 校验 .xdev.lock 与 .xdev.toml 的一致性 |
| `xdev lock --json` | JSON 格式输出检测结果 |

---

> **文档版本**: 2026-07-08 | **覆盖代码版本**: 1.2.2-alpha commit（本次会话所有改动）
