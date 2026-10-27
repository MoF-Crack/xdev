> **文档类型**：长期参考文档
> **状态**：Living
> **来源**：docs/operations.md
> **P4B 迁移目标**：原位保留
> **替代文档**：无
> **治理记录**：P4A 元数据治理，2026-07-16

# XDev 操作文档

> 版本: 1.2.2-alpha | 最后更新: 2026-07-10

## 1. 安装与初始化

### 1.1 首次安装

```bash
# 下载 xdev 二进制，放入 PATH 可达的位置
# Windows: 放入任意 PATH 目录（如 C:\Tools\xdev.exe）
# macOS/Linux: 放入 /usr/local/bin/xdev

# 运行初始化向导
xdev init

# 非交互模式（自动选择推荐驱动器）
xdev init --yes

# 指定自定义目录
xdev init --xmdevenv-dir D:\XDev

# 跳过 Shell RC 注入
xdev init --no-path-fix
```

**初始化流程**：
1. 选择 XDev Home 目录（Windows 自动扫描 D:-J: 盘，推荐空闲 ≥ 20 GiB 的非 C 盘）
2. 创建目录结构（install/、shims/、cache/、downloads/、env/、manifests/）
3. 交互式选择要安装的语言/工具
4. 注入 Shell RC（bash/zsh/fish/PowerShell）
5. 安装 xdev 自身到 `current/xdev/`

### 1.2 目录结构

```
<XDevHome>/
├── config.yaml              # 全局配置
├── .home.cache              # Home 目录缓存
├── last_selection.json      # 用户驱动器选择记录
├── install/                 # 工具安装目录
│   ├── go/1.22.0/          # Go 1.22.0
│   ├── python/3.12.4/      # Python 3.12.4
│   └── node/20.18.0/       # Node.js 20.18.0
├── current/                 # 活动版本 junction/symlink
│   ├── go → install/go/1.22.0
│   ├── python → install/python/3.12.4
│   └── xdev → install/xdev/current
├── shims/                   # Shim 二进制（xdev 副本）
│   ├── go.exe              # Windows: 完整副本
│   ├── python.exe
│   └── node.exe
├── downloads/               # 下载缓存
├── cache/                   # 清单缓存
├── env/                     # 环境变量片段
├── manifests/               # 工具清单 YAML
│   └── third-party/        # 第三方清单
├── plugins/                 # Lua 插件
├── services/                # 服务数据目录
└── .xdev.lock              # 项目锁文件（项目根目录）
```

### 1.3 环境变量

| 变量 | 用途 | 默认值 |
|------|------|--------|
| `XMDEV_HOME` | 覆盖 XDev Home 目录 | — |
| `XMDEV_FORCE_HOME=1` | 强制使用 `~/.xdev`（跳过智能驱动器选择） | — |
| `GOROOT` | Go 安装路径（由 xdev 管理） | `<install>/go/<ver>` |
| `GOPATH` | Go 工作空间 | `<install>/go/../gopath` |
| `JAVA_HOME` | Java 安装路径 | `<install>/java/<ver>` |
| `NODE_HOME` | Node.js 安装路径 | `<install>/node/<ver>` |
| `PYTHON_HOME` | Python 安装路径 | `<install>/python/<ver>` |

## 2. 工具管理

### 2.1 安装工具

```bash
# 安装最新版本
xdev install go
xdev install python
xdev install node

# 安装指定版本
xdev install go@1.22.0
xdev install python@3.12.4
xdev install node@20

# 安装但不激活
xdev install go@1.21.0 --no-activate

# 强制重装（先下载+校验成功，再删旧安装）
xdev install go@1.21.0 --force
```

> 2026-10-26：`--from-local`（按项目配置批量安装）与 `--locked`
> （按 lockfile 精确安装）两个标志**不存在**（未在 `internal/cli/install.go`
> 注册）。按项目配置安装请逐个钉版本：`xdev install go@1.27.3 python@3.11`。

### 2.4 Lockfile 相关命令（已删除）

> **2026-10-26 修订**：本节此前记录 `xdev lock`、`xdev lock --check`、
> `xdev lock --json` 与 `xdev install --locked` 等命令。这些**全部不存在**：
> `internal/lockfile` 已在 Spec 012 删除，对应 CLI 命令与 `--locked` 标志
> 也一并移除。
>
> 当前实现中「可复现安装」只能靠**显式钉版本**：
>
> ```bash
> # 项目里用 .xdev.yaml 声明版本（yaml 格式，非 toml）
> cat .xdev.yaml
> # tools:
> #   go: "1.27.3"
> #   python: "3.11"
>
> # 安装时显式钉版本（不依赖 lockfile）
> xdev install go@1.27.3 python@3.11
> ```
>
> 已安装版本清单可用 `xdev list` 查看，需跨机器同步时用
> `xdev profile export` / `import`。

