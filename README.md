# XDev - 开发环境管理器

> 跨平台开发环境版本管理工具，支持 CLI、TUI 和 GUI 三种操作模式
>
> 命令名：`xdev` | 模块路径：`github.com/xmdemt/xdev`

## 核心特性

- **零依赖** —— 单一可执行文件，无需安装运行时
- **跨平台** —— Windows 10/11（主要）、macOS 12+、Ubuntu 20.04+
- **多版本管理** —— 同时安装同一工具的多个版本，一键切换
- **自动检测** —— 扫描系统中已安装的开发工具
- **本地迁移** —— 将本地安装的工具迁移到 xdev 管理
- **Shim 调度** —— 单二进制多入口，PATH 中只需一个条目
- **断点续传** —— 下载支持暂停/继续/取消，SHA-256 校验
- **多镜像源** —— 内置阿里云、清华、npmmirror、goproxy.cn 等国内源
- **Profile 导入导出** —— 一键备份和恢复开发环境配置
- **三级环境变量** —— XDev 级 / 用户级 / 系统级统一管理
- **易扩展** —— 插件协议标准化，新增一个工具 < 200 行 Go 代码
- **Lockfile 锁定** —— 精确版本 pinning，确保团队环境可复现
- **依赖检查** —— 工具间依赖声明与安装/卸载时自动校验
- **浅色主题** —— GUI 支持暗色/浅色主题切换，持久化用户选择
- **批量安装** —— 内置优化算法，支持高效批量安装多个工具

## 快速开始

```bash
# 1. 初始化（选择安装目录，创建配置）
xdev init

# 2. 安装工具
xdev install python@3.12
xdev install node@20
xdev install go@1.22
xdev install java@17

# 3. 切换版本
xdev use python@3.11

# 4. 验证
python --version
xdev doctor
```

或直接运行 GUI：

```bash
xdev-gui.exe
```

## 下载

从项目发布页面下载最新的可执行文件：

- `xdev-<version>.exe` — CLI 命令行工具
- `xdev-gui-<version>.exe` — GUI 图形界面工具

## 命令总览

### 核心命令

| 命令 | 说明 |
|------|------|
| `xdev init` | 初始化（创建目录与默认配置） |
| `xdev available` | 列出所有支持的工具 |
| `xdev search <keyword>` | 搜索工具 |
| `xdev list` | 列出已安装的工具及版本 |
| `xdev list <tool> --remote` | 列出该工具的远程可用版本 |
| `xdev current [tool]` | 显示当前激活版本 |
| `xdev install <tool>[@<ver>]` | 下载并安装 |
| `xdev install --from-local` | 从 `.xdev.toml` 批量安装 |
| `xdev install --locked` | 仅按 `.xdev.lock` 精确版本安装 |
| `xdev lock` | 从 `.xdev.toml` 重新生成 `.xdev.lock` |
| `xdev lock --check` | 校验 `.xdev.lock` 与 `.xdev.toml` 是否一致 |
| `xdev lock --json` | JSON 格式输出 lockfile 状态 |
| `xdev use <tool>@<ver>` | 切换激活版本 |
| `xdev uninstall <tool>@<ver>` | 卸载 |
| `xdev which <tool>` | 显示当前指向的路径 |
| `xdev env <tool>@<ver>` | 输出 eval 用的环境变量 |
| `xdev doctor` | 健康检查 |
| `xdev config list / get / set` | 配置管理 |
| `xdev self update` | 自更新 |
| `xdev tui` | 启动 TUI 终端界面 |
| `xdev gui` | 启动 GUI 图形界面 |
| `xdev refresh [<tool>]` | 刷新工具版本列表（自动嗅探发现新模式） |
| `xdev sniff [--all] [--tool <name>] [--headless]` | 远程嗅探新版本（4 种内置嗅探器） |
| `xdev updates` | 列出已安装工具中可用的版本升级 |
| `xdev sniff service install [--schedule <cron>]` | 安装系统定时嗅探任务（Win/Linux/macOS） |
| `xdev sniff service status` | 查看定时嗅探任务状态 |
| `xdev sniff service uninstall` | 移除系统定时嗅探任务 |

### 项目级与扩展命令

