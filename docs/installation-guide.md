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
| `xdev-<version>-windows-amd64.exe` | Windows CLI 工具 |
| `xdev-gui-<version>-windows-amd64.exe` | Windows GUI 工具 |
| `xdev-<version>-linux-amd64` | Linux CLI 工具 |
| `xdev-gui-<version>-linux-amd64` | Linux GUI 工具 |
| `xdev-<version>-darwin-amd64` | macOS Intel CLI 工具 |
| `xdev-<version>-darwin-arm64` | macOS Apple Silicon CLI 工具 |
| `xdev-gui-<version>-darwin-amd64` | macOS Intel GUI 工具 |
| `xdev-gui-<version>-darwin-arm64` | macOS Apple Silicon GUI 工具 |

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

## 3. 通过 AppImage 安装（Linux GUI）

```bash
# 下载 AppImage
chmod +x xdev-gui-<version>-linux-amd64.AppImage
./xdev-gui-<version>-linux-amd64.AppImage
```

## 4. 通过 DMG 安装（macOS GUI）

```bash
# 挂载 DMG
hdiutil attach xdev-gui-<version>-darwin-amd64.dmg
# 将 xdev-gui.app 拖入 Applications 文件夹
# 弹出 DMG
hdiutil detach /Volumes/XDev
```

## 5. 从源码构建

### 前置要求

- Go 1.22+
- Git
- （构建 GUI 需要）Node.js 18+ + npm + Wails CLI

### 构建 CLI

```bash
git clone https://github.com/xmdemt/xmdevenvmt
cd xmdevenvmt

# CLI 二进制（Spec 030：根入口，默认不链接 Wails）
go build -ldflags "-s -w -X github.com/xmdemt/xdev/internal/version.Version=$(git describe --tags)" -o build/xdev.exe .
```

### 构建 GUI

```bash
# 安装 Wails CLI
go install github.com/wailsapp/wails/v2/cmd/wails@latest

# 构建 GUI（需要 Node.js + npm 前端构建）
wails build -ldflags "-X github.com/xmdemt/xdev/internal/version.Version=$(git describe --tags)"
```

GUI 构建步骤会自动编译 `frontend/` 目录下的 Vue.js 项目，产出 `xdev-gui` 二进制文件。

### 构建脚本

项目提供构建脚本简化流程：

```bash
# Windows
.\build.ps1

# Linux / macOS
./build.sh
```

可选参数：
- `-SkipFrontend`：跳过前端构建
- `-SkipCLI`：跳过 CLI 构建
- `-SkipGUI`：跳过 GUI 构建
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