### 2.2 切换版本

```bash
# 切换到已安装的版本
xdev use go@1.21.0
xdev use python@3.11.9

# 查看当前活动版本
xdev current
xdev current go

# 查看工具实际路径
xdev which go
```

### 2.3 卸载工具

```bash
# 卸载指定版本
xdev uninstall go@1.21.0

# 注意：必须指定完整版本号
```

### 2.4 升级工具

```bash
# 检查可用升级
xdev upgrade --check
xdev upgrade go --check

# 升级单个工具
xdev upgrade go

# 升级所有工具
xdev upgrade --all

# 升级并清理旧版本
xdev upgrade --all --cleanup
```

### 2.5 查看可用工具

```bash
# 列出所有支持的工具
xdev available

# 搜索工具
xdev search python

# 列出已安装的工具
xdev list

# 列出某工具的已安装版本
xdev list go

# 列出某工具的远程可用版本
xdev list go --remote
```

### 2.6 刷新版本列表

```bash
# 刷新单个工具的远程版本
xdev refresh go

# 刷新所有工具
xdev refresh --all
```

## 3. 项目级版本管理

> **2026-10-26 修订**：本节此前描述 `.xdev.toml`（含 `[env]` / `[tasks]` 段）、
> `xdev local` 命令族、`.xdev.lock` 锁文件三套机制。核查源码后确认
> **三者都不存在于当前实现**：
>
> | 旧描述 | 当前实现 |
> |---|---|
> | `.xdev.toml`（TOML） | `.xdev.yaml`（YAML），且**只支持 `tools` 段** |
> | `[env]` 项目级环境变量 | 不支持 |
> | `[tasks.*]` 任务编排 | 不存在（无 `xdev task` / `xdev run` 命令）|
> | `xdev local <tool>@<ver>` | 不存在 |
> | `xdev install --from-local` | 不存在（标志未注册）|
> | `.xdev.lock` + `xdev lock` | Spec 012 已删除 |
>
> 下面的内容已按当前实现重写。

### 3.1 项目配置 `.xdev.yaml`

在项目根目录创建 `.xdev.yaml` 声明工具版本：

```yaml
tools:
  go: "1.27.3"        # 精确版本
  python: "^3.12"     # semver 范围（兼容 3.12.x）
  node: "20"          # 主版本匹配
  java: latest        # 最新版
```

支持的版本语义与 `xdev install` 一致：`latest` / `lts` / `1.27.3` /
`^3.12` / `~3.11`。

**查找与生效**：从 CWD 逐级向上查找（也接受 `.xdev.yml`，最多 64 层）。
`shim` 优先按它解析，未命中回落全局 `current`。

**重要约束**：

- 只解析**本机已安装**的版本，不联网
- 声明的版本未安装时回落全局 `current`，并在 stderr 提示一次
- 无法在项目配置中声明环境变量或任务 —— 需要时用 `xdev isolate` 或
  Shell 层面的方案（如 direnv）

### 3.2 生效方式

项目配置由 `shim` 在**每次执行工具时**按 CWD 读取，因此无需显式"切换"：

```bash
cd ~/proj-a && go version    # 按 proj-a/.xdev.yaml
cd ~/proj-b && go version    # 按 proj-b/.xdev.yaml
```

若希望 `PATH` 与环境变量也随 `cd` 变化（而不只是 shim 解析），
需启用 cd hook：

```bash
eval "$(xdev hook --shell bash)"   # 手动加载（当前 shell）
xdev activate bash                 # 注入到 Shell RC（永久）
xdev deactivate                    # 移除
```

### 3.3 可复现安装的现状

`.xdev.lock` / `xdev lock` 已在 Spec 012 删除。当前推荐做法：

```bash
# 1. 项目里钉精确版本（写进 .xdev.yaml 并提交）
#    tools:
#      go: "1.27.3"
#      python: "3.11.9"

# 2. 新机器上按清单显式安装
xdev install go@1.27.3 python@3.11.9

# 3. 或整体导出/导入环境
xdev profile export > env.json      # 旧机器
xdev profile import < env.json      # 新机器
```