| 命令 | 说明 |
|------|------|
| `xdev local [tool@ver]` | 设置/显示项目级工具版本（写入 `.xdev.toml`） |
| `xdev local --unset <tool>` | 移除项目级版本 |
| `xdev hook --shell bash` | 输出当前目录的环境变量（供 cd hook eval） |
| `xdev activate [shell]` | 注入 cd hook 到 Shell RC（自动切换版本） |
| `xdev deactivate` | 移除 cd hook |
| `xdev task list` / `xdev run <name>` | 列出/运行 `.xdev.toml` 中定义的任务 |
| `xdev service list / start / stop / status` | 服务编排（数据库/缓存/Web 服务） |
| `xdev isolate setup / teardown / exec` | 环境隔离（none / container / nix） |
| `xdev remote ...` | 远程开发环境（docker / ssh adapter） |
| `xdev plugin list / install / create` | 插件管理（内置 + Lua + vfox 兼容） |
| `xdev manifest ...` | Manifest 管理 |
| `xdev ai ...` | AI 辅助（需本地 Ollama） |
| `xdev keygen` / `xdev sign-manifest` | Ed25519 密钥与 manifest 签名 |
| `xdev local <tool>[@<ver>]` | 设置/查看项目级工具版本 |
| `xdev hook <shell>` | 输出 shell cd hook 脚本 |
| `xdev activate <shell>` | 激活 cd hook（自动切换版本） |
| `xdev deactivate` | 停用 cd hook |
| `xdev task list / run <name>` | 任务管理 |
| `xdev run <name>` | 运行任务（简写） |
| `xdev service list/start/stop/status` | 服务编排 |
| `xdev isolate setup/teardown/exec` | 环境隔离 |
| `xdev remote create/list/connect/destroy` | 远程开发 |
| `xdev plugin list/install/create` | 插件管理 |
| `xdev ai suggest/diagnose/status` | AI 助手 |
| `xdev manifest list/add/remove/update` | Manifest 仓库管理 |
| `xdev ai run migrate` | 本地已安装工具扫描与迁移（复用 `internal/env/scan_local.go`，非 sniff 功能） |

### 嗅探触发策略（配置在 config.yaml 的 sniff: 段）

| 策略 | 说明 |
|------|------|
| `interval`（默认） | 到设定周期后，下一次启动 xdev 时触发嗅探（惰性，无常驻进程） |
| `startup` | 每次启动 xdev 时后台异步嗅探（不阻塞主命令） |
| `service` | 安装系统定时任务（schtasks / cron / launchd），由系统服务定期调用 |
| `manual` | 纯手动，仅 `xdev sniff` 触发，无任何自动行为 |

配置文件示例：
```yaml
sniff:
  strategy: interval          # interval | startup | service | manual
  interval: 24h               # 嗅探间隔（仅 interval 策略）
  concurrency: 3              # 并发嗅探数
  rate_limit_per_hour: 60     # 每小时最大 API 请求数
  auto_archive_fill: true     # 自动为发现的版本生成占位 archive 条目
```

## 版本规范

| 写法 | 含义 | 示例 |
|------|------|------|
| `latest` | 最新版本 | `xdev install python@latest` |
| `lts` | 长期支持版 | `xdev install node@lts` |
| `3.12.0` | 精确版本 | `xdev install python@3.12.0` |
| `^3.12` | semver 范围 | `xdev install python@^3.12` |
| `~3.12` | 仅 minor 内可升级 | `xdev install python@~3.12` |

## 工作原理（Shim 调度机制）

```
E:\XDev\
├── config.yaml           # 用户配置
├── xdev.exe              # xdev 二进制（shim 源）
├── install/              # 实际安装目录
│   ├── python/3.12.0/
│   ├── node/20.18.0/
│   └── ...
├── current/              # 当前激活版本（Junction/Symlink）
│   ├── python → install/python/3.12.0
│   ├── node   → install/node/20.18.0
│   └── ...
├── shims/                # Shim 目录（PATH 最优先）
│   ├── python.exe  ← xdev 二进制副本
│   ├── node.exe    ← xdev 二进制副本
│   └── ...
├── cache/                # 下载缓存
└── manifests/            # 工具元数据（YAML）
```

当你执行 `python --version` 时：

1. Shell 在 PATH 中找到 `E:\XDev\shims\python.exe`（xdev 二进制的副本）
2. xdev 检测到 `argv[0]` 是 `python`，进入 shim 调度
3. 查找 `current/python` 找到当前激活版本的安装路径
4. 通过 `os.Exec` 调用真正的 `python.exe`，透传所有参数和 I/O

