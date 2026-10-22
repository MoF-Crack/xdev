# XDev 安装指南

> 跨平台开发环境版本管理工具，支持 CLI、TUI 和 GUI 三种操作模式

## 系统要求

| 平台 | 最低版本 | 架构 |
|------|----------|------|
| Windows 10/11 | 1809+ | x86_64, arm64 |
| macOS | 12+ (Monterey) | x86_64, arm64 |
| Ubuntu | 20.04+ | x86_64, arm64 |
| 其他 Linux | glibc 2.28+ | x86_64, arm64 |

## 1. 下载预编译二进制

从 GitHub Releases 页面下载最新版本：

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
# 将 xdev.app 拖入 Applications 文件夹
# 弹出 DMG
hdiutil detach /Volumes/XDev
```

## 5. 从源码构建

### 前置要求

- Go 1.25+（见 `go.mod`）
- Git
- Node.js 18+ + npm（前端资源由 `//go:embed all:dist` 嵌入）
- （可选）Wails CLI：仅用于生成绑定或 `wails build`

### 构建单二进制

```bash
git clone https://github.com/xmdemt/xdev
cd xdev

# 先构建前端资源（否则 embed 失败）
cd frontend && npm ci && npm run build && cd ..

# 单二进制（CLI/TUI/GUI/shim；ADR-0006）
# GUI 必须带 Wails 标签：desktop,production。缺标签的二进制启动 GUI 会弹
# "Wails applications will not build without the correct build tags"。
go build -tags desktop,production -ldflags "-s -w" -o build/xdev .
```

### 构建 GUI（可选）

```bash
# 安装 Wails CLI
go install github.com/wailsapp/wails/v2/cmd/wails@latest

# wails 会编译前端并嵌入，产物仍是同一个 xdev 二进制
wails build -ldflags "-s -w"
```

`wails build` 只负责前端构建与资源嵌入，产物名由 `wails.json` 决定（`xdev`）。

### 构建脚本

项目提供构建脚本简化流程：

```bash
# Windows
.\build.ps1

# Linux / macOS
./build.sh
```

可选参数：
- `-SkipBuild`：跳过编译（`build.ps1`）
- `-SkipAudit`：跳过下载源审计

## 6. 验证安装

```bash
# 查看版本
xdev --version

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