`xdev profile` 包含工具、版本、环境变量、代理与镜像源配置，是目前
最接近"环境可复现"的能力。


## 4. 环境管理

### 4.1 环境变量输出

```bash
# 输出工具的环境变量（用于 eval）
xdev env go@1.22.0

# PowerShell 格式
xdev env go@1.22.0 --shell powershell

# 示例输出 (bash):
# export GOROOT="E:\XDev\install\go\1.22.0"
# export GOPATH="E:\XDev\install\go\..\gopath"
```

### 4.2 隔离 Shell

```bash
# 进入包含所有工具环境的子 Shell
xdev shell

# 进入仅包含指定工具的子 Shell
xdev shell go@1.22 python@3.12

# 指定 Shell 类型
xdev shell --shell bash
xdev shell --shell pwsh
```

### 4.3 Windows PATH 管理

```bash
# 查看当前 PATH 模式
xdev config get path.mode

# 设置 PATH 模式
xdev config set path.mode shim     # 通过 shim 目录（推荐）
xdev config set path.mode direct   # 直接添加 current/<tool>/bin
```

**PATH 模式说明**：

| 模式 | 机制 | 优点 | 缺点 |
|------|------|------|------|
| `shim` | `shims/` 目录前置 PATH | 切换版本无需修改 PATH | 每次调用有 shim 调度开销 |
| `direct` | `current/<tool>/bin` 加入 PATH | 无调度开销 | 切换版本需修改注册表 PATH |

## 5. 服务管理

### 5.1 服务命令

```bash
# 列出可用服务
xdev service list

# 启动服务
xdev service start mysql

# 查看服务状态
xdev service status
xdev service status mysql

# 查看服务日志
xdev service logs mysql

# 实时跟踪日志
xdev service logs mysql -f

# 停止服务
xdev service stop mysql

# 重启服务
xdev service restart mysql
```

### 5.2 服务标记文件

服务通过 `<InstallDir>/<service>/.xdev-service` 标记文件定义：

```
type=database
version=8.0.36
port=3306
exec=mysql/bin/mysqld
datadir=data
```

### 5.3 服务环境变量

启动服务时自动设置：
- `<NAME>_PORT` — 服务端口
- `<NAME>_DATA_DIR` — 数据目录
- 标记文件中的其他键值对（`_exec` 除外）

## 6. 配置管理

### 6.1 查看配置

```bash
# 列出所有配置
xdev config list

# 查看单个配置
xdev config get core.install_dir
xdev config get mirror.python
xdev config get path.mode
```

### 6.2 修改配置

```bash
# 设置配置值
xdev config set mirror.python https://mirrors.aliyun.com/python
xdev config set path.mode shim
xdev config set core.log_level debug
```

### 6.3 代理配置

```bash
# 查看代理设置
xdev config proxy

# 设置代理
xdev config proxy --http http://proxy:8080 --https http://proxy:8080 --enable

# 设置 No Proxy
xdev config proxy --no-proxy "localhost,*.internal"

# 禁用代理
xdev config proxy --disable
```

### 6.4 镜像配置

默认镜像源（中国优化）：

| 工具 | 默认镜像 |
|------|----------|
| Python | `https://mirrors.aliyun.com/python` |
| Node.js | `https://npmmirror.com/mirrors/node` |
| Go | `https://goproxy.cn/dl` |
| Rust | `https://rsproxy.cn/rustup` |

```bash
# 设置自定义镜像
xdev config set mirror.python https://pypi.tuna.tsinghua.edu.cn/simple

# 镜像速度测试（GUI 中自动执行）
# CLI 中通过下载时自动选择最快镜像
```

## 7. 环境隔离

### 7.1 隔离模式

```bash
# 查看隔离状态
xdev isolate setup --mode none

# 容器隔离（需要 Docker）
xdev isolate setup --mode container

# Nix 风格隔离
xdev isolate setup --mode nix

# 在隔离环境中执行命令
xdev isolate exec -- go test ./...

# 销毁隔离环境
xdev isolate teardown
```

## 8. 远程开发

### 8.1 Docker 远程环境

```bash
# 创建 Docker 远程环境
xdev remote create --adapter docker --image ubuntu:22.04

# 列出远程环境
xdev remote list

# 连接到远程环境
xdev remote connect <id>

# 销毁远程环境
xdev remote destroy <id>
```

### 8.2 SSH 远程环境

```bash
# 创建 SSH 远程环境
xdev remote create --adapter ssh --host dev.example.com --user developer --ssh-key ~/.ssh/id_ed25519

# 连接
xdev remote connect <id>
```

