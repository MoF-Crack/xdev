# XDev 使用手册

> 跨平台开发环境版本管理工具 — CLI / TUI / GUI

## 目录

- [快速入门](#快速入门)
- [基本命令](#基本命令)
- [Shim 调度系统](#shim-调度系统)
- [版本管理](#版本管理)
- [项目级配置与 Lockfile](#项目级配置与-lockfile)
- [Profile 导入导出](#profile-导入导出)
- [操作模式：CLI / TUI / GUI](#操作模式cli--tui--gui)
- [环境变量管理](#环境变量管理)
- [多镜像源配置](#多镜像源配置)
- [高级功能](#高级功能)
- [常见问题排查](#常见问题排查)

---

## 快速入门

```bash
# 1. 初始化向导
xdev init

# 2. 安装工具
xdev install python@3.12
xdev install node@20
xdev install go@1.22

# 3. 切换版本
xdev use python@3.11

# 4. 查看已安装
xdev list

# 5. 验证
python --version
xdev doctor
```

---

## 基本命令

### `xdev init` — 初始化

创建 XDev 主目录结构和默认配置。Windows 上交互式选择安装盘（推荐非系统盘）。

```bash
xdev init                         # 交互式向导
xdev init --yes                   # 非交互（使用推荐盘）
xdev init --xmdevenv-dir D:\XDev  # 指定主目录
xdev init --no-path-fix           # 不修改环境（CI 使用）
```

### `xdev install` — 安装工具

```bash
xdev install python@3.12.0        # 安装指定版本
xdev install node@latest          # 安装最新版
xdev install go@lts               # 安装 LTS 版
xdev install java@^17             # semver 范围匹配
xdev install --from-local         # 从 .xdev.toml 批量安装
xdev install --locked             # 仅按 .xdev.lock 精确安装
```

安装过程中：
- 断点续传，支持暂停/继续/取消
- SHA-256 校验验证下载完整性
- 自动解压并安装到 `install/<tool>/<version>/`

### `xdev use` — 切换版本

```bash
xdev use python@3.11              # 切换激活版本
xdev use node@18                  # 自动解析到 18.x 最新版
```

切换后，`current/<tool>` 符号链接会指向新版本。

### `xdev uninstall` — 卸载

```bash
xdev uninstall python@3.12.0      # 卸载指定版本
```

如果卸载的是当前激活版本，会自动尝试激活其他已安装版本。

### `xdev list` — 列出已安装

```bash
xdev list                         # 列出所有已安装工具及版本
xdev list python --remote         # 列出 python 的远程可用版本
```

### `xdev current` — 当前激活版本

```bash
xdev current                      # 显示所有激活版本
xdev current python               # 显示 python 的激活版本
```

### `xdev search` — 搜索工具

```bash
xdev search python                # 按名称、显示名、描述搜索
xdev search android               # 搜索 Android 相关工具
```

### `xdev available` — 所有支持的工具

```bash
xdev available                    # 列出所有支持的工具及其分类
```

输出表格包含：Name、Display、Category、Default、Description。

### `xdev which` — 查看路径

```bash
xdev which python                 # 显示当前 python 指向的路径
```

### `xdev doctor` — 健康检查

```bash
xdev doctor                       # 运行诊断检查
xdev doctor --fix                 # 自动修复检测到的问题
xdev doctor --ai                  # AI 辅助诊断（需 Ollama）
```

检查项：
1. XDev 主目录存在且可写
2. 所有必要子目录存在
3. Shim 目录在 PATH 中
4. Shell RC 已注入 cd hook
5. Shim 二进制文件正常
6. Manifest 目录有工具
7. 用户 PATH 无损坏条目
8. 系统 PATH 无损坏条目

### `xdev config` — 配置管理

```bash
xdev config list                  # 查看所有配置
xdev config get mirror.python     # 查看特定配置
xdev config set path.mode shim    # 修改配置
xdev config proxy                 # 查看代理配置
xdev config proxy --http http://proxy:8080 --enable  # 设置代理
```

### `xdev upgrade` — 升级工具

```bash
xdev upgrade python               # 升级 python 到最新版
xdev upgrade --all                # 升级所有已安装工具
xdev upgrade --all --check        # 检查可升级但不执行
xdev upgrade --all --cleanup      # 升级后清理旧版本
```

### `xdev self update` — 自更新

```bash
xdev self update                  # 检查并应用 xdev 自身更新
xdev self version                 # 显示当前版本
xdev self uninstall               # 卸载 xdev（保留数据目录可选）
```

### `xdev env` — 环境变量输出

```bash
eval "$(xdev env python@3.12.0)"              # 输出 sh 格式
iex "$(xdev env python@3.12.0 | Out-String)"  # 输出 PowerShell 格式
xdev env python@3.12.0 --shell powershell     # 指定 shell 格式
```

### `xdev shell` — 子 Shell

```bash
xdev shell                        # 进入含所有激活工具 env 的子 Shell
xdev shell python@3.12            # 仅包含 python@3.12 环境
xdev shell java@21 go@1.22        # 包含多个指定工具
```

---

## Shim 调度系统

### 工作原理

```
E:\XDev\
├── xdev.exe              # xdev 二进制（shim 源）
├── install/              # 实际安装目录
│   ├── python/3.12.0/
│   └── node/20.18.0/
├── current/              # 当前激活版本（符号链接）
│   ├── python → install/python/3.12.0
│   └── node   → install/node/20.18.0
├── shims/                # Shim 目录（PATH 最优先）
│   ├── python.exe  ← xdev 二进制副本
│   ├── node.exe    ← xdev 二进制副本
│   └── ...
└── manifests/            # 工具元数据（YAML）
```

当你执行 `python --version` 时：

1. Shell 在 PATH 中找到 `E:\XDev\shims\python.exe`（xdev 二进制的副本）
2. xdev 检测到 `argv[0]` 是 `python`，进入 shim 调度
3. 查找 `current/python` 找到当前激活版本的安装路径
4. 通过 `os.Exec` 调用真正的 `python.exe`，透传所有参数和 I/O

### PATH 模式

```bash
xdev config set path.mode shim     # Shim 模式（默认）
xdev config set path.mode direct   # 直接模式（不使用 shim）
xdev config set path.mode symlink  # 符号链接模式
```

- **shim**：xdev 二进制副本作为代理分发（推荐）
- **direct**：直接通过 PATH 操作
- **symlink**：通过符号链接指向真实二进制

### 配置选项

```bash
# Windows 上将 shim 目录添加到用户 PATH 注册表
xdev config set path.inject_to_windows_path true

# 将 cd hook 注入 Shell RC
xdev config set path.inject_to_shell_rc true
```

---

## 版本管理

### 版本规范

| 写法 | 含义 | 示例 |
|------|------|------|
| `latest` | 最新版本 | `xdev install python@latest` |
| `lts` | 长期支持版 | `xdev install node@lts` |
| `3.12.0` | 精确版本 | `xdev install python@3.12.0` |
| `^3.12` | semver 范围（主版本不变） | `xdev install python@^3.12` |
| `~3.12` | 仅 minor 内可升级 | `xdev install python@~3.12` |

### 多版本共存

xdev 支持同时安装同一工具的多个版本，通过 `xdev use` 切换。

```bash
xdev install python@3.11
xdev install python@3.12
xdev install python@3.13

xdev use python@3.11   # 切换到 3.11
xdev use python@3.13   # 切换到 3.13
```

---

## 项目级配置与 Lockfile

### 项目配置 `.xdev.toml`

在项目根目录创建 `.xdev.toml` 声明工具版本和环境变量：

```toml
[tools]
python = "3.12"
node = "20"
java = "17"

[env]
PYTHONPATH = "./src"
JAVA_HOME = "{{.installDir}}"

[tasks.build]
run = "npm run build"
desc = "构建项目"

[tasks.test]
run = "pytest"
dir = "./tests"

[tasks.lint]
run = "golangci-lint run"
sources = ["*.go", "internal/**/*.go"]
```

### 项目管理命令

```bash
# 设置项目级工具版本
xdev local python@3.12
xdev local node@20

# 查看项目配置
xdev local
xdev local --show

# 移除项目级版本
xdev local --unset node

# 在当前目录运行项目任务
xdev run build
xdev run test
xdev task list
xdev task run lint

# cd 自动钩子：进入目录时自动切换版本
eval "$(xdev hook --shell bash)"     # 输出 cd hook 脚本
xdev activate bash                   # 注入 cd hook 到 Shell RC
xdev deactivate                      # 移除 cd hook
```

### Lockfile `.xdev.lock`

Lockfile 记录精确版本号和校验和，确保团队环境可复现：

```bash
# 生成 lockfile（从 .xdev.toml 解析版本）
xdev lock

# 检查 lockfile 是否与 .xdev.toml 一致
xdev lock --check

# JSON 格式输出
xdev lock --json

# 从 lockfile 精确安装
xdev install --locked

# 仅锁定特定工具
xdev lock python node
```

---

## Profile 导入导出

导出完整的开发环境配置为 JSON 文件，可以在另一台机器上恢复：

```bash
# 导出环境（工具、版本、环境变量、代理、镜像源）
xdev profile export my-profile.json

# 导入环境
xdev profile import my-profile.json
```

Profile 包含：
- 已安装的工具及其版本
- 每个工具的激活状态
- PATH 模式、代理配置、镜像源配置
- 用户环境变量
- GUI 收藏工具和自定义工具

---

## 操作模式：CLI / TUI / GUI

### CLI 模式（默认）

```bash
xdev install python@3.12
xdev list
```

直接运行 xdev 命令，适合脚本和快速操作。

### TUI 模式（终端界面）

```bash
xdev tui
# 或直接运行 xdev（不带参数）自动启动 TUI
```

TUI 提供交互式界面，使用 Bubble Tea 框架构建，适合在终端中浏览和管理工具。

### GUI 模式（桌面图形界面）

```bash
xdev gui
# 或直接运行 xdev-gui.exe
```

GUI 通过 Wails v2 构建，Go 后端 + Vue.js 前端，单二进制打包。支持：
- 暗色/浅色主题切换
- 工具浏览和安装
- 版本管理
- 收藏工具

GUI 二进制命名约定：`xdev-gui` 或 `xdev-gui-<version>` 会自动启动 GUI 界面。

---

## 环境变量管理

xdev 管理三级环境变量：

### 工具级环境变量

每个工具在其 manifest 中声明环境变量（如 `PYTHONHOME`、`JAVA_HOME`），激活时自动设置。

### 项目级环境变量

在 `.xdev.toml` 的 `[env]` 段中定义：

```toml
[env]
PYTHONPATH = "./src"
MY_CUSTOM_VAR = "value"
```

### 用户级环境变量

通过 `xdev config` 间接管理，profile import/export 会包含用户环境变量。

### 持久环境变量

```bash
# 查看作用于当前 Shell 的环境变量
xdev shell                       # 进入含所有工具 env 的子 Shell
xdev env python@3.12.0           # 查看 python 的环境变量
```

---

## 多镜像源配置

xdev 内置多个国内镜像源，加速下载：

```yaml
# config.yaml 示例
mirror:
  python: https://mirrors.aliyun.com/python
  node: https://npmmirror.com/mirrors/node
  go: https://goproxy.cn
  rust: https://rsproxy.cn
  android: https://mirrors.aliyun.com/android.googlesource.com
  flutter: https://storage.flutter-io.cn
  maven: https://mirrors.aliyun.com/maven
```

可通过 `xdev config set mirror.<tool> <url>` 自定义。

```bash
# 查看当前镜像配置
xdev config list | grep mirror

# 修改 python 镜像源
xdev config set mirror.python https://mirrors.tuna.tsinghua.edu.cn/python
```

---

## 工具版本嗅探与 GitHub Token

### 全局嗅探机制

xdev 的「嗅探」(sniff) 功能会自动发现工具的远程可用版本，支持多种上游源：

- **GitHub Releases API** (`api.github.com`) — 多数托管在 GitHub 的工具
- **Google Android 仓库** — Android SDK 工具链
- **官网 HTML 页面** — 部分无统一 API 的工具（如仓颉）
- **其他自定义源** — Lua 插件或 manifest 中可配置

嗅探在以下时机触发：后台定时（默认 24h）、启动时、手动 `xdev sniff`。

### GitHub API 限流

GitHub 类工具依赖 `api.github.com`，其 API 存在访问频率限制：

| 认证状态 | 每小时限额 | 说明 |
|---------|-----------|------|
| 未认证 | ≈ 60 次 | 全量嗅探（约 40+ 工具）极易触发 |
| 已配置 Token | 5,000 次 | 足够日常使用 |

未认证时，全量嗅探很快耗尽配额，日志中会出现：
```
github remote: API rate limit exhausted
```

### 配置 GitHub Token

配置 Token 可将配额提升至 ≈5,000 次/小时，避免嗅探因限流中断。

**方法一：环境变量（推荐）**

设置用户级环境变量（Windows 在「系统属性 > 环境变量」中添加）：

- `GITHUB_TOKEN` — 首选变量名
- `GH_TOKEN` — 备选变量名

从 Windows 资源管理器启动 GUI 时，xdev 也会读取注册表 `HKCU\Environment` 中的值，确保 Explorer 启动的进程也能找到 Token。

**方法二：config.yaml**

```yaml
sniff:
  github_token: ghp_xxxxxxxxxxxx  # 注意：勿提交到仓库
```

优先级：配置文件 > `GITHUB_TOKEN` 环境变量 > `GH_TOKEN` 环境变量。

启动日志确认 Token 是否生效：
```
[startup] GitHub API token: configured
```
若未配置：
```
[startup] GitHub API token: not configured — rate limit ~60 req/h
```

### 代理与 Token 的区别

| 概念 | 作用 | 配置方式 |
|------|------|---------|
| **代理 (Proxy)** | 解决网络连通性（如公司内网、防火墙） | `xdev config proxy --http http://proxy:8080 --enable` |
| **Token** | 解决 GitHub API 配额限制 | 见上方配置方法 |

**建议**：
- 配置 Token 后，确认代理设置无误，避免无效代理导致连接失败
- Token 仅用于读取公开的 releases 信息，不需要 CI/Runner 权限
- 创建 Token 时无需勾选任何 `repo` 或 `admin` 权限，`public_repo` 只读即可

### 常见问题

**Q: 已经配置了 Token，为什么还提示未配置？**

重启终端或 GUI 使环境变量生效。从 Windows 资源管理器启动 GUI 时，需重新登录或重启 Explorer。

**Q: 配置 Token 后嗅探仍然失败？**

1. 检查网络连通性：`curl -I https://api.github.com`
2. 确认 Token 有效：`curl -H "Authorization: token ghp_xxx" https://api.github.com/rate_limit`
3. 检查是否配置了无效代理——代理与服务端不通会导致 API 不可达（日志可能显示 proxy refused）
4. 关闭不必要的代理，或在 config.yaml 中将 `proxy.enabled` 设为 `false`

---

## 更新忽略与本地扫描结果

### 可用更新「忽略」

顶部更新徽标（如「2 个更新」）展开后，每项除「安装」外提供 **忽略**：

- 忽略后该工具不再出现在可用更新列表，徽标计数减少
- 持久化路径：`$XMDEV_HOME/cache/ignored_tools.json`（字段 `kind=update`）
- 仪表盘 **忽略清单** 可查看全部忽略项，并 **取消忽略**
- 取消忽略后，若 sniff 缓存仍显示有新版本，该项会重新出现

### 本地扫描结果

仪表盘「扫描本地环境」完成后会展示 **本地扫描结果** 列表（工具 / 版本 / 路径）：

| 操作 | 行为 |
|------|------|
| **迁移** | 将检测到的本地安装纳入 xdev 管理（复用现有迁移逻辑） |
| **忽略** | 写入忽略清单（`kind=local-scan`），下次扫描不再显示 |

忽略清单与更新忽略共用同一文件 `$XMDEV_HOME/cache/ignored_tools.json`。在忽略清单中移除某项后，对应更新或本地扫描结果可再次出现。

---

## 高级功能

### 服务编排

管理开发环境中的长驻服务（数据库、缓存等）：

```bash
xdev service list                # 列出可用服务
xdev service start mysql         # 启动 MySQL
xdev service stop redis          # 停止 Redis
xdev service status              # 查看所有服务状态
xdev service logs mysql -f      # 实时查看日志
xdev service restart mysql       # 重启服务
```

### 环境隔离

创建隔离的开发环境：

```bash
# 支持模式：none（默认）、container（Docker）、nix
xdev isolate --mode container setup       # 创建 Docker 隔离环境
xdev isolate exec -- go test ./...        # 在隔离环境中执行命令
xdev isolate --mode container teardown    # 销毁隔离环境
```

### 远程开发

管理远程开发环境（Docker / SSH）：

```bash
# Docker 远程环境
xdev remote create --adapter docker --image ubuntu:22.04

# SSH 远程环境
xdev remote create --adapter ssh --host dev.example.com --user ubuntu

# 管理
xdev remote list                  # 列出远程环境
xdev remote connect <id>          # 连接到远程环境
xdev remote destroy <id>          # 销毁远程环境
```

### Lua 插件扩展

通过 Lua 脚本自定义工具支持：

```bash
xdev plugin list                  # 列出已安装插件
xdev plugin install <url>         # 从 URL 安装 Lua 插件
xdev plugin create mytool         # 创建插件模板
xdev plugin remove mytool         # 移除插件
```

插件存储在 `$XMDEV_HOME/plugins/` 目录，支持完整的工具声明周期（版本发现、下载、环境变量）。

### Manifest 仓库管理

```bash
xdev manifest list                # 列出已加载的 manifest
xdev manifest add <repo-url>      # 从 Git 仓库添加第三方 manifest
xdev manifest remove <name>       # 移除第三方 manifest
xdev manifest update              # 更新所有第三方 manifest
```

### AI 辅助功能

需要本地 [Ollama](https://ollama.com) 服务：

```bash
# 检查 AI 服务状态
xdev ai status

# 版本推荐
xdev ai suggest python

# 环境诊断
xdev ai diagnose

# 配置 AI
xdev ai config --model codellama --url http://localhost:11434
```

AI 功能优雅降级：Ollama 不可用时仅提示安装。

### 密钥与签名

```bash
# 生成 Ed25519 密钥对
xdev keygen --pub-key keys/pub.key --priv-key keys/priv.key

# 签名 manifest 文件
xdev sign-manifests --priv-key keys/priv.key

# 填充 SHA-256 校验和到 manifest
xdev fill-sha256 --dry-run         # 预览
xdev fill-sha256                   # 下载并填充
```

### 自动补全

```bash
# 生成 Shell 补全脚本
xdev completion bash > /etc/bash_completion.d/xdev
xdev completion zsh > /usr/local/share/zsh/site-functions/_xdev
xdev completion fish > ~/.config/fish/completions/xdev.fish
xdev completion powershell > _xdev.ps1
```

---

## 常见问题排查

### "xdev: command not found"

确保 xdev 二进制在 PATH 中，或运行 `xdev init` 完成安装向导。

```bash
# Windows：检查 shim 目录是否在用户 PATH
xdev doctor --fix
```

### 下载失败或速度慢

```bash
# 检查网络
xdev doctor

# 配置国内镜像源
xdev config set mirror.python https://mirrors.tuna.tsinghua.edu.cn/python
xdev config set mirror.node https://npmmirror.com/mirrors/node

# 配置代理
xdev config proxy --http http://proxy:8080 --enable
```

### 工具安装后无法运行

```bash
# 检查 shim
xdev which python

# 检查激活状态
xdev current python

# 重新激活
xdev use python@3.12

# 运行诊断
xdev doctor --fix
```

### Shell 中版本不自动切换

```bash
# 确保 cd hook 已注入
xdev activate bash

# 手动加载
source ~/.bashrc

# 或临时使用
eval "$(xdev hook --shell bash)"
```

### 测试时环境被污染

使用 `--no-path-fix` 标志跳过所有注册表和 Shell 修改：

```bash
xdev install --no-path-fix python@3.12
```

### GUI 无法启动

- 确保系统安装了 WebView2 运行时（Windows）
- 运行 `xdev doctor` 检查配置
- 使用 CLI 模式作为替代

### 关闭颜色输出

```bash
xdev --no-color <command>
```

或设置环境变量 `NO_COLOR=1`。

### 覆盖 XDev 主目录

```bash
xdev --xmdevenv-dir D:\CustomXDev <command>
# 或设置环境变量
export XMDEV_HOME=/custom/path
```
