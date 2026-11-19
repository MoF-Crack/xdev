# XDev — 开发环境管理器

> 跨平台开发环境版本管理工具，支持 CLI、TUI 和 GUI 三种操作模式
>
> **版本：1.0.0-rc.1（RC1，待 CI 发布）** · 命令名：`xdev` · 模块路径：`github.com/xmdemt/xdev`
>
> 仓库：[github.com/MoF-Crack/xdev](https://github.com/MoF-Crack/xdev)

## 核心特性

- **单文件分发** —— Go 业务代码与前端资源合并；桌面模式使用系统 WebView，Linux 需要 GTK/WebKitGTK 系统库
- **跨平台** —— Windows 10/11（主要）、macOS 12+、Ubuntu 20.04+
- **多版本管理** —— 同时安装同一工具的多个版本，一键切换
- **项目级版本** —— `.xdev.yaml` 声明项目所需版本，shim 按当前目录自动解析（无需来回 `xdev use`）
- **环境变量注入** —— 激活版本的工具环境变量注入子进程，Windows / macOS / Linux 一致生效
- **自动检测** —— Host-scan 扫描 PATH 上的全部本地实例（跳过 xdev shim）
- **本地迁移** —— 将本机已装工具迁移到 xdev 管理，包管理器可在 xdev / 本地实例间切换
- **Shim 调度** —— 单二进制多入口，PATH 中只需一个条目；Windows 优先硬链接
- **断点续传** —— 下载支持暂停/继续/取消，SHA-256 校验；嗅探版本走 TOFU
- **多镜像源** —— 内置阿里云、清华、npmmirror、goproxy.cn 等国内源
- **Profile 导入导出** —— 一键备份和恢复开发环境配置
- **三级环境变量** —— XDev 级 / 用户级 / 系统级统一管理
- **远程嗅探** —— GitHub / JSON / HTML / ArchivePattern 四种嗅探器发现新版本
- **形态区分** —— CLI / TUI / GUI 多包工具安装前选择形态
- **包管理器管理** —— `xdev pm list/install`，定义数据化到签名覆盖的 `manifests/package-managers.yaml`
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

`1.0.0-rc.1` 正在准备发布，目标 Tag 为 `v1.0.0-rc.1`，计划标记为 GitHub **Pre-release**。本地修复验证已完成，跨平台发布 CI、签名、打包及公开资产验收待完成；下表是预期的 11 项资产，尚不代表可下载。最新状态见 [RC1 发布说明](<docs/releases/1.0.0-rc.1.md#发布验证>)，公开资产发布后可从 [GitHub Releases](https://github.com/MoF-Crack/xdev/releases) 获取。目前可下载的版本仍为 [1.0.0-beta.3](https://github.com/MoF-Crack/xdev/releases/tag/v1.0.0-beta.3)。

RC1 修复 Windows GUI 启动留下控制台，以及自更新未采用应用代理配置的问题。Windows GUI 在文件日志就绪后释放自身控制台关联，CLI、TUI 与 shim 保留标准输入输出；自更新每次操作使用一份代理配置快照，元数据、归档、`SHA256SUMS` 与资产 API 回退共用该次网络配置。

| 预期发布产物 | 说明 |
|------|------|
| `xdev-windows-amd64-v1.0.0-rc.1.zip` | Windows 单二进制（GUI+TUI+CLI） |
| `xdev-linux-amd64-v1.0.0-rc.1.tar.gz` | Linux 单二进制（amd64） |
| `xdev-linux-arm64-v1.0.0-rc.1.tar.gz` | Linux 单二进制（arm64） |
| `xdev-darwin-amd64-v1.0.0-rc.1.tar.gz` | macOS 单二进制（Intel） |
| `xdev-darwin-arm64-v1.0.0-rc.1.tar.gz` | macOS 单二进制（Apple Silicon） |
| `xdev-1.0.0-rc.1-x86_64.AppImage` | Linux AppImage（amd64） |
| `xdev-1.0.0-rc.1-aarch64.AppImage` | Linux AppImage（arm64） |
| `xdev-darwin-arm64-v1.0.0-rc.1.dmg` | macOS（Apple Silicon） |
| `xdev-1.0.0-rc.1-linux-amd64.deb` | Linux Debian 软件包（amd64） |
| `xdev-1.0.0-rc.1-linux-amd64.rpm` | Linux RPM 软件包（amd64） |
| `SHA256SUMS` | 汇总所有平台资产的完整校验和 |

每个平台只需一个可执行文件：无参数启动 GUI，`xdev tui` 进入 TUI，其余参数作为 CLI 命令。
本地构建产物位于 `build/bin/xdev.exe`（或 `build/bin/xdev`）；本地测试包不属于公开 Release 资产。

## 旧版升级和自更新

RC1 资产公开且校验和齐备后，**beta.3 的 Release 列表更新器可以发现它，无需将 RC1 改为非 Pre-release 或 Latest**。beta.3 用户可在设置中点击「检查更新」，或退出 GUI 后运行 `xdev self update`。手动检查不受启动检查的 24 小时节流限制；发现更新后选择「下载并安装」，即可查看真实下载字节及校验、解压、安装阶段，也可单独下载归档。

**安装 RC1 之前，beta.3 自更新仍不读取 GUI 设置中的代理。** 受限网络下，即使设置页已配置代理，更新检查或下载仍可能失败；此时可使用标准环境代理，或待 RC1 发布后从 [GitHub Releases](https://github.com/MoF-Crack/xdev/releases) 手动下载对应平台归档。RC1 的应用代理修复在运行新程序后生效。

较早的 alpha / beta.2 仍只读取 GitHub `/releases/latest`，目前该入口指向 [beta.3](https://github.com/MoF-Crack/xdev/releases/tag/v1.0.0-beta.3)。可先升级到 beta.3，再由新版更新器升级 RC1；也可在 RC1 发布后直接手动下载 RC1。beta.3 的 GitHub Latest 兼容标记继续保留，`beta` 版本号在 SemVer 中仍表示预发布。

手动升级时，使用同一 Release 的 `SHA256SUMS` 核对下载文件，将归档解压到单独目录，退出旧 XDev 后再运行或替换可执行文件，保留原配置目录与已安装工具。下载或校验失败不会覆盖原二进制；安装成功后请自行关闭并重新打开 XDev，当前进程在重新启动前仍运行旧版代码。完整步骤和验证边界见 [RC1 发布说明](<docs/releases/1.0.0-rc.1.md>)；历史升级记录见 [beta.3 发布说明](<docs/releases/1.0.0-beta.3.md>)。

## 新版 GUI 与品牌（1.0.0-beta.2）

<img src="docs/assets/brand/xdev-wordmark.svg" alt="XDev F2 流弧单 X 一体字标（浅色背景用）" width="269" />

<img src="docs/assets/brand/xdev-app-icon.svg" alt="XDev 应用图标：靛紫圆角底与白色流弧 X" width="80" />

F2 字标由一个流弧 **x** 与 **dev** 一体组成，不再叠加独立 X 或重复品牌文字。上方静态字标供浅色背景使用；应用图标固定为靛紫底、白色流弧 X，不随主题变色。

- **三主主题**：护眼·雾松、亮色·冰川、暗色·深空；顶部可快速切换，设置页提供三张主题主卡。
- **经典主题兼容**：午夜、极夜、余烬仍可选择，保留已有主题偏好。
- **响应式工作区**：宽度 ≥ 1280 px 时，工具列表与非遮挡详情面板并排；更窄窗口使用详情抽屉。
- **操作契约不变**：当前工具链、工具目录筛选 tabs（全部 / 已安装 / 本地）与安装会话契约沿用既有行为。
- **原生图标接入**：Windows 构建将多尺寸 ICO 实际嵌入可执行文件；macOS DMG 包装为带 ICNS 的 `XDev.app`；Linux 桌面入口使用应用图标。对应平台 CI 打包已通过；macOS/Linux 真机 GUI 启动尚未执行，不以打包成功代替真机体验验证。

以下截图均使用**测试示例数据**，不代表用户真实环境、已安装工具或本机路径。

### 亮色·冰川

![亮色·冰川工作区（测试示例数据）](<docs/assets/screenshots/workspace-light.png>)

### 护眼·雾松

![护眼·雾松工作区（测试示例数据）](<docs/assets/screenshots/workspace-eye-care.png>)

### 暗色·深空

![暗色·深空工作区（测试示例数据）](<docs/assets/screenshots/workspace-dark.png>)

本次变更、兼容范围与发布验证状态见 [1.0.0-beta.2 发布说明](<docs/releases/1.0.0-beta.2.md>)。

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
| `xdev hook --shell bash` | 输出当前目录的环境变量（供 cd hook eval；按 `.xdev.yaml` 解析） |
| `xdev activate [shell]` | 注入 cd hook 到 Shell RC |
| `xdev deactivate` | 移除 cd hook |
| `xdev pm list [--all]` | 列出包管理器及状态（installed / installable / 需 xdev install） |
| `xdev pm install <name> [--dry-run]` | 执行包管理器官方安装脚本（scoop / brew / choco） |
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

## 项目级版本（`.xdev.yaml`）

在项目根放一份 `.xdev.yaml`，声明该项目需要的工具版本：

```yaml
# 项目根/.xdev.yaml
tools:
  python: "3.11"
  node: "^20"
  go: latest
```

从当前目录逐级向上查找（最多 64 层），`shim` 优先按它解析版本：

```bash
cd ~/proj-a && python --version   # 3.11.x（项目声明）
cd ~/proj-b && python --version   # 3.12.x（另一个项目的声明）
```

要点：

- 版本规格复用下方「版本规范」的语义（`latest` / `lts` / `3.12` / `^3.12` / `~3.11`）
- **只在本机已安装集合上解析**，不联网——不会因为你执行 `python script.py` 就突然开始下载
- 声明的版本未安装时，回落全局 `current` 并在 stderr 提示一次
- **无项目配置时行为与之前完全一致**（静默回落，无额外开销）

配合 `path.mode: auto` 或 `xdev activate` 使用 cd hook 时，`xdev hook` 会输出
项目环境的 PATH 与环境变量供 shell eval。

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

GUI 工具目录按「编程语言 / 构建工具 / 开发工具 / 移动开发 / 系统工具 / 本地工具 / 包管理器」分组。安装、启用、更新在详情面板或窄屏详情抽屉中操作。

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
  mode: shim                       # shim | direct | auto（auto = shim + cd hook 项目级切换）
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

需要 Go 1.27+、Node 22+（GUI 前端）和固定版本的 [Wails 3](https://wails.io/) CLI：

```bash
# Windows / macOS
go install github.com/wailsapp/wails/v3/cmd/wails3@v3.1.0
# Linux：先安装 GTK3 / WebKitGTK 4.1 开发库，再编译生成器
go install -tags gtk3 github.com/wailsapp/wails/v3/cmd/wails3@v3.1.0
```

Linux 桌面构建使用 GTK3 + WebKitGTK 4.1 开发库；桌面运行依赖系统 WebView。

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

# 单二进制（GUI/TUI/CLI/shim），使用共享构建与公钥注入流程
node scripts/build.mjs --skip-audit
# 已有前端资源时可直接编译；Linux 标签额外加 gtk3：
# go build -tags desktop,production -o build/bin/xdev.exe .
# 无 desktop 标签用于开发维护命令，不是发布用桌面产物。
```

产物行为：无参数 → GUI；`xdev tui` → TUI；其他参数 → CLI 命令。
版本字符串来自 `internal/version/version.go`。发布流水线会再注入 `GitCommit`、`BuildDate` 与 Ed25519 根公钥。

## 添加新工具

1. 在 `manifests/<tool>.yaml` 编写清单（参考 `manifests/python.yaml`）
2. 启动时 `RegisterAll` 会为 catalog 中每个可安装条目生成 Generic 插件（Java 除外，它有专用 `ArchiveFor`）
3. 可选：把工具名加入 `manifests/index.yaml` 对应分类，便于索引
4. `go build`

Lua / vfox 插件放到 `$XMDEV_HOME/plugins/`，无需改 Go 代码。

## 1.0.0-beta.1 历史要点

以下保留 beta.1 README 的功能要点，作为本次 UI 更新之前的能力背景：

- **项目级多版本**：`.xdev.yaml` 声明项目所需版本，shim 按 CWD 自动解析，未命中回落全局
- **环境变量首次在非 Windows 生效**：env 从「写注册表」改为「注入子进程环境」（`internal/envctx`）
- **`path_append`**：修复 `go install` / `cargo install` 装完找不到工具
- **`xdev pm`**：包管理器列表与安装，定义数据化到签名覆盖的 YAML
- **安装编排单一化**：GUI 不再自写一套，统一走 `installer.InstallWithProgress`
- **`Activate` plan/apply**：修复已失效的回滚记账（`_ = junctionCreated` 类死变量）
- 修复 POSIX PATH 单引号导致 `$PATH` 被覆盖、shim 频繁弹终端窗口、`ANDROID_HOME` 指向错误等

完整列表见 [`CHANGELOG.md`](CHANGELOG.md)。

## 开发路线

| 里程碑 | 内容 | 状态 |
|--------|------|------|
| **M1 核心** | CLI + Config + Manifest + Resolver + Downloader + Extractor + Shim + Doctor | ✅ |
| **M2 首批插件** | 编程语言运行时 | ✅ |
| **M3 Android** | Android SDK/NDK 全套 + Flutter | ✅ |
| **M4 TUI** | Bubble Tea 终端界面 | ✅ |
| **M5 GUI** | Wails 3 + Vue 3 桌面应用（见 ADR-0007） | ✅ |
| **M6 完善** | 镜像测速、断点续传、Profile 导入导出、自更新 | ✅ |
| **0.3.x** | 工具嗅探、开发工具扁平分类、包管理器自动安装与多实例、形态区分、安全加固 | ✅ |
| **隔离 / 远程** | Isolator + Orchestrator 框架已落地，跨平台与生产化打磨中 | 🔶 |

更细的架构说明见 [`docs/architecture.md`](docs/architecture.md)，文档索引见 [`docs/README.md`](docs/README.md)。

## License

MIT