## 9. AI 助手

### 9.1 前置条件

需要本地运行 Ollama 并拉取模型：

```bash
# 安装 Ollama (https://ollama.com)
ollama serve

# 拉取模型
ollama pull llama3
```

### 9.2 AI 命令

```bash
# 检查 AI 服务状态
xdev ai status

# 配置 AI
xdev ai config --url http://localhost:11434 --model llama3 --timeout 60s

# AI 推荐工具版本
xdev ai suggest go

# AI 诊断环境问题
xdev ai diagnose

# 在 doctor 中使用 AI
xdev doctor --ai
```

## 10. 插件系统

### 10.1 内置插件

| 分类 | 工具 |
|------|------|
| 核心 | python |
| 语言 | node, go, rust, dotnet, php, ruby, lua, perl, dart, kotlin, bun, deno, zig, julia, haskell, scala, erlang, typescript |
| Android | android-platform-tools, android-build-tools, android-platform, android-ndk, android-cmdline-tools, android-emulator |
| 构建工具 | maven, gradle, cmake, ninja, make, git, llvm, mingw, sbt |
| Flutter | flutter |
| 包管理 | uv |
| 自定义逻辑 | java |

### 10.2 Lua 插件

```bash
# 列出已安装的 Lua 插件
xdev plugin list

# 安装 Lua 插件
xdev plugin install https://example.com/xdev-plugin-elixir.lua

# 创建 Lua 插件模板
xdev plugin create elixir

# 移除 Lua 插件
xdev plugin remove elixir
```

**Lua 插件模板**：

```lua
PLUGIN = {
  name = "elixir",
  display_name = "Elixir",
  category = "language",
  description = "The Elixir programming language",
  homepage = "https://elixir-lang.org",
  shims = {"elixir", "elixirc", "iex", "mix"},
  env_vars = {
    ELIXIR_HOME = "{{install}}"
  }
}

function available()
  -- 返回可用版本列表
  return {"1.16.0", "1.15.7", "1.14.5"}
end

function archive(version, os, arch)
  -- 返回下载 URL 和格式
  return {
    url = "https://github.com/elixir-lang/elixir/releases/download/v" .. version .. "/elixir-otp-26.zip",
    format = "zip"
  }
end
```

### 10.3 Vfox 兼容插件

xdev 兼容 version-fox 格式的 Lua 插件，放入 `$XMDEV_HOME/plugins/` 即可自动加载。

## 11. 清单管理

### 11.1 内置清单

xdev 嵌入了 40+ 工具的清单 YAML，无需网络即可查看可用工具。

### 11.2 第三方清单

```bash
# 添加第三方清单仓库
xdev manifest add https://github.com/example/xdev-manifests

# 列出所有清单（含第三方）
xdev manifest list

# 更新第三方清单
xdev manifest update

# 移除第三方清单
xdev manifest remove custom-tool
```

### 11.3 清单签名

```bash
# 生成 Ed25519 密钥对
xdev keygen --pub-key public.key --priv-key private.key

# 签名清单
xdev sign-manifests --manifests-dir manifests --priv-key private.key

# 填充 SHA-256（发布前操作）
xdev fill-sha256 --manifests-dir manifests --dry-run
xdev fill-sha256 --manifests-dir manifests
```

## 12. 配置文件导入/导出

### 12.1 导出环境配置

```bash
# 导出当前环境到 JSON 文件
xdev profile export my-setup.json
```

导出内容包含：
- 已安装工具及版本
- 活动版本
- PATH 模式
- 代理设置
- 镜像配置
- 用户环境变量
- GUI 收藏
- 自定义工具

### 12.2 导入环境配置

```bash
# 从 JSON 文件导入环境
xdev profile import my-setup.json
```

导入操作会：
1. 应用 PATH 模式
2. 设置代理和镜像
3. 安装缺失的工具
4. 激活指定版本
5. 设置用户环境变量
6. 恢复收藏和自定义工具

## 13. 诊断与修复

### 13.1 健康检查

```bash
# 运行完整诊断
xdev doctor

# 自动修复问题
xdev doctor --fix

# AI 辅助诊断
xdev doctor --ai
```

**检查项目**：
1. XDev Home 目录存在且可写
2. 所有必需子目录存在
3. Shim 目录在 PATH 中
4. Shell RC 包含 xdev hook
5. Shim 二进制文件存在
6. 工具清单非空
7. 用户 PATH 无异常
8. 系统 PATH 无异常

