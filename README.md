# XDev — 开发环境管理器

> 跨平台开发环境版本管理工具，支持 CLI、TUI 和 GUI 三种操作模式
>
> **当前版本：0.3.2** · 命令名：`xdev` · 模块路径：`github.com/xmdemt/xdev`
>
> 仓库：[github.com/MoF-Crack/xdev](https://github.com/MoF-Crack/xdev)

## 核心特性

- **零依赖** —— 单一可执行文件，无需安装运行时
- **跨平台** —— Windows 10/11（主要）、macOS 12+、Ubuntu 20.04+
- **多版本管理** —— 同时安装同一工具的多个版本，一键切换
- **自动检测** —— Host-scan 扫描 PATH 上的全部本地实例（跳过 xdev shim）
- **本地迁移** —— 将本机已装工具迁移到 xdev 管理，包管理器可在 xdev / 本地实例间切换
- **Shim 调度** —— 单二进制多入口，PATH 中只需一个条目；Windows 优先硬链接
- **断点续传** —— 下载支持暂停/继续/取消，SHA-256 校验；嗅探版本走 TOFU
- **多镜像源** —— 内置阿里云、清华、npmmirror、goproxy.cn 等国内源
- **Profile 导入导出** —— 一键备份和恢复开发环境配置
- **三级环境变量** —— XDev 级 / 用户级 / 系统级统一管理
- **远程嗅探** —— GitHub / JSON / HTML / ArchivePattern 四种嗅探器发现新版本
- **形态区分** —— CLI / TUI / GUI 多包工具安装前选择形态
- **易扩展** —— 新增工具只需一份 YAML manifest，Generic 插件自动注册

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

或直接运行 GUI（单二进制多态入口）：

```bash
xdev            # 无参数 → 启动 GUI 桌面应用
xdev tui        # 显式启动 TUI
xdev gui        # 显式启动 GUI（等价无参数）
```

## 下载

从 [GitHub Releases](https://github.com/MoF-Crack/xdev/releases) 下载对应平台的产物：

| 产物 | 说明 |
|------|------|
| `xdev-windows-amd64-v0.3.2.zip` | Windows 单二进制（GUI+TUI+CLI） |
| `xdev-linux-*-v0.3.2.tar.gz` | Linux 单二进制（amd64 / arm64） |
| `xdev-darwin-*-v0.3.2.tar.gz` / `.dmg` | macOS 单二进制 |
| `xdev-0.3.2-linux-*.deb` / `.rpm` | Linux 软件包 |

每个平台只需一个可执行文件：无参数启动 GUI，`xdev tui` 进入 TUI，其余参数作为 CLI 命令。
本地构建产物位于 `build/bin/xdev-0.3.2.exe`（或 `build/bin/xdev`）。

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
| `xdev install <tool>[@<ver>]` | 下载并安装（`--force` / `--reinstall` 可强制重装） |
| `xdev use <tool>@<ver>` | 切换激活版本 |
| `xdev uninstall <tool>@<ver>` | 卸载指定版本 |
| `xdev upgrade [tool]` | 升级到清单最新版（`--all` / `--check` / `--cleanup`） |
| `xdev which <tool>` | 显示当前指向的路径 |
| `xdev env <tool>@<ver>` | 输出 eval 用的环境变量 |
| `xdev doctor` | 健康检查 |
| `xdev config list / get / set` | 配置管理 |
| `xdev config proxy` | 查看与设置下载代理 |
| `xdev self update` | 自更新 |
| `xdev self uninstall` | 移除 shim 与数据目录 |
| `xdev tui` | 启动 TUI 终端界面（单二进制：无参数运行 `xdev` 启动 GUI，`xdev tui` 显式进入 TUI） |
| `xdev gui` | 启动 GUI 图形界面 |
| `xdev refresh [<tool>]` | 刷新工具版本列表（`--all` 刷新全部） |
| `xdev sniff [--all] [--tool <name>] [--headless]` | 远程嗅探新版本 |
| `xdev updates` | 列出已安装工具中可用的版本升级 |
| `xdev sniff service install [--schedule <cron>]` | 安装系统定时嗅探任务（Win/Linux/macOS） |
| `xdev sniff service status` | 查看定时嗅探任务状态 |
| `xdev sniff service uninstall` | 移除系统定时嗅探任务 |

### 扩展命令

| 命令 | 说明 |
|------|------|
| `xdev hook --shell bash` | 输出当前目录的环境变量（供 cd hook eval） |
| `xdev activate [shell]` | 注入 cd hook 到 Shell RC |
| `xdev deactivate` | 移除 cd hook |
| `xdev isolate setup / teardown / exec` | 环境隔离（none / container / nix） |
| `xdev remote create / list / connect / destroy` | 远程开发环境（docker / ssh） |
| `xdev plugin list / install / create / remove` | 插件管理（内置 Generic + Lua + vfox 兼容） |
| `xdev manifest list / add / remove / update` | Manifest 仓库管理 |
| `xdev profile export / import` | 导出或导入环境配置 |
| `xdev keygen` | 生成 Ed25519 密钥对 |
| `xdev sign-manifests` | 为 manifest 签名 |
| `xdev version` | 打印版本 |

### 嗅探触发策略（配置在 `config.yaml` 的 `sniff:` 段）

| 策略 | 说明 |
|------|------|
| `interval`（默认） | 到设定周期后，下一次启动 xdev 时触发嗅探（惰性，无常驻进程） |
| `startup` | 每次启动 xdev 时后台异步嗅探（不阻塞主命令） |
| `service` | 安装系统定时任务（schtasks / cron / launchd），由系统服务定期调用 |
| `manual` | 纯手动，仅 `xdev sniff` 触发，无任何自动行为 |

```yaml
sniff:
  strategy: interval          # interval | startup | service | manual
  interval: 24h               # 嗅探间隔（仅 interval 策略）
  concurrency: 3              # 并发嗅探数
  rate_limit_per_hour: 60     # 每小时最大 API 请求数
  auto_archive_fill: true     # 自动为发现的版本生成占位 archive 条目
```

嗅探发现的新版本带 `unverified` 标记；安装前提示确认。占位 SHA-256（全零）跳过已签名清单的哈希核对，走 TOFU；已签名的真实哈希仍强制校验。

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
│   ├── python.exe  ← xdev 硬链接或副本
│   ├── node.exe    ← xdev 硬链接或副本
│   └── ...
├── cache/                # 下载缓存
└── manifests/            # 工具元数据（YAML）
```

当你执行 `python --version` 时：

1. Shell 在 PATH 中找到 `E:\XDev\shims\python.exe`（xdev 二进制的硬链接或副本）
2. xdev 检测到 `argv[0]` 是 `python`，进入 shim 调度
3. 查找 `current/python` 找到当前激活版本的安装路径
4. 通过 `os.Exec` 调用真正的 `python.exe`，透传所有参数和 I/O

Host-scan 会列出 PATH 上的每一个实例（本机 leftover 与 xdev 管理副本并存），而不是只认 `LookPath` 的第一个命中。包管理器详情里可在 xdev 实例与本地实例之间切换。

## 支持的工具（52 个 catalog + 本机包管理器）

| 分类 | 工具 |
|------|------|
| 编程语言 | Java, Python, Node.js, Go, Rust, .NET, PHP, Ruby, Lua, Perl, Dart, Kotlin, Bun, Deno, Zig, Julia, Haskell, Scala, Erlang, TypeScript, 仓颉 |
| 移动开发 | Android SDK（platform-tools / build-tools / platforms / ndk / cmdline-tools / emulator）、Flutter |
| 构建工具 | Gradle, Maven, CMake, Make, Ninja, Git, LLVM/Clang, MinGW, sbt |
| 开发工具 | VS Code, Cursor, Zed, IntelliJ IDEA Community, PyCharm Community, GoLand, CLion, WebStorm, RubyMine, Claude Code, Codex, OpenCode, GitHub Copilot CLI, DSH（DeepSeek Harness，npm 托管安装） |
| 包管理器 | UV（catalog）；Scoop / Homebrew / Chocolatey 可自动安装；yarn / pnpm 经 xdev Node 的 corepack 或 `npm i -g` 启用；pipx 经 pip / uv 安装 |

GUI 工具目录按「编程语言 / 构建工具 / 开发工具 / 移动开发 / 系统工具 / 本地工具 / 包管理器」分组。安装、启用、更新在详情抽屉中操作。

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

## 从源码构建

需要 Go 1.25、Node 22（GUI 前端）和 [Wails v2](https://wails.io/) CLI。

```bash
git clone https://github.com/MoF-Crack/xdev.git
cd xdev

# Windows
.\build.ps1            # 单二进制（GUI+TUI+CLI）+ 下载源审计
.\build.ps1 -SkipAudit # 跳过链接审计

# Unix
./build.sh
# SKIP_AUDIT=1 ./build.sh
```

手动构建（单二进制模型）：

```bash
# 前端资源（GUI 的 //go:embed 依赖 frontend/dist）
cd frontend && npm ci && npm run build && cd ..

# 单二进制（含 GUI/TUI/CLI；等价 wails build）
wails build -o build/bin/xdev.exe
# 或先 npm build 后：
# go build -ldflags "-s -w" -o build/bin/xdev.exe .
```

产物行为：无参数 → GUI；`xdev tui` → TUI；其他参数 → CLI 命令。
版本字符串来自 `internal/version/version.go`。发布流水线会再注入 `GitCommit`、`BuildDate` 与 Ed25519 根公钥。

## 添加新工具

1. 在 `manifests/<tool>.yaml` 编写清单（参考 `manifests/python.yaml`）
2. 启动时 `RegisterAll` 会为 catalog 中每个可安装条目生成 Generic 插件（Java 除外，它有专用 `ArchiveFor`）
3. 可选：把工具名加入 `manifests/index.yaml` 对应分类，便于索引
4. `go build`

Lua / vfox 插件放到 `$XMDEV_HOME/plugins/`，无需改 Go 代码。

## 0.3.2 要点

相对 0.3.1：嗅探安装不再把占位 SHA 当成篡改；忽略列表测试不再污染生产缓存；安装确认不再卡 3 秒以上；包管理器多实例（D: leftover + E: xdev）同时列出并可切换；yarn/pnpm 优先走 xdev Node；详情抽屉统一安装/启用/更新入口。完整列表见 [`CHANGELOG.md`](CHANGELOG.md)。

## 开发路线

| 里程碑 | 内容 | 状态 |
|--------|------|------|
| **M1 核心** | CLI + Config + Manifest + Resolver + Downloader + Extractor + Shim + Doctor | ✅ |
| **M2 首批插件** | 编程语言运行时 | ✅ |
| **M3 Android** | Android SDK/NDK 全套 + Flutter | ✅ |
| **M4 TUI** | Bubble Tea 终端界面 | ✅ |
| **M5 GUI** | Wails v2 + Vue 3 桌面应用 | ✅ |
| **M6 完善** | 镜像测速、断点续传、Profile 导入导出、自更新 | ✅ |
| **0.3.x** | 工具嗅探、开发工具扁平分类、包管理器自动安装与多实例、形态区分、安全加固 | ✅ |
| **隔离 / 远程** | Isolator + Orchestrator 框架已落地，跨平台与生产化打磨中 | 🔶 |

更细的架构说明见 [`docs/architecture.md`](docs/architecture.md)，文档索引见 [`docs/README.md`](docs/README.md)。

## License

MIT