## 支持的工具（38 个）

| 分类 | 工具 |
|------|------|
| 编程语言 | Java, Python, Node.js, Go, Rust, .NET, PHP, Ruby, Lua, Perl, Dart, Kotlin, Bun, Deno, Zig, Julia, Haskell, Scala, Erlang |
| 移动开发 | Android SDK (platform-tools/build-tools/platforms/ndk/cmdline-tools/emulator), Flutter |
| 构建工具 | Gradle, Maven, CMake, Make, Ninja |
| 系统工具 | Git, LLVM/Clang, MinGW |
| 包管理器 | UV, NPM, Yarn, PNPM, PIP, Cargo |

## 配置文件

`<XDev主目录>/config.yaml`：

```yaml
core:
  xmdev_dir: E:\XDev
  download_dir: E:\XDev\download
  install_dir: E:\XDev\install
  shim_dir: E:\XDev\shims
  cache_dir: E:\XDev\cache
  env_dir: E:\XDev\env
  manifest_dir: E:\XDev\manifests

path:
  mode: shim                       # shim | direct
  inject_to_windows_path: true

mirror:
  python: https://mirrors.aliyun.com/python
  node: https://npmmirror.com/mirrors/node
  go: https://goproxy.cn
  rust: https://rsproxy.cn
  android: https://mirrors.aliyun.com/android.googlesource.com
  flutter: https://storage.flutter-io.cn
  maven: https://mirrors.aliyun.com/maven

proxy:
  http: ""
  https: ""
  no_proxy: ""
  enabled: false
```

## 项目配置

在项目根目录创建 `.xdev.toml` 文件管理项目级工具版本：

```toml
[tools]
python = "3.12"
node = "20"

[env]
PYTHONPATH = "./src"

[tasks.build]
run = "npm run build"
desc = "构建项目"

[tasks.test]
run = "pytest"
dir = "./tests"
```

```bash
# 设置项目级工具版本
xdev local python@3.12

# 运行任务
xdev run build

# 从 lockfile 安装（确保团队环境一致）
xdev install --locked
```

Lockfile (`.xdev.lock`) 自动记录精确版本和校验和，确保团队开发环境可复现。

## 从源码构建

```bash
git clone https://github.com/xmdemt/xmdevenvmt
cd xmdevenvmt

# CLI 二进制
go build -ldflags "-s -w -X github.com/xmdemt/xdev/internal/version.Version=$(git describe --tags)" -o build/xdev.exe ./cmd/xmdevenvmt

# GUI 二进制（需要 Wails CLI）
wails build -ldflags "-X github.com/xmdemt/xdev/internal/version.Version=$(git describe --tags)"
```

## 添加新工具

1. 在 `manifests/<tool>.yaml` 中编写清单（参考 `manifests/python.yaml`）
2. 在 `internal/plugins/<tool>/<tool>.go` 中实现 `plugin.Plugin` 接口
3. 在 `internal/cli/register.go` 中注册：`r.Register(<tool>.New(t))`
4. `go build`

## 开发路线

| 里程碑 | 内容 | 状态 |
|--------|------|------|
| **M1 核心** | CLI + Config + Manifest + Resolver + Downloader + Extractor + Shim + Doctor | ✅ |
| **M2 首批插件** | 19 个编程语言运行时 | ✅ |
| **M3 Android** | Android SDK/NDK 全套 + Flutter | ✅ |
| **M4 TUI** | Bubble Tea 终端界面 | ✅ |
| **M5 GUI** | Wails v2 + Vue 3 桌面应用 | ✅ |
| **M6 完善** | 镜像测速、断点续传、Profile 导入导出、自更新 | ✅ |
| **Phase 1** | 项目级版本管理(.xdev.toml) + cd hook + Plugin扩展 + Resolver增强 | ✅ |
| **Phase 2** | Lockfile + Task runner + Lua 插件 + 远程版本发现 | ✅ |
| **Phase 3** | 服务编排 + vfox 兼容 + Manifest 仓库 | ✅ 服务编排/兼容层已完成；macOS/Linux 跨平台移植待启动 |
| **Phase 4** | 环境隔离(Isolator) + 远程开发(Orchestrator) + AI 集成 | 🔶 框架已落地(CLI+GUI)，跨平台与生产化打磨进行中 |

## License

MIT