### 13.2 常见问题

#### 问题：Shim 不生效

```bash
# 检查 shim 目录是否在 PATH
xdev doctor

# 修复 PATH
xdev doctor --fix

# 手动添加 shim 目录到 PATH
# Windows: xdev 会自动写入注册表
# Unix: 确保 shell RC 中有 eval "$(xdev hook --shell bash)"
```

#### 问题：版本切换后环境变量未更新

```bash
# 重新激活
xdev use go@1.22.0

# 或重启 Shell
# Windows: 新开命令提示符/PowerShell
# Unix: 新开终端或 source ~/.bashrc
```

#### 问题：下载速度慢

```bash
# 检查镜像配置
xdev config list

# 设置国内镜像
xdev config set mirror.python https://mirrors.aliyun.com/python
xdev config set mirror.node https://npmmirror.com/mirrors/node

# 或启用代理
xdev config proxy --http http://proxy:8080 --enable
```

#### 问题：Windows 注册表 PATH 损坏

```bash
# 运行诊断
xdev doctor

# 自动修复
xdev doctor --fix
```

## 14. 自更新与卸载

### 14.1 更新 xdev

```bash
# 检查并更新
xdev self update

# 查看当前版本
xdev self version
# 或
xdev version
```

### 14.2 卸载 xdev

```bash
# 完整卸载
xdev self uninstall

# 卸载流程：
# 1. 移除所有 shim
# 2. 清理 Shell RC 中的 hook
# 3. 移除环境变量片段
# 4. 移除 current/ junction
# 5. 可选：删除整个数据目录
# 6. 提示用户手动删除二进制文件
```

## 15. Shell 补全

```bash
# 生成 bash 补全
xdev completion bash > /etc/bash_completion.d/xdev

# 生成 zsh 补全
xdev completion zsh > "${fpath[1]}/_xdev"

# 生成 fish 补全
xdev completion fish > ~/.config/fish/completions/xdev.fish

# 生成 PowerShell 补全
xdev completion powershell > XDev-completion.ps1
```

## 16. 任务运行器（不存在）

> **2026-10-26 修订**：本节此前描述 `.xdev.toml` 的 `[tasks.*]` 配置与
> `xdev task list` / `xdev task run` / `xdev run` 三条命令。
>
> 核查确认**这些能力都不存在于当前实现**：
> - `.xdev.yaml` 的 `internal/project` 只解析 `tools` 段，无 `tasks`
> - `xdev task` / `xdev run` 不是注册的子命令（实测
>   `error: unknown command "task" for "xdev"`）
>
> 这里的节号保留为空节，是为了不打乱后续章节编号（第 17 节起为 GUI/全局标志）。
>
> **替代方案**：任务编排请用 Make / Task / just / npm scripts 等专用工具；
> xdev 的职责边界是「管理工具版本」，不承担构建编排。


## 17. GUI 操作

### 17.1 启动 GUI

```bash
# 启动桌面 GUI
xdev gui
```

### 17.2 GUI 功能

- **工具浏览**：按分类浏览可用工具，查看版本列表
- **一键安装**：点击安装/切换/卸载工具版本
- **本地检测**：扫描系统中已安装的工具，一键迁移到 xdev 管理
- **服务管理**：启动/停止/监控开发服务
- **配置编辑**：可视化编辑配置、代理、镜像
- **AI 助手**：集成 Ollama 的智能建议
- **收藏夹**：标记常用工具快速访问
- **自定义工具**：添加不在清单中的工具
- **配置导入/导出**：一键同步环境配置
- **首次运行向导**：引导新用户完成初始化

## 18. 全局标志参考

| 标志 | 缩写 | 类型 | 默认值 | 说明 |
|------|------|------|--------|------|
| `--xmdevenv-dir` | | string | `""` | 覆盖 XDev Home 目录 |
| `--verbose` | `-v` | bool | `false` | 详细日志输出 |
| `--no-color` | | bool | `false` | 禁用彩色输出 |
| `--no-path-fix` | | bool | `false` | 不修改 PATH 和注册表环境变量 |

**`--no-path-fix` 作用范围**：
- 跳过 Shell RC 注入
- 跳过 Windows 注册表 PATH 修改
- 跳过 Windows 注册表环境变量写入（GOROOT、GOPATH 等）
- 跳过 `WM_SETTINGCHANGE` 广播
- **用于 CI/CD 和集成测试，避免污染系统环境**
