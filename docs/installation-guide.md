# XDev 安装指南

> 跨平台开发环境版本管理工具，支持 CLI、TUI 和 GUI 三种操作模式

## 系统要求

| 平台 | 最低版本 | 架构 |
|------|----------|------|
| Windows | Windows 10/11，桌面界面需要 WebView2 Runtime | amd64 发布产物 |
| macOS | 需要与构建目标兼容的 macOS；本轮尚未本地验证 | amd64、arm64 构建目标 |
| Linux | 桌面构建依赖 GTK3、WebKitGTK 4.1 及构建产物所需的系统库 | amd64、arm64 构建目标 |

不要仅凭 glibc 版本推断桌面二进制可运行。最低系统版本应以该次发布实际验证的平台为准。本轮集成验证已通过 Windows desktop 构建、原生 WebView2 GUI 渲染/绑定调用、临时 home 的 GetConfig 与正常退出；Linux manifest/plugin/gui/installer 全包测试及 `desktop,production,gtk3` 构建通过。macOS 尚未实机验证，Linux 原生窗口交互也不由上述构建结果保证。

## 1. 下载预编译二进制

从 [GitHub Releases](https://github.com/MoF-Crack/xdev/releases) 选择发布版本，并下载与系统匹配的归档。以下是发布流程的产物命名约定；以该版本实际提供的附件为准：

> **注意**：建议至少 20 GiB 磁盘空间用于安装开发工具（特别是 Android SDK）。

| 文件 | 说明 |
|------|------|
| `xdev-windows-amd64-v<version>.zip` | Windows（CLI/TUI/GUI 单文件） |
| `xdev-linux-amd64-v<version>.tar.gz` | Linux x86_64 |
| `xdev-linux-arm64-v<version>.tar.gz` | Linux arm64 |
| `xdev-darwin-amd64-v<version>.tar.gz` | macOS Intel |
| `xdev-darwin-arm64-v<version>.tar.gz` | macOS Apple Silicon |
| `xdev-<version>-linux-amd64.deb` / `.rpm` | Debian / Fedora 安装包 |
| `xdev-<version>-x86_64.AppImage` | Linux 便携包 |
| `xdev-darwin-arm64-v<version>.dmg` | macOS 安装镜像 |
| `SHA256SUMS` | 全部产物的校验和 |

> 0.3.3 起为**单二进制**（ADR-0006）：CLI、TUI、GUI 与所有工具 shim 都是同一个
> `xdev[.exe]`，不再有 `xdev-gui-*` 产物。无参数运行即启动 GUI。

下载后将二进制文件放到 PATH 中的任意目录，或直接运行：

```bash
# Windows (PowerShell)
> xdev.exe init

# Linux / macOS
$ chmod +x xdev
$ ./xdev init
```

## 2. 通过包管理器安装（Linux）

### Debian / Ubuntu

```bash
# 添加仓库（如已配置）
sudo apt update
sudo apt install xdev
```

### Fedora / RHEL

```bash
sudo dnf install xdev
```

或直接下载 `.deb` / `.rpm` 包从 Releases 页面安装。

## 3. 通过 AppImage 安装（Linux）

```bash
# 下载 AppImage
chmod +x xdev-<version>-x86_64.AppImage
./xdev-<version>-x86_64.AppImage        # 无参数 → 启动 GUI
./xdev-<version>-x86_64.AppImage list   # 带参数 → CLI
```

## 4. 通过 DMG 安装（macOS）

```bash
# 挂载 DMG
hdiutil attach xdev-darwin-arm64-v<version>.dmg
# 当前镜像内为 xdev 可执行文件，不是 .app bundle
# 将 /Volumes/XDev/xdev 复制到自己选择的目录，再运行或加入 PATH
# 弹出 DMG
hdiutil detach /Volumes/XDev
```

## 5. 从源码构建

### 前置要求

- Go 1.27.0 或更高，最低版本以 `go.mod` 为准；本轮 Windows 使用 1.27.3。
- Git、Node.js 22 和 npm（与当前 CI 工具链一致）。
- 固定版本 Wails 3 绑定生成器：

```sh
# Windows / macOS
go install github.com/wailsapp/wails/v3/cmd/wails3@v3.1.0
# Linux：先安装下面的 GTK3 / WebKitGTK 4.1 开发库
go install -tags gtk3 github.com/wailsapp/wails/v3/cmd/wails3@v3.1.0
```

确保 `wails3` 在 PATH 中，或用 `WAILS3` 环境变量指定路径。`GO`、`NPM` 同样可指定真实工具路径，避免调用 XDev shim 造成构建工具选择不明确。

Linux 当前使用 GTK3 后端。在提供对应包的 Debian/Ubuntu 系统安装：

```sh
sudo apt-get update
sudo apt-get install -y libgtk-3-dev libwebkit2gtk-4.1-dev
```

macOS 需要 Xcode Command Line Tools；本轮尚未本地运行 macOS 构建。Windows 桌面运行需要 WebView2 Runtime。

### 构建正式单二进制

项目使用 `wails.yaml` 描述 Wails v3 配置，旧版 `wails.json` 不再作为依据。配置保留原生开发入口，但本轮未验证直接 `wails3 dev`；直接 `wails3 build` 不保证执行项目的公钥注入、签名检查及发布打包流程。

在仓库根目录执行正式项目构建入口：

```sh
node scripts/build.mjs
```

该入口依次进行下载源审计、Wails 绑定生成、`npm ci --ignore-scripts`、前端构建和 Go 编译。绑定生成使用 `desktop,bindings` 条件，不要求预先存在 dist；正式构建去掉 bindings 并嵌入真实前端资源。生成绑定位于 `frontend/bindings`，无需手动维护旧版桥接目录。

正式产物为 `build/bin/xdev.exe`（Windows）或 `build/bin/xdev`（Unix），同目录另有版本化副本。Windows/macOS 正式标签为 `desktop,production`，Linux 为 `desktop,production,gtk3`；仍是 CLI/TUI/GUI/shim 共用的一个二进制。

平台包装脚本调用同一个 Node 入口：

```powershell
# Windows PowerShell
.\build.ps1
.\build.ps1 -SkipBuild  # 仅执行审计，跳过构建
.\build.ps1 -SkipAudit  # 跳过下载源审计，执行构建
```

```sh
# Linux / macOS
./build.sh
./build.sh --skip-audit
```

`build.ps1` 只接受 `-SkipBuild` 和 `-SkipAudit` 两个开关。Node 入口的其他参数须直接传给 `node scripts/build.mjs`，不要作为 PowerShell 包装脚本参数使用。

### 无桌面依赖的维护构建

签名等维护命令可以从干净检出直接构建，不要求前端 dist：

```sh
go build -o xdev-maintenance .
./xdev-maintenance version
```

不带 desktop 标签的产物包含 GUI stub，调用 GUI 会返回明确错误。它用于开发测试及维护命令，不能作为完整桌面发布产物分发。

独立更新绑定时：

```sh
wails3 generate bindings -f '-tags desktop,bindings' -ts -d frontend/bindings ./internal/gui
# Linux 使用 -tags desktop,bindings,gtk3
```

发布构建由 CI 先生成一份统一签名输入，再供所有平台使用。`node scripts/build.mjs --release` 会要求公钥并强制检查清单签名覆盖；缺少签名不能以跳过测试的方式发布。普通本地构建不能代替生产签名流程。

## 6. 验证安装

```bash
# 查看版本
xdev version

# 运行健康检查
xdev doctor
```

`xdev doctor` 将检查：
- XDev 主目录是否存在且可写
- 所有必要子目录是否创建（install/、shims/、cache/、downloads/、env/、manifests/）
- Shim 目录是否在 PATH 中
- Shell RC 是否注入了 cd hook
- 已安装工具的 shim 是否正确
- Manifest 目录是否有有效工具
- 用户和系统 PATH 是否有问题

检测到问题时，运行自动修复：

```bash
xdev doctor --fix
```

## 7. Shell 集成

安装后，运行初始化向导完成 Shell 集成：

```bash
xdev init
```

初始化向导将：
1. 选择 XDev 主目录（Windows 上推荐非系统盘，如 D:\XDev）
2. 创建目录结构并写入默认配置
3. 交互式选择要安装的工具
4. 注入 Shell RC cd hook（自动切换版本）
5. （Windows）将 shim 目录添加到用户 PATH 注册表

或手动集成：

```bash
# 注入 cd hook
xdev activate bash  # bash / zsh / fish / pwsh

# 移除 cd hook
xdev deactivate
```

## 下一步

- 运行 `xdev doctor` 验证安装
- 运行 `xdev available` 查看所有支持的工具
- 运行 `xdev install python@3.12` 安装第一个工具
- 阅读 [使用手册](usage-manual.md) 了解完整功能
