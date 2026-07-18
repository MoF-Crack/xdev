> **文档类型**：长期参考文档
> **状态**：Living
> **来源**：docs/technical-reference.md
> **P4B 迁移目标**：原位保留
> **替代文档**：无
> **治理记录**：P4A 元数据治理，2026-07-16

# XDev 技术文档

> 跨平台多语言开发环境版本管理器 — 技术原理与架构详解

## 目录

- [1. 项目概览](#1-项目概览)
- [2. 架构设计](#2-架构设计)
- [3. 核心机制](#3-核心机制)
  - [3.1 Shim 调度机制](#31-shim-调度机制)
  - [3.2 版本切换机制](#32-版本切换机制)
  - [3.3 环境变量管理](#33-环境变量管理)
  - [3.4 下载管理](#34-下载管理)
- [4. 安装工作流](#4-安装工作流)
- [5. 插件系统](#5-插件系统)
- [6. 镜像系统](#6-镜像系统)
- [7. 版本解析](#7-版本解析)
- [8. 远程版本发现](#8-远程版本发现)
- [9. 解压引擎](#9-解压引擎)
- [10. PATH 管理](#10-path-管理)
- [11. GUI 架构](#11-gui-架构)
  - [11.1 Wails 绑定生成机制](#111-wails-绑定生成机制)
  - [11.2 App 拆分架构](#112-app-拆分架构)
  - [11.3 强类型返回值](#113-强类型返回值)
  - [11.4 性能优化](#114-性能优化)
  - [11.5 资源安全](#115-资源安全)
  - [11.6 util 共享包](#116-util-共享包)
- [12. 目录结构](#12-目录结构)
- [13. 数据流图](#13-数据流图)

---

## 1. 项目概览

**XDev** 是一个统一的开发环境版本管理器，类似于 nvm/fnm/pyenv/rustup 的合体，但用一个工具管理所有开发语言的运行时版本。

| 属性 | 值 |
|------|-----|
| 语言 | Go |
| CLI 框架 | Cobra |
| GUI 框架 | Wails v2.12.0 (Go + WebView2) |
| 前端 | Vue 3 + Naive UI + Vite |
| 模块路径 | `github.com/xmdemt/xdev` |
| 支持平台 | Windows (主要) / macOS / Linux |
| 支持工具数 | 38 个 |

**核心价值**：一个二进制管理所有工具版本，PATH 中只需一个条目。

---

## 2. 架构设计

```
┌─────────────────────────────────────────────────────┐
│                    用户交互层                         │
│  ┌──────────┐  ┌──────────┐  ┌──────────────────┐  │
│  │ CLI      │  │ TUI      │  │ GUI (Wails+Vue)  │  │
│  │ (Cobra)  │  │ (Bubble  │  │                  │  │
│  │          │  │  Tea)    │  │                  │  │
│  └────┬─────┘  └────┬─────┘  └────────┬─────────┘  │
│       │              │                 │             │
├───────┼──────────────┼─────────────────┼─────────────┤
│       │         核心服务层              │             │
│  ┌────▼─────────────▼─────────────────▼──────────┐  │
│  │              Installer (编排器)                │  │
│  │  Resolve → Download → Verify → Extract →      │  │
│  │  Activate (Junction + Shim + EnvVar)           │  │
│  └──┬──────┬───────┬──────┬──────┬──────┬───────┘  │
│     │      │       │      │      │      │           │
│  ┌──▼──┐┌──▼──┐┌───▼──┐┌──▼──┐┌──▼──┐┌──▼────┐    │
│  │Plugin││Mirror││Down- ││Extr-││Shim ││Env    │    │
│  │Reg.  ││Table ││loader││actor││Disp.││Manager│    │
│  └─────┘└─────┘└──────┘└─────┘└─────┘└───────┘    │
│                                                     │
├─────────────────────────────────────────────────────┤
│                   基础设施层                         │
│  ┌──────────┐  ┌──────────┐  ┌──────────────────┐  │
│  │ Config   │  │ Manifest │  │ Win Registry     │  │
│  │ (YAML)   │  │ (Embed)  │  │ (Syscall)        │  │
│  └──────────┘  └──────────┘  └──────────────────┘  │
└─────────────────────────────────────────────────────┘
```

---

## 3. 核心机制

### 3.1 Shim 调度机制

Shim 是 XDev 最核心的设计——**单二进制多入口调度**。

#### 设计原理

传统做法是为每个工具创建独立的 shim 脚本（如 `.cmd` 文件），但这导致：
- 大量小文件散落在 shims 目录
- 每个脚本需要独立维护
- `.cmd` 脚本无法正确处理 stderr 输出

XDev 的方案：**xdev 二进制本身也是所有工具的 shim**。通过将 xdev 二进制复制为 `java.exe`、`node.exe` 等名称，利用 `os.Args[0]` 检测调用名称，自动分发到对应的真实工具。

#### 调度流程

```
用户输入: java -version
    │
    ▼
PATH 查找 → E:\XDev\shims\java.exe (xdev 二进制的副本)
    │
    ▼
main() → shim.MaybeExec("java")
    │
    ├── 名为 "xdev"/"xdev-*" → 返回 false → 走 CLI
    │
    └── 名为 "java" → shim.Lookup("java")
         │
         ├── 1. 检查 current/java/ 目录
         │   └── findInDir("E:\XDev\current\java", "java")
         │       ├── current/java/bin/java.exe  ← 命中
         │       ├── current/java/bin/java.cmd
         │       ├── current/java/java.exe
         │       └── current/java/java.cmd
         │
         ├── 2. 跨工具扫描 (如 npm → current/node/)
         │   └── scanFor() → 遍历 current/*/ 查找
         │
         └── 找到 → exec.Command(target).Run()
              │
              ▼
         执行: E:\XDev\current\java\bin\java.exe -version
```

#### Shim 索引

为避免每次 Lookup 都遍历文件系统，XDev 维护了一个惰性构建的反向索引：

```go
shimIndex map[string]shimEntry  // alias → {path, toolName}
```

- 首次 Lookup 时构建，扫描所有 `current/*/` 目录
- 安装/卸载/激活后通过 `InvalidateShimIndex()` 失效
- 下次 Lookup 自动重建

#### Shim 创建

```go
func InstallShim(shimDir, name, selfPath string) error
```

- **Unix**：创建符号链接（1 字节）
- **Windows**：复制二进制（同时创建 `<name>` 和 `<name>.exe`）

Shim 始终使用 `$XMDEV_HOME/xdev.exe`（主目录中的副本），而非开发目录中的二进制，确保可移植性。

#### 为什么不用 .cmd 脚本？

| 方案 | 优点 | 缺点 |
|------|------|------|
| `.cmd` 脚本 | 文件小 | stderr 处理异常、无法捕获退出码、PowerShell 兼容性差 |
| **xdev 二进制副本** | 完美兼容所有 Shell | 每个约 19MB |

19MB 的磁盘开销换来的是 100% 的兼容性和可靠性，这是值得的权衡。

---

### 3.2 版本切换机制

XDev 使用 **Junction**（Windows）/ **Symbolic Link**（Unix）实现版本切换，无需修改 PATH。

#### 目录结构

```
E:\XDev\
├── install/
│   ├── java/
│   │   ├── 17/          ← Java 17 安装
│   │   └── 25/          ← Java 25 安装
│   ├── node/
│   │   └── 20.18.0/     ← Node.js 安装
│   └── python/
│       └── local/       ← 本地 Python（迁移而来）
├── current/
│   ├── java → install/java/25    ← Junction，指向当前活跃版本
│   ├── node → install/node/20.18.0
│   └── python → install/python/local
└── shims/
    ├── java.exe         ← xdev 二进制副本
    ├── javac.exe
    ├── node.exe
    ├── npm.exe
    └── python.exe
```

#### 切换流程

```
xdev use java@17
    │
    ▼
1. 删除 current/java (旧 Junction)
2. 创建 Junction: current/java → install/java/17
   Windows: mklink /J current\java install\java\17
   Unix:    symlink install/java/17 current/java
3. 复制 xdev 二进制到 $XMDEV_HOME/xdev.exe（如需更新）
4. 重新创建 shims (java.exe, javac.exe, jar.exe, jshell.exe)
5. 设置用户级环境变量: JAVA_HOME=E:\XDev\install\java\17
6. 广播 WM_SETTINGCHANGE (Windows)
```

#### 为什么用 Junction 而非 SymbolicLink？

| 类型 | 需要管理员权限 | 需要开发者模式 | 可靠性 |
|------|:---:|:---:|:---:|
| SymbolicLink | ✅ 需要 | ✅ 需要 | 中 |
| **Junction** | ❌ 不需要 | ❌ 不需要 | 高 |
| 目录复制 | ❌ 不需要 | ❌ 不需要 | 低（慢且占空间） |

XDev 在 Windows 上优先使用 Junction，回退到 SymbolicLink，最终回退到目录复制。

---

### 3.3 环境变量管理

XDev 采用**用户级注册表环境变量**机制，取代了早期的 env fragment 文件注入。

#### 机制对比

| 维度 | 旧方案 (env fragment) | 新方案 (用户级注册表) |
|------|----------------------|---------------------|
| 存储位置 | `$XMDEV_HOME/env/<tool>.sh` | `HKCU\Environment` |
| 作用域 | 仅 shim 子进程 | 全局所有进程 |
| IDE 可用 | ❌ | ✅ |
| 新终端可用 | ❌（需 shim 注入） | ✅（Windows 自动加载） |
| 覆盖行为 | 有 bug（append 不覆盖重复键） | 正确覆盖 |

#### 环境变量写入流程

```
Activate() → p.EnvVars(installPath) → setUserEnvVars(vars)
    │
    ├── JAVA_HOME=E:\XDev\install\java\17
    ├── NODE_HOME=E:\XDev\install\node\20.18.0
    ├── PYTHONHOME=E:\XDev\install\python\local
    └── ... (由 manifest 的 env_vars 定义)
    │
    ▼
env.SetUserEnvVar(name, value)
    │
    ▼
RegOpenKeyExW(HKCU\Environment) → RegSetValueExW(name, value, REG_EXPAND_SZ)
    │
    ▼
BroadcastEnvironmentChange() → SendMessageTimeout(HWND_BROADCAST, WM_SETTINGCHANGE, "Environment")
```

#### 环境变量来源

每个工具的环境变量由 manifest YAML 中的 `env_vars` 定义：

```yaml
# manifests/java.yaml
env_vars:
  JAVA_HOME: "{{install}}"

# manifests/node.yaml
env_vars:
  NODE_HOME: "{{install}}"

# manifests/rust.yaml
env_vars:
  RUSTUP_HOME: "{{install}}\\.rustup"
  CARGO_HOME: "{{install}}\\.cargo"
```

`{{install}}` 占位符在激活时替换为实际安装路径。

---

### 3.4 下载管理

XDev 的下载管理器支持**暂停/继续/取消/断点续传**。

#### 架构

```
┌──────────────────────────────────────┐
│           Manager (GUI 用)           │
│                                      │
│  tasks map[string]*Task              │
│  ┌──────────────────────────────┐    │
│  │ Task                         │    │
│  │  ID: "java@17"              │    │
│  │  Status: downloading        │    │
│  │  Progress: 45.2%            │    │
│  │  Speed: "2.3 MB/s"          │    │
│  │  ETA: "12s"                 │    │
│  │  Downloaded: 45,000,000     │    │
│  │  Total: 99,500,000          │    │
│  │  ctx: context.Context       │    │
│  │  cancel: context.CancelFunc │    │
│  │  pausedAt: int64            │    │
│  └──────────────────────────────┘    │
└──────────────────────────────────────┘
```

#### 状态机

```
                  ┌──────────┐
                  │downloading│
                  └──┬─┬─┬───┘
            Pause   │  │  │  Cancel
               ┌────┘  │  └──────┐
               ▼       │         ▼
         ┌─────────┐   │   ┌──────────┐
         │ paused  │   │   │cancelled │
         └────┬────┘   │   └──────────┘
         Resume│       │        │
               │    Failed│  Cleanup .part
               ▼       │        │
         ┌──────────┐  ▼   ┌──────────┐
         │downloading│────→│  failed  │
         └────┬─────┘     └──────────┘
         Complete│
               ▼
         ┌──────────┐
         │completed │
         └──────────┘
```

#### 断点续传

```
1. 首次下载: GET url → 写入 dest.part
2. 暂停: cancel ctx, 记录 pausedAt = 已下载字节数
3. 恢复: 检测 dest.part 文件大小 → GET url + Range: bytes=pausedAt-
4. 取消: 删除 dest.part 文件
5. 完成: dest.part → rename → dest
```

#### SHA-256 校验

下载完成后与 manifest 中的 SHA-256 比对。若 manifest 中 SHA-256 为全零（未提供），则将计算值保存到 `.sha256` 旁文件供后续校验。

**续传 SHA-256 修复**：断点续传时，SHA-256 哈希器需要覆盖整个文件内容，而非仅续传部分。恢复下载时，先将已存在的 `.part` 文件内容读入哈希器，再继续写入新数据，确保最终 SHA-256 校验值正确。

---

## 4. 安装工作流

完整的安装流程由 `Installer` 编排，共 8 步：

```
xdev install python@3.12
    │
    ▼
┌─────────────────────────────────────────────────────────┐
│ Step 1: ResolveVersion                                   │
│   "3.12" → resolver.Parse() → KindRange(3.12.*)         │
│   → resolver.Resolve(available_versions) → "3.12.7"     │
├─────────────────────────────────────────────────────────┤
│ Step 2: ArchiveFor                                       │
│   选择当前 OS/Arch 对应的归档                             │
│   windows/amd64 → python-3.12.7-embed-amd64.zip         │
├─────────────────────────────────────────────────────────┤
│ Step 3: Mirror Apply                                     │
│   上游 URL → 镜像 URL (如 npmmirror/aliyun)              │
│   跳过自有 CDN (Adoptium/GitHub)                         │
├─────────────────────────────────────────────────────────┤
│ Step 4: Download                                         │
│   HTTP GET → .part 文件 → 进度回调 → 重试(3次,指数退避)  │
├─────────────────────────────────────────────────────────┤
│ Step 5: SHA-256 Verify                                   │
│   计算下载文件 SHA-256 → 与 manifest 比对                 │
├─────────────────────────────────────────────────────────┤
│ Step 6: Extract                                          │
│   根据 format 选择解压引擎 (zip/tar.gz/tar.xz/7z)       │
│   strip=N 去除前导路径，防止 Zip Slip 攻击               │
├─────────────────────────────────────────────────────────┤
│ Step 7: Activate                                         │
│   7a. 创建 Junction: current/<tool> → install/<tool>/<v>│
│   7b. 复制 xdev 二进制到 $XMDEV_HOME/xdev.exe           │
│   7c. 创建 shims (java.exe, javac.exe, ...)             │
│   7d. 设置用户级环境变量 (JAVA_HOME, NODE_HOME, ...)     │
├─────────────────────────────────────────────────────────┤
│ Step 8: Broadcast                                        │
│   WM_SETTINGCHANGE → 新终端立即感知变化                   │
└─────────────────────────────────────────────────────────┘
```

---

## 5. 插件系统

### Plugin 接口

```go
type Plugin interface {
    Name() string                                    // 工具标识
    DisplayName() string                             // 显示名
    Description() string                             // 描述
    Homepage() string                                // 主页
    Category() string                                // 分类
    Manifest() *manifest.Tool                        // Manifest 数据
    ResolveVersion(spec string) (string, error)      // 版本解析
    DefaultVersion() string                          // 默认版本
    ArchiveFor(version string) (*manifest.Archive, error) // 归档选择
    InstallDir(root, version string) string          // 安装路径
    Shims() []string                                 // Shim 名称列表
    EnvVars(installPath string) map[string]string    // 环境变量
    PostInstall(ctx context.Context, installPath string) error // 安装后钩子
    Deactivate(installPath string) error            // 切换/卸载时的清理钩子
}
```

### 扩展方法

Phase 1 为 Plugin 接口新增了两个方法：

```go
PostInstall(ctx context.Context, installPath string) error  // 安装后钩子
Deactivate(installPath string) error                         // 停用钩子
```

`Generic` 提供了 no-op 默认实现，具体插件可按需覆盖。

### Lua 插件引擎

Phase 2 引入了基于 `github.com/yuin/gopher-lua` 的 Lua 插件系统：

- **沙箱 VM**：限制文件系统和网络访问
- **LuaPlugin**：实现 Plugin 接口，委托给 Lua 函数
- **vfox 兼容**：适配 vfox Lua 插件格式

```lua
PLUGIN = {
    name = "mytool",
    display_name = "My Tool",
    category = "build",
    shims = {"mytool"},
    available = function() return {"1.0.0"} end,
    archive = function(version, os, arch)
        return { url = "...", format = "tar.gz", strip = 1 }
    end,
}
```

### Generic 实现

`Generic` 结构体嵌入 `*manifest.Tool`，委托所有方法：

```go
type Generic struct {
    *manifest.Tool
}
```

38 个具体插件只需嵌入 `*Generic`，在需要自定义行为时覆盖方法。例如 Python 插件仅 35 行代码。

### Registry

```go
type Registry struct {
    plugins map[string]plugin.Plugin
}
```

- `Register(p)` — 注册插件
- `Get(name)` — 按名获取
- `All()` — 返回所有插件（按名排序）

---

## 6. 镜像系统

### 镜像重写流程

```
原始 URL: https://www.python.org/ftp/python/3.12.7/python-3.12.7-embed-amd64.zip
    │
    ▼ mirror.Apply("python", url)
    │
    ├── 查找镜像配置: mirror["python"] = "https://mirrors.aliyun.com/python"
    │
    ├── 速度测试 (可选): SpeedTest() → HEAD 请求 → 按延迟排序
    │
    └── URL 重写: rewrite(original, mirror)
        │
        ├── 提取上游路径尾部: /3.12.7/python-3.12.7-embed-amd64.zip
        └── 拼接镜像: https://mirrors.aliyun.com/python/3.12.7/python-3.12.7-embed-amd64.zip
```

### 默认镜像

| 工具 | 镜像源 |
|------|--------|
| Python | mirrors.aliyun.com |
| Node | npmmirror.com |
| Go | goproxy.cn |
| Rust | rsproxy.cn |
| Maven | mirrors.aliyun.com |
| Android | mirrors.aliyun.com |
| Flutter | flutter-io.cn |

### 跳过镜像

以下 CDN 已足够快，不进行镜像重写：
- Adoptium (Eclipse Temurin JDK)
- GitHub Releases
- npm Registry

---

## 7. 版本解析

XDev 实现了一个最小化的 semver 解析器（无第三方依赖）：

### 支持的版本规格

| 语法 | 类型 | 示例 | 匹配 |
|------|------|------|------|
| `latest` | KindLatest | `xdev install node@latest` | 最高可用版本 |
| `lts` | KindLTS | `xdev install node@lts` | LTS 版本 |
| `17` | KindExact | `xdev use java@17` | 主版本号匹配（17.x.x 中最高） |
| `17.0` | KindExact | `xdev use java@17.0` | 次版本号匹配 |
| `17.0.19` | KindExact | `xdev install java@17.0.19` | 精确匹配 |
| `^3.12` | KindRange | `xdev install python@^3.12` | 兼容范围 (≥3.12, <4.0) |
| `~3.11` | KindRange | `xdev install python@~3.11` | 近似范围 (≥3.11, <3.12) |
| `>=3.12` | KindRange | `xdev install python@>=3.12` | 最低版本 |
| `3.10 \|\| 3.12` | KindRange | `xdev install python@"3.10 \|\| 3.12"` | OR 逻辑 |

### 解析流程

```
"3.12" → Parse() → Spec{Kind: KindExact, Major: 3, Minor: 12}
    │
    ▼ Resolve(available_versions)
    │
    ├── 过滤匹配版本: [3.12.0, 3.12.1, ..., 3.12.7]
    └── 返回最高版本: "3.12.7"
```

---

## 8. 远程版本发现

支持三种远程源类型：

### GitHub Releases

```
RemoteSource{Type: "github", URL: "python/cpython"}
    │
    ▼
GET https://api.github.com/repos/python/cpython/releases
    │
    ├── ETag 缓存 (5min TTL)
    ├── 分页加载
    └── Rate Limit 检测 (403 → 等待)
```

### JSON API

```
RemoteSource{Type: "json", URL: "https://api.nodejs.org/releases", Path: "version"}
    │
    ▼
GET URL → JSON 解析 → dot-separated 路径遍历 → 提取版本号
```

### HTML 页面

```
RemoteSource{Type: "html", URL: "https://...", Pattern: `v(\d+\.\d+\.\d+)`}
    │
    ▼
GET URL → 正则匹配 → 提取版本号
```

---

## 9. 解压引擎

| 格式 | 实现方式 | 备注 |
|------|---------|------|
| `zip` | Go `archive/zip` | 原生支持 |
| `tar.gz` / `tgz` | Go `compress/gzip` + `archive/tar` | 原生支持 |
| `tar.bz2` / `tbz2` | Go `compress/bzip2` + `archive/tar` | 原生支持 |
| `tar.xz` / `txz` | Go `github.com/xi2/xz` + `archive/tar` | 原生支持 |
| `tar.zst` / `tzst` | Go `github.com/klauspost/compress/zstd` + `archive/tar` | 原生支持 |
| `7z` | 系统 `7z` / `7za` 命令 | 需要安装 7-Zip |

### 安全特性

- **Zip Slip / Tar Slip 防护**：`isSafePath()` 检查解压路径是否在目标目录内
- **符号链接验证**：拒绝绝对路径 symlink，检查目标是否在解压目录内
- **Strip 路径**：`strip=N` 去除前 N 层路径，处理上游 tarball 的顶层目录

---

## 10. PATH 管理

### Windows PATH 架构

Windows 的 PATH 由两部分合并：

```
进程 PATH = 系统 PATH (HKLM\...\Environment\Path) + 用户 PATH (HKCU\Environment\Path)
```

系统 PATH 在前，用户 PATH 在后。这意味着如果系统 PATH 中有 `D:\Env\jdk1.8\bin`，即使 xdev 的 shims 在用户 PATH 中，系统 PATH 的 JDK 8 也会优先。

### XDev 的 PATH 策略

XDev 确保 `E:\XDev\shims` 在用户 PATH 的最前面：

```
用户 PATH: E:\XDev\shims;d:\IDE\Trae CN\bin;C:\Users\...
```

由于 Windows 先加载系统 PATH 再加载用户 PATH，shims 目录必须在用户 PATH 最前面才能保证优先级。

### PATH 清理

`CleanupUserPath()` 自动清理以下问题：

| 问题 | 处理方式 |
|------|---------|
| xdev install 目录在 PATH 中 | 移除（如 `E:\XDev\install\node\20.18.0`） |
| 旧版 xmdevenvmt shim 路径 | 移除（如 `E:\XimuLocalDev\xmdevenvmt\shims`） |
| 双反斜杠路径 | 修复（`D:\\Env\\jdk17` → `D:\Env\jdk17`） |
| 引号包裹的路径 | 去引号（`"D:\Env\jdk17"` → `D:\Env\jdk17`） |
| shim 目录不在最前 | 移到最前 |

### WM_SETTINGCHANGE 广播

修改注册表后，XDev 调用 `BroadcastEnvironmentChange()` 发送 `WM_SETTINGCHANGE` 消息：

```go
SendMessageTimeout(HWND_BROADCAST, WM_SETTINGCHANGE, 0, "Environment", ...)
```

这通知所有已打开的 Explorer 和终端刷新环境变量，新打开的终端会立即获取最新 PATH。

---

## 11. GUI 架构

### 技术栈

| 层 | 技术 |
|----|------|
| 后端 | Go + Wails v2.12.0 |
| 前端 | Vue 3 (Composition API) + Naive UI |
| 构建 | Vite 5.0+ |
| i18n | 自研轻量方案（基于 Vue ref + computed） |
| 通信 | Wails 自动生成 JS 绑定 + 事件推送 |

### 前后端通信

```
前端 Vue 组件
    │
    ├── 同步调用: window.go.gui.App.方法名(参数)
    │   └── Wails 运行时桥接 → Go gui.App 方法
    │
    └── 事件监听: window.runtime.EventsOn('installProgress')
        └── Go 后端 runtime.EventsEmit() → 前端回调
```

### Wails 绑定生成机制

Wails v2 使用 `//go:build bindings` / `//go:build !bindings` 构建标签机制生成 TypeScript 绑定：

1. 编译临时二进制时使用 `-tags bindings`
2. 临时二进制通过反射提取所有导出方法的签名
3. 生成 `frontend/wailsjs/go/gui/App.js` 和 `App.d.ts`
4. 生成 `frontend/wailsjs/go/models.ts`（强类型 DTO）

**关键文件**：
- `main.go` — `//go:build !bindings`，正常 CLI/GUI 入口
- `main_bindings.go` — `//go:build bindings`，仅用于绑定生成的入口

### GUI 后端架构（App 拆分）

原始 `app.go` 是一个 3670 行的 God Object，包含 73 个方法覆盖 7 个职责域。现已按职责拆分为 8 个文件：

```
internal/gui/
├── app.go              # 核心结构体 + 生命周期 + 缓存管理 (~430 行)
├── app_tool_query.go   # 工具查询: GetCategories, GetToolsByCategory, GetInstalledVersions, ...
├── app_install.go      # 安装管理: InstallTool, UseVersion, UninstallVersion, 下载任务管理
├── app_local_detect.go # 本地检测: ScanLocalEnvironments, GetDashboardStats, GetPackageManagers, ...
├── app_config.go       # 配置管理: GetConfig, SetMirrorConfig, GetProxyConfig, TestProxy, ...
├── app_migration.go    # 迁移功能: ScanLegacyDir, MigrateLocalTool, InitConfig, ...
├── app_env_vars.go     # 环境变量: GetEnvVars, SetEnvVar, DeleteEnvVar, SwitchToLocalVersion, ...
├── app_profile.go      # Profile: ExportProfile, ImportProfile
├── gui.go              # Wails 应用启动、进程管理
└── log.go              # 日志系统
```

### 强类型返回值

所有暴露给前端的 Wails 绑定方法均使用强类型结构体替代 `map[string]interface{}`：

| 方法 | 返回类型 | 说明 |
|------|---------|------|
| `GetDebugInfo()` | `DebugInfo` | 调试信息 |
| `GetDashboardStats()` | `DashboardStats` | 仪表盘统计 |
| `CheckForUpdate()` | `UpdateCheckResult` | 更新检查结果 |
| `RunDiagnostics()` | `[]DiagnosticResult` | 诊断结果列表 |
| `GetConfig()` | `ConfigInfo` | 配置信息（含嵌套 `ProxyInfo`） |
| `GetProxyConfig()` | `ProxyInfo` | 代理配置 |
| `TestProxy()` | `ProxyTestResult` | 代理测试结果 |
| `ImportProfile()` | `ImportResult` | 导入结果 |
| `MigrateAllLocalTools()` | `[]MigrationResult` | 迁移结果列表 |

强类型确保：
- Wails 绑定生成正确的 TypeScript 类型定义
- 前端获得完整的类型提示和编译时检查
- `omitempty` 标签正确控制空值字段的序列化行为

### 性能优化

#### 异步仪表盘统计

`GetDashboardStats()` 采用异步刷新模式，避免阻塞 UI：

```
前端调用 GetDashboardStats()
    │
    ├── 缓存有效 → 直接返回缓存值 (<1ms)
    │
    └── 缓存过期 → 立即返回旧缓存/零值 (<1ms)
                  │
                  └── 后台 goroutine:
                      ├── atomic CAS 防止并发风暴
                      ├── WaitGroup 并行计算 installSize + cacheSize
                      ├── 更新缓存 + 设置 30s 过期
                      └── EventsEmit("dashboardStatsUpdated")
```

**并发防护**：使用 `atomic.CompareAndSwapInt32(&dashRefreshing, 0, 1)` 确保同时只有一个刷新 goroutine 在运行，防止前端快速切换页面导致的并发风暴。

#### 快照模式（ScanLocalEnvironments）

```
ScanLocalEnvironments()
    │
    ├── refreshLocalCache()  ← 刷新本地工具缓存
    │
    ├── RLock → 复制 localMap 快照 → RUnlock  ← 持锁时间极短
    │
    └── 遍历快照，不持锁执行 getVersion()  ← 耗时操作在锁外
        │
        └── Lock → 更新 localMap 中的版本 → Unlock  ← 短锁更新
```

#### 并行包管理器检测

`GetPackageManagers()` 使用 `sync.WaitGroup` 并行检测 15 个包管理器：

```
GetPackageManagers()
    │
    ├── 初始化 results []PackageManagerInfo
    │
    ├── WaitGroup.Add(15)
    ├── for each pm: go detectPackageManager(pm)  ← 并行执行
    │
    └── WaitGroup.Wait()  ← 等待所有检测完成
```

串行检测耗时 2-30 秒，并行降至 2-3 秒。

#### Channel 同步（waitForLocalCache）

```
waitForLocalCache()
    │
    ├── 快速检查: localMap 非空 → 直接返回
    │
    └── select:
        ├── <-localReady  ← channel 通知（事件驱动）
        └── <-time.After(5s)  ← 超时兜底
```

`localReady` channel 通过 `sync.Once` 保证只关闭一次，避免轮询等待。

### 资源安全

#### Goroutine 生命周期管理

| 组件 | 问题 | 解决方案 |
|------|------|---------|
| `refreshDashboardStats` | 并发风暴 | `atomic.CompareAndSwapInt32` in-flight 标志 |
| `domReady` 延迟刷新 | 应用关闭时 goroutine 泄漏 | `context.WithCancel` + shutdown 中 cancel |
| `InstallTool` 轮询 | 无限循环 | `maxPolls = 7200`（1 小时上限）+ 超时清理 |

#### HTTP 响应体关闭

`TestProxy` 中 `resp.Body.Close()` 使用 `defer` 确保异常路径下不泄漏连接。

### util 共享包

从 3+ 个包中提取的共享工具函数：

```
internal/util/
├── fs.go       # CopyFile, CopyDir (原分散在 gui/installer/shim)
├── format.go   # HumanBytesStr, HumanSpeedStr, HumanETAStr, FormatDurationStr
├── mirror.go   # ShouldApplyMirror, CookieFromInstallArgs
└── process.go  # HideWindow (CREATE_NO_WINDOW 常量)
```

提取目的：
- 消除 5 处代码重复
- 统一格式化逻辑
- 修复 config ↔ env 双向依赖（`UpdateShimDirInUserPath` 移至 env 包）

---

## 12. 目录结构

### 项目源码

```
xmdevenvmt/
├── main.go                    # 主入口 (//go:build !bindings)
├── main_bindings.go           # 绑定生成入口 (//go:build bindings)
├── cmd/xmdevenvmt/main.go     # 纯 CLI 入口
├── manifests/*.yaml           # 38 个工具 manifest
├── frontend/                  # Vue 3 前端
│   ├── src/
│   │   ├── App.vue
│   │   ├── AppContent.vue     # 主界面
│   │   ├── SetupWizard.vue    # 安装向导
│   │   └── i18n/              # 国际化
│   └── wailsjs/               # Wails 自动生成绑定
├── internal/
│   ├── cli/                   # Cobra 命令树 (30+ 子命令: 核心 + 项目级 + 扩展)
│   ├── config/                # 配置加载与管理
│   ├── plugin/                # 插件接口 + Generic 实现
│   ├── plugins/               # 38 个具体工具插件
│   ├── manifest/              # Manifest 数据结构
│   ├── downloader/            # HTTP 下载 + 任务管理
│   ├── extractor/             # 多格式解压引擎
│   ├── mirror/                # 镜像 URL 重写
│   ├── env/                   # 环境变量 / PATH / 注册表
│   ├── installer/             # 安装工作流编排
│   ├── shim/                  # 单二进制多入口调度器
│   ├── resolver/              # Semver 版本解析
│   ├── remote/                # 远程版本发现
│   ├── project/              # 项目级配置 (.xdev.toml)
│   ├── lockfile/             # Lockfile 机制 (.xdev.lock)
│   ├── task/                 # Task runner
│   ├── service/              # 服务编排
│   ├── ai/                   # AI 助手 (Ollama)
│   ├── isolator/             # 环境隔离 (none/container/nix)
│   ├── orchestrator/         # 远程开发 (Docker/SSH)
│   ├── gui/                   # Wails GUI 后端（多文件拆分）
│   │   ├── app.go             # 核心结构体 + 生命周期
│   │   ├── app_tool_query.go  # 工具查询
│   │   ├── app_install.go     # 安装管理
│   │   ├── app_local_detect.go# 本地检测 + 仪表盘
│   │   ├── app_config.go      # 配置管理
│   │   ├── app_migration.go   # 迁移功能
│   │   ├── app_env_vars.go    # 环境变量
│   │   ├── app_profile.go     # Profile 导入导出
│   │   ├── app_project.go     # 项目配置/Lockfile/插件/服务/AI/隔离/远程
│   │   ├── gui.go             # Wails 启动
│   │   └── log.go             # 日志
│   ├── util/                  # 共享工具函数
│   │   ├── fs.go              # CopyFile, CopyDir
│   │   ├── format.go          # HumanSpeedStr, HumanETAStr
│   │   ├── mirror.go          # ShouldApplyMirror, CookieFromInstallArgs
│   │   └── process.go         # HideWindow
│   ├── tui/                   # Bubble Tea 终端 UI
│   ├── i18n/                  # 后端国际化
│   ├── update/                # 自更新
│   └── version/               # 版本号
└── build/bin/                 # 构建产物
```

### 运行时目录

```
E:\XDev\                       # 主目录 ($XMDEV_HOME)
├── xdev.exe                   # xdev 二进制 (shim 源)
├── config.yaml                # 用户配置
├── install/                   # 工具安装目录
│   ├── java/17/
│   ├── java/25/
│   ├── node/20.18.0/
│   └── python/local/
├── current/                   # 当前活跃版本 (Junction)
│   ├── java → install/java/25
│   ├── node → install/node/20.18.0
│   └── python → install/python/local
├── shims/                     # Shim 目录 (在 PATH 中)
│   ├── java.exe               # xdev 二进制副本
│   ├── javac.exe
│   ├── node.exe
│   ├── npm.exe
│   └── python.exe
├── cache/                     # 下载缓存
│   ├── java-17.zip
│   └── java-25.zip
├── env/                       # 环境变量片段 (已废弃)
│   ├── java.sh
│   ├── node.sh
│   └── python.sh
└── manifests/                 # Manifest 缓存
```

---

## 13. 数据流图

### 安装数据流

```
Manifest YAML
    │
    ▼
manifest.LoadCatalog() → Catalog
    │
    ▼
plugin.Generic{Tool} → Plugin
    │
    ├── ResolveVersion("3.12") → "3.12.7"
    ├── ArchiveFor("3.12.7") → Archive{URL, SHA256, Format}
    │
    ▼
mirror.Apply() → 镜像 URL
    │
    ▼
downloader.Download() → cache/python-3.12.7.zip
    │
    ▼
SHA-256 校验
    │
    ▼
extractor.Extract() → install/python/3.12.7/
    │
    ▼
installer.Activate()
    ├── Junction: current/python → install/python/3.12.7
    ├── Shim: shims/python.exe (复制 xdev 二进制)
    └── EnvVar: PYTHONHOME=E:\XDev\install\python\3.12.7
```

### Shim 调度数据流

```
用户输入: python --version
    │
    ▼
OS 查找 PATH → E:\XDev\shims\python.exe
    │
    ▼
main() → shim.MaybeExec("python")
    │
    ▼
shim.Lookup("python")
    │
    ├── shimIndex["python"] → {path, toolName}
    │   (惰性构建，扫描 current/*/ 目录)
    │
    ▼
target = "E:\XDev\current\python\python.exe"
    │
    ▼
exec.Command(target, "--version").Run()
    │
    ▼
输出: Python 3.12.7
```

### 版本切换数据流

```
xdev use java@17
    │
    ▼
installer.Activate(java, "E:\XDev\install\java\17", "17")
    │
    ├── 1. 删除 current/java (旧 Junction)
    ├── 2. mklink /J current\java install\java\17
    ├── 3. 复制 xdev.exe → E:\XDev\xdev.exe
    ├── 4. 复制 E:\XDev\xdev.exe → shims\java.exe
    ├── 5. 复制 E:\XDev\xdev.exe → shims\javac.exe
    ├── 6. SetUserEnvVar("JAVA_HOME", "E:\XDev\install\java\17")
    ├── 7. BroadcastEnvironmentChange()
    └── 8. InvalidateShimIndex()
```

---

## 14. 项目配置与 Lockfile

### .xdev.toml 项目配置

项目级配置文件 `.xdev.toml` 管理项目特定的工具版本、环境变量和任务：

```go
type ProjectConfig struct {
    Tools map[string]string  `toml:"tools"`   // 工具名 → 版本 spec
    Env   map[string]string  `toml:"env"`     // 环境变量
    Tasks map[string]Task    `toml:"tasks"`   // 任务名 → Task
}

type Task struct {
    Run  string `toml:"run"`            // 要执行的命令
    Desc string `toml:"desc,omitempty"` // 描述
    Dir  string `toml:"dir,omitempty"`  // 工作目录
}
```

- `FindConfig(dir)` 从给定目录向上搜索 `.xdev.toml`
- `SetTool(dir, tool, version)` 设置项目级工具版本
- 兼容 `.tool-versions`（asdf 格式）自动转换

### cd hook 机制

Shell 集成通过 `xdev activate` 注入 shell RC 文件：

```
cd → hook → xdev hook <shell> → 读取 .xdev.toml → 输出 export 语句
```

支持 bash、zsh、fish、pwsh 四种 shell。

### Lockfile 机制

`.xdev.lock` 锁定精确版本，确保团队环境可复现：

```go
type Lockfile struct {
    Version   int                    `toml:"version"`
    CreatedAt time.Time              `toml:"created_at"`
    Tools     map[string]LockedTool  `toml:"tools"`
}

type LockedTool struct {
    Version     string `toml:"version"`      // 原始 spec
    Resolved    string `toml:"resolved"`     // 精确版本
    SHA256      string `toml:"sha256"`       // 归档校验和
    URL         string `toml:"url"`          // 归档 URL
    InstallPath string `toml:"install_path"` // 安装路径
}
```

安装流程自动更新 lockfile：

```
xdev install node@20
    │
    ├── InstallWithProgress() → InstallResult{Spec, Version, URL, SHA256}
    │
    └── updateLockfile() → .xdev.lock 写入:
        [tools.node]
        version = "20"           ← 原始 spec
        resolved = "20.18.0"     ← 精确版本
        sha256 = "abc123..."     ← 校验和
        url = "https://..."      ← 下载 URL
```

`xdev install --locked` 从 lockfile 安装，使用 `Resolved` 字段确保精确版本。

---

## 15. 服务编排与环境隔离

### 服务编排

`service.Manager` 管理长期运行的后台进程（数据库、Web 服务器等）：

```go
type Manager struct { cfg *config.Config }
func (m *Manager) List() []ServiceInfo
func (m *Manager) Start(name string) error
func (m *Manager) Stop(name string) error
```

### 环境隔离

`isolator.Isolator` 接口提供三种隔离模式：

| 模式 | 实现 | 说明 |
|------|------|------|
| `none` | `noneIsolator` | 直接修改 PATH，无隔离 |
| `container` | `containerIsolator` | Docker 容器隔离 |
| `nix` | `nixIsolator` | Nix flake 隔离 |

### 远程开发

`orchestrator.Adapter` 接口支持远程开发环境：

| 适配器 | 说明 |
|--------|------|
| `DockerAdapter` | 通过 Docker 创建远程开发容器 |
| `SSHAdapter` | 通过 SSH 连接远程服务器 |

### AI 集成

`ai.Assistant` 通过 Ollama HTTP API 提供 AI 辅助功能：

- `SuggestVersions(tool, currentVersion)` — 版本推荐
- `DiagnoseIssue(problem)` — 环境诊断
- `Generate(prompt)` — 通用单轮问答

所有功能在 Ollama 不可用时优雅降级。

## 16. Lockfile 命令

### `xdev lock` — 重新生成 Lockfile

```bash
xdev lock [tool...]          # 重新生成整个或部分 lockfile
xdev lock --check            # 校验 lockfile 与 .xdev.toml 一致性
xdev lock --check --json     # JSON 格式输出
```

**工作流程**（`internal/cli/lock.go`）：

1. `lockfile.FindConfigDir(".")` 查找项目根目录
2. `project.FindConfig(dir)` 读取 `.xdev.toml`
3. `loadCatalogAndRegistry()` 加载 catalog 和 registry
4. 对每个 tool：`reg.Get(toolName)` → `ResolveVersion(spec)` → `ArchiveFor(ver)` → `lockfile.AddTool()`
5. `lockfile.Save(dir, lf)` 写入 `.xdev.lock`

**漂移检测**（`--check`）：

| 检测项 | 说明 |
|--------|------|
| Spec 漂移 | `.xdev.toml` 中 go="1.22" 但 `.xdev.lock` 中 version="1.21" |
| 缺失条目 | toml 有工具但 lock 没有 |
| 过期条目 | lock 有工具但 toml 没有 |

### `xdev lock` 与 `xdev install --locked` 的关系

| 命令 | 用途 |
|------|------|
| `xdev lock` | 从 `.xdev.toml` **生成** `.xdev.lock` |
| `xdev install --locked` | 从 `.xdev.lock` **精确安装**（不解析版本） |
| `xdev install` | 正常安装 + 自动更新 `.xdev.lock` |

---

## 17. 分层信任模型（Layered Trust）

### 设计原则

XDev 对不同类型的 manifest 采用不同的信任策略：

| Manifest 类型 | 信任模型 | 行为 |
|--------------|---------|------|
| **内置 manifest**（随二进制发布） | 强制 Ed25519 签名验证 | 无签名或签名失败 → **拒绝下载** |
| **第三方 manifest**（用户添加） | TOFU（Trust On First Use） | 无签名时记录 SHA-256 sidecar，后续比对 |

### 代码路径

**签名验证**（`internal/downloader/downloader.go:108-114`）：

```go
if len(opts.ManifestSignature) > 0 {
    // 有签名 → 验证
    if err := verifyManifestSignature(opts); err != nil {
        return fmt.Errorf("manifest signature verification failed: %w", err)
    }
} else if signer.HasRootKey() && !opts.IsThirdParty {
    // 内置 manifest 无签名 → 拒绝
    return fmt.Errorf("manifest signature is missing...")
}
// 第三方 manifest 无签名 → 降级到 TOFU（不拒绝）
```

**第三方来源检测**（`internal/installer/installer.go:290-317`）：

- 内置 manifest：签名文件在 `ManifestDir/<tool>.yaml.sig`
- 第三方 manifest：签名文件在 `ManifestDir/third-party/<tool>.yaml.sig`
- 无签名文件时，检查 `third-party/<tool>.yaml` 是否存在 → 设置 `isThirdParty=true`

### TOFU 降级机制

1. 首次下载：计算 SHA-256 → 写入 `<cache>/<file>.sha256`（只读权限 0o444）
2. 后续下载：读取 sidecar → 与计算值比对
3. 不匹配：拒绝下载
4. 网络攻击者无法同时替换归档和 sidecar（sidecar 本地存储）

---

## 18. RequiredPlugin 依赖检查

### manifest YAML 声明

```yaml
name: flutter
required_plugins:
  - java
  - android-sdk-tools
```

### 安装时检查流程

`installer.InstallWithProgress` 入口处：

```
p.RequiredPlugin() → ["java", "android-sdk-tools"]
  │
  ├── reg.Get("java") → 不存在 → error "required plugin java is not supported"
  │   └── 存在 → os.Stat(javaInstallPath)
  │       └── 不存在（未安装）→ error "required plugin java is not installed"
  │           └── 存在（已安装）→ 继续检查下一个
  │
  └── reg.Get("android-sdk-tools") → ...
```

### 卸载时检查流程

`installer.Uninstall` 入口处：

```
遍历 reg.All() 中的每个 tool：
  ├── tool.Name() == p.Name() → 跳过（自身）
  └── tool.RequiredPlugin() 包含 p.Name()：
      └── os.Stat(toolInstallPath) 存在 → error "cannot uninstall java: required by flutter"
```

---

## 19. 批量安装（InstallMany）

### 接口

```go
func (i *Installer) InstallMany(ctx, items []BatchInstallItem) []BatchInstallResult
```

### 处理逻辑

| 输入 | 处理 | 输出 Status |
|------|------|------------|
| tool 不在 registry | 跳过 | "unsupported" |
| installPath 已存在 | 跳过（不重复下载） | "already_installed" |
| 安装成功 | 返回结果 | "installed" |
| 安装失败 | 捕获错误 | "failed" |

### 与单工具安装的区别

| 特性 | Install | InstallMany |
|------|---------|-------------|
| 进度回调 | 支持 | 不支持（批量无单工具进度） |
| 后安装处理 | 完整激活 | 可选（NoActivate） |
| 错误处理 | 立即返回 | 聚合到 []BatchInstallResult |
| 使用场景 | 交互式单工具安装 | 批量/CI 场景 |

---

## 20. 新增工具函数（util 包）

| 函数 | 签名 | 替代的重复代码 |
|------|------|--------------|
| `AtomicWriteFile` | `(path string, data []byte, perm os.FileMode) error` | config/lockfile/manifest/refresh 中 6 处 tmp+rename |
| `CompareSemver` | `(a, b string) int` | installer + update 中重复的版本比较 |
| `ParseSemverParts` | `(s string) [3]int` | installer + update 中重复的版本解析 |
| `ActiveVersion` | `(xmdeDir, toolName string) (string, bool)` | cli/installer/gui 中 4 处 current 链接读取 |
| `ActiveInstallPath` | `(xmdeDir, toolName string) string` | cli/env 中重复的路径构造 |
| `IsSafeExternalURL` | `(rawURL string) error` | remote/ai/update 中的 URL SSRF 防护 |
| `IsLocalhost` | `(host string) bool` | AI localhost 检测 |
| `IsProtectedSystemEnvVar` | `(name string) bool` | GUI 中 3 份系统变量保护名单 |
| `IsPrivateOrReservedIP` | `(ip net.IP) bool` | SSRF 防护中的内网 IP 检测 |
