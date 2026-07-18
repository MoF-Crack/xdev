> **文档类型**：Roadmap
> **状态**：Frozen
> **来源**：.trae/documents/xdev-improvement-roadmap.md
> **P4B 迁移目标**：docs/roadmap.md
> **替代文档**：无
> **治理记录**：P4A 元数据治理，2026-07-16
> **迁移记录**：P4B 迁移完成，2026-07-16，源路径：.trae/documents/xdev-improvement-roadmap.md

# XDev 完善方案路线图

> 基于 2026 年 6 月与 mise / asdf / vfox / FlyEnv / uni-env 的深度对比分析
> 配置文件格式：`.xdev.toml`（TOML）

---

## 一、现状总结

### XDev 已有优势（保持）
- ✅ Windows Junction + Shim 原生架构
- ✅ CLI + TUI + GUI 三模式（唯一）
- ✅ 中国镜像源内置（阿里云/清华/npmmirror）
- ✅ Ed25519 Manifest 签名验证
- ✅ 本地工具迁移能力
- ✅ Manifest 嵌入式 FS + 远程版本发现框架
- ✅ 收藏夹版本管理

### XDev 关键不足（需补齐）

| # | 不足 | 严重度 | 竞品参考 |
|---|------|--------|---------|
| 1 | 无项目级声明式配置 | 🔴 P0 | mise (.mise.toml) / FlyEnv (.flyenv) |
| 2 | 无 cd hook 自动版本切换 | 🔴 P0 | mise / asdf / vfox / FlyEnv 均已有 |
| 3 | 无 Lockfile 机制 | 🟡 P1 | mise / uni-env |
| 4 | 无任务运行器 | 🟡 P1 | mise (mise run) |
| 5 | 无插件系统（新工具需改源码） | 🟡 P1 | asdf (Bash) / vfox (Lua) |
| 6 | 生态规模有限（30+ vs 800+） | 🟡 P1 | asdf 800+ / mise 700+ |
| 7 | 无服务编排能力 | 🟡 P2 | FlyEnv (Nginx/MySQL/Redis) |
| 8 | 无跨平台支持（仅 Windows） | 🟡 P2 | FlyEnv/mise/vfox 均已跨平台 |
| 9 | 无容器/Nix 隔离 | 🟢 P3 | uni-env (Isolator) |
| 10 | 无远程开发支持 | 🟢 P3 | uni-env (Orchestrator) |
| 11 | 无 AI 集成 | 🟢 P3 | FlyEnv (Ollama/n8n) |

---

## 二、四阶段路线图

### 🏗️ Phase 1：生存基础（P0 — 项目级版本管理）

**目标**：补齐所有竞品已具备的核心功能，让 xdev 具备项目级环境管理能力

#### 1.1 `.xdev.toml` 项目级配置文件

**新增文件**：`internal/project/project.go`

```go
// 项目级配置
type ProjectConfig struct {
    Tools    map[string]string `toml:"tools"`     // node = "20" / python = "^3.12"
    Env      map[string]string `toml:"env"`       // 环境变量
    Tasks    map[string]Task   `toml:"tasks"`     // 任务定义（Phase 2）
}

type Task struct {
    Run     string `toml:"run"`      // 命令
    Desc    string `toml:"desc"`     // 描述
    Dir     string `toml:"dir,omitempty"`  // 工作目录
    Sources []string `toml:"sources,omitempty"` // 源文件（触发条件）
}
```

**查找逻辑**：从当前目录向上递归查找 `.xdev.toml`，直到找到或到达根目录

**兼容性**：同时支持读取 `.tool-versions`（asdf 格式），作为降级方案

**涉及文件**：
- 新增 `internal/project/project.go` — 配置文件解析、查找、合并
- 新增 `internal/project/project_test.go`
- 修改 `internal/cli/root.go` — 添加 `xdev local` 子命令
- 修改 `internal/cli/install.go` — `xdev install` 支持从 `.xdev.toml` 批量安装

**CLI 命令**：
```
xdev local [tool@version]    # 设置/显示项目级版本
xdev local --unset [tool]    # 移除项目级版本
xdev local --show            # 显示当前项目配置
```

#### 1.2 cd hook 自动版本切换

**核心思路**：在 Shell RC 中注入 `cd()` 函数覆盖，每次切换目录时检测 `.xdev.toml` 并动态调整 PATH

**新增文件**：`internal/env/hook.go`

```go
// Shell hook 模板
type HookTemplate struct {
    Shell string  // bash / zsh / fish / powershell / pwsh
}

// 生成 cd hook 脚本
func GenerateHook(shell, xmdevHome string) string
```

**hook 工作流程**：
1. 用户执行 `cd /path/to/project`
2. `cd` hook 触发，执行 `xdev hook --shell bash`
3. `xdev hook` 命令读取 `.xdev.toml`，解析所需工具版本
4. 输出 `export PATH=...` 和 `export TOOL_HOME=...`
5. hook eval 这些输出，PATH 动态更新

**涉及文件**：
- 新增 `internal/env/hook.go` — hook 脚本生成
- 修改 `internal/env/env.go` — `InjectShellRC` 增加 hook 注入
- 新增 `internal/cli/hook.go` — `xdev hook` 子命令
- 修改 `internal/config/config.go` — `PathConfig.Mode` 增加 `"auto"` 选项
- 修改 `internal/shim/shim.go` — `Lookup` 支持项目级版本优先

**CLI 命令**：
```
xdev hook --shell bash       # 输出当前目录的环境变量设置
xdev activate bash           # 激活 cd hook（写入 Shell RC）
xdev deactivate              # 停用 cd hook
```

**Shell RC 注入内容示例（Bash）**：
```bash
# >>> xdev >>>
export XMDEV_HOME="$HOME/.xdev"
eval "$(xdev hook --shell bash)"
# <<< xdev <<<
```

**`xdev hook` 命令输出示例**：
```bash
export PATH="/path/to/xdev/install/node/20.18.0/bin:/path/to/xdev/install/python/3.12.0/bin:$PATH"
export NODE_HOME="/path/to/xdev/install/node/20.18.0"
```

#### 1.3 Plugin 接口扩展

**修改文件**：`internal/plugin/plugin.go`

```go
type Plugin interface {
    // ... 现有方法 ...
    PostInstall(ctx context.Context, installPath string) error  // 新增：安装后钩子
    Deactivate(installPath string) error                        // 新增：停用清理
}
```

**`Generic` 默认实现**：`PostInstall` 和 `Deactivate` 为 no-op，特定工具插件可覆盖

**涉及文件**：
- 修改 `internal/plugin/plugin.go` — 接口扩展
- 修改 `internal/plugin/generic.go` — 默认实现
- 修改 `internal/installer/installer.go` — 安装后调用 `PostInstall`，切换版本时调用 `Deactivate`

#### 1.4 Resolver 增强

**修改文件**：`internal/resolver/resolver.go`

- 支持 pre-release 标签：`3.12.0-beta.1`
- 支持范围操作符组合：`>=3.12 <4.0`（不含 `||`）
- 支持非标准版本号排序：`1.22rc1` vs `1.22.0`

---

### 🚀 Phase 2：差异化增强（P1 — Lockfile + 任务 + 插件）

**目标**：在补齐核心功能基础上，增加差异化能力

#### 2.1 Lockfile 机制

**新增文件**：`internal/lockfile/lockfile.go`

```go
type Lockfile struct {
    Version     int                       `toml:"version"`
    CreatedAt   time.Time                 `toml:"created_at"`
    Tools       map[string]LockedTool     `toml:"tools"`
}

type LockedTool struct {
    Version    string `toml:"version"`
    Resolved   string `toml:"resolved"`
    SHA256     string `toml:"sha256"`
    URL        string `toml:"url"`
    InstallPath string `toml:"install_path"`
}
```

**文件名**：`.xdev.lock`（与 `.xdev.toml` 同级目录）

**工作流**：
- `xdev install` 后自动生成/更新 `.xdev.lock`
- `xdev install --locked` 只使用 lockfile 中的精确版本，不解析
- CI/CD 场景：`xdev install --locked` 确保团队环境一致

**涉及文件**：
- 新增 `internal/lockfile/lockfile.go` — Lockfile 读写
- 修改 `internal/cli/install.go` — `--locked` 标志
- 修改 `internal/installer/installer.go` — 安装后写入 lockfile

#### 2.2 任务运行器

**新增文件**：`internal/task/task.go`

```go
type Task struct {
    Name     string
    Run      string
    Desc     string
    Dir      string
    Sources  []string
}

type Runner struct {
    cfg    *config.Config
    reg    *plugin.Registry
    tasks  map[string]Task
}
```

**`.xdev.toml` 中的任务定义**：
```toml
[tasks.build]
run = "go build -o bin/app ./cmd/app"
desc = "Build the application"

[tasks.test]
run = "go test ./..."
desc = "Run tests"

[tasks.dev]
run = "air -c .air.toml"
desc = "Start dev server with hot reload"
dir = "backend"
```

**CLI 命令**：
```
xdev task list              # 列出可用任务
xdev task run <name>        # 执行任务
xdev run <name>             # 简写
```

**涉及文件**：
- 新增 `internal/task/task.go` — 任务解析和执行
- 修改 `internal/project/project.go` — 解析 tasks 段
- 新增 `internal/cli/task.go` — `xdev task` 子命令

#### 2.3 插件系统

**新增文件**：`internal/plugin/lua_engine.go`

**方案**：嵌入 Lua VM（使用 `github.com/yuin/gopher-lua`），支持用户自定义工具插件

**插件目录**：`$XMDEV_HOME/plugins/<tool>.lua`

**Lua 插件 API**：
```lua
-- plugins/custom-tool.lua
PLUGIN = {
    name = "custom-tool",
    display_name = "Custom Tool",
    category = "build",
    shims = {"ctool"},

    -- 列出可用版本（必须实现）
    available = function()
        return {"1.0.0", "1.1.0", "2.0.0"}
    end,

    -- 返回下载信息（必须实现）
    archive = function(version, os, arch)
        return {
            url = "https://example.com/tool/" .. version .. "/tool-" .. os .. "-" .. arch .. ".tar.gz",
            format = "tar.gz",
            strip = 1
        }
    end,

    -- 返回环境变量（可选）
    env_vars = function(install_path)
        return { CUSTOM_TOOL_HOME = install_path }
    end
}
```

**涉及文件**：
- 新增 `internal/plugin/lua_engine.go` — Lua VM + 插件加载
- 新增 `internal/plugin/lua_plugin.go` — LuaPlugin 适配器（实现 Plugin 接口）
- 修改 `internal/cli/register.go` — 扫描 `$XMDEV_HOME/plugins/` 目录加载 Lua 插件
- 新增 `internal/cli/plugin.go` — `xdev plugin` 子命令

**CLI 命令**：
```
xdev plugin list            # 列出已安装插件
xdev plugin install <url>   # 安装插件
xdev plugin create <name>   # 创建插件模板
```

#### 2.4 远程版本发现实现

**当前状态**：`manifest.RemoteSource` 结构已定义，但未实现远程发现逻辑

**新增文件**：`internal/remote/fetcher.go`（增强现有 `internal/remote/`）

**实现**：
- `github` 类型：调用 GitHub Releases API，分页 + ETag 缓存
- `json` 类型：HTTP GET + JSON 路径遍历
- `html` 类型：HTTP GET + 正则提取版本号

**涉及文件**：
- 修改 `internal/remote/remote.go` — 实现三种远程源的版本发现
- 修改 `internal/manifest/manifest.go` — `Tool.Versions()` 优先使用远程发现结果
- 修改 `internal/cli/available.go` — `xdev available` 支持远程版本列表

---

### 🌐 Phase 3：生态扩展（P2 — 跨平台 + 服务编排）

**目标**：扩展 xdev 的适用范围

#### 3.1 跨平台支持（macOS / Linux）

**涉及文件**：
- 修改 `internal/env/env.go` — Shell RC 注入适配 macOS/Linux
- 修改 `internal/env/win_path_other.go` — 实现 Linux/macOS PATH 管理
- 修改 `internal/installer/installer.go` — Symlink 替代 Junction
- 修改 `internal/shim/shim.go` — Unix 使用符号链接替代二进制复制
- 修改 `internal/config/config.go` — 主目录默认路径适配
- 修改 `build.ps1` → `build.sh` — Linux/macOS 构建脚本
- 修改所有 manifest YAML — 补充 darwin/linux 平台的 Archive 条目

**优先级**：macOS（开发者主力）> Linux（服务器/CI）

#### 3.2 服务编排（GUI 面板）

**新增文件**：`internal/service/manager.go`

```go
type Service struct {
    Name     string
    Type     string  // database | web-server | cache | queue | search
    Version  string
    Status   string  // running | stopped | error
    Port     int
    DataDir  string
    Config   map[string]string
}

type Manager struct {
    services map[string]*Service
}
```

**支持的服务**（分批实现）：
- M1：MySQL / PostgreSQL / Redis / Nginx
- M2：MongoDB / Elasticsearch / RabbitMQ / Caddy
- M3：MinIO / Consul / Etcd

**GUI 面板**：在 Wails 前端新增"服务"标签页

**CLI 命令**：
```
xdev service list           # 列出可用服务
xdev service start <name>   # 启动服务
xdev service stop <name>    # 停止服务
xdev service status         # 查看状态
```

#### 3.3 .tool-versions 兼容层

**涉及文件**：
- 新增 `internal/project/tool_versions.go` — 解析 `.tool-versions` 格式
- 修改 `internal/project/project.go` — 查找时同时支持 `.xdev.toml` 和 `.tool-versions`

#### 3.4 生态扩展策略

- 兼容 asdf 插件协议：通过 Lua 适配层运行 asdf Bash 插件
- 兼容 vfox 插件：直接加载 vfox Lua 插件
- 社区 manifest 仓库：`xdev manifest add <repo-url>` 拉取第三方 manifest

---

### 🎯 Phase 4：平台突破（P3 — 容器 + 远程 + AI）

**目标**：对标 uni-env 愿景，实现环境隔离和远程开发

#### 4.1 环境隔离（Isolator）

**新增文件**：`internal/isolator/isolator.go`

```go
type IsolationMode string  // "none" | "container" | "nix"

type Isolator interface {
    Mode() IsolationMode
    Setup(ctx context.Context, spec *EnvironmentSpec) error
    Teardown(ctx context.Context) error
    Exec(ctx context.Context, cmd []string) (*ExecResult, error)
}
```

- `noneIsolator`：当前行为（直接修改 PATH）
- `containerIsolator`：生成 Dockerfile / devcontainer.json，调用 Docker API
- `nixIsolator`：生成 flake.nix，调用 `nix develop`

#### 4.2 远程开发（Orchestrator）

**新增文件**：`internal/orchestrator/orchestrator.go`

```go
type Adapter interface {
    Name() string
    Create(ctx context.Context, spec *EnvironmentSpec) (*Instance, error)
    Start(ctx context.Context, id string) error
    Stop(ctx context.Context, id string) error
    Destroy(ctx context.Context, id string) error
    Exec(ctx context.Context, id string, cmd []string) (*ExecResult, error)
}
```

内置 Adapter：`local-docker` / `ssh` / `k8s`

#### 4.3 AI 集成

- Ollama 本地模型管理（通过 xdev 安装和管理 Ollama）
- 环境配置 AI 建议：分析项目依赖，推荐工具版本
- 故障诊断 AI 助手：`xdev doctor --ai` 使用 LLM 分析问题

---

## 三、Phase 1 详细实施计划

### 3.1 新增文件清单

| 文件 | 职责 |
|------|------|
| `internal/project/project.go` | 项目级配置解析、查找、合并 |
| `internal/project/project_test.go` | 单元测试 |
| `internal/env/hook.go` | Shell cd hook 脚本生成 |
| `internal/cli/hook.go` | `xdev hook` / `xdev activate` / `xdev deactivate` 命令 |
| `internal/cli/local.go` | `xdev local` 命令 |

### 3.2 修改文件清单

| 文件 | 修改内容 |
|------|---------|
| `internal/plugin/plugin.go` | 接口增加 `PostInstall` / `Deactivate` |
| `internal/plugin/generic.go` | 默认实现（no-op） |
| `internal/installer/installer.go` | 安装后调用 `PostInstall`，切换版本调用 `Deactivate` |
| `internal/env/env.go` | `InjectShellRC` 增加 hook 注入逻辑 |
| `internal/config/config.go` | `PathConfig.Mode` 增加 `"auto"` 选项 |
| `internal/shim/shim.go` | `Lookup` 支持项目级版本优先查找 |
| `internal/resolver/resolver.go` | 支持 pre-release、范围组合 |
| `internal/cli/root.go` | 注册新子命令 |
| `internal/cli/install.go` | 支持从 `.xdev.toml` 批量安装 |
| `internal/gui/app.go` | GUI 支持项目级版本显示和切换 |
| `frontend/src/AppContent.vue` | 前端项目级版本状态展示 |

### 3.3 依赖新增

| 依赖 | 用途 |
|------|------|
| `github.com/pelletier/go-toml/v2` | TOML 配置文件解析 |

### 3.4 验证步骤

1. `xdev local node@20` → 在当前目录生成 `.xdev.toml`
2. `xdev activate bash` → Shell RC 注入 cd hook
3. `cd /project-with-xdev-toml` → PATH 自动切换到项目指定版本
4. `cd /other-project` → PATH 恢复全局版本
5. `xdev install --from-local` → 从 `.xdev.toml` 批量安装
6. `xdev hook --shell bash` → 输出当前目录环境变量
7. 单元测试覆盖：配置解析、版本解析、hook 生成

---

## 四、Phase 2 详细实施计划

### 4.1 新增文件清单

| 文件 | 职责 |
|------|------|
| `internal/lockfile/lockfile.go` | Lockfile 读写 |
| `internal/task/task.go` | 任务解析和执行 |
| `internal/task/task_test.go` | 单元测试 |
| `internal/plugin/lua_engine.go` | Lua VM + 插件加载 |
| `internal/plugin/lua_plugin.go` | LuaPlugin 适配器 |
| `internal/cli/task.go` | `xdev task` 子命令 |
| `internal/cli/plugin.go` | `xdev plugin` 子命令 |

### 4.2 依赖新增

| 依赖 | 用途 |
|------|------|
| `github.com/yuin/gopher-lua` | Lua VM 嵌入 |

### 4.3 验证步骤

1. `xdev install node@20` → 自动生成 `.xdev.lock`
2. `xdev install --locked` → 只使用 lockfile 版本
3. `xdev task list` → 列出 `.xdev.toml` 中的任务
4. `xdev run build` → 执行 build 任务
5. 编写 Lua 插件 → `xdev plugin list` 显示 → 安装使用
6. 远程版本发现：`xdev available node` 显示远程版本列表

---

## 五、架构决策记录

| 决策 | 选择 | 理由 |
|------|------|------|
| 项目配置格式 | `.xdev.toml` | TOML 表达力强，Go 生态支持好，与 mise 的 .mise.toml 类似 |
| 版本切换机制 | cd hook + PATH 动态修改 | 与 mise/vfox 一致，零延迟，无需 shim 代理 |
| 插件语言 | Lua | 与 vfox 一致，跨平台安全，比 Bash 更可靠 |
| Lockfile 格式 | TOML | 与 `.xdev.toml` 格式一致 |
| 服务编排策略 | 原生二进制管理 | 与 FlyEnv 一致，不依赖 Docker，性能更好 |
| 跨平台优先级 | macOS > Linux | 开发者主力平台优先 |

---

## 六、风险与缓解

| 风险 | 缓解措施 |
|------|---------|
| cd hook 在某些 Shell 不稳定 | 提供回退到手动 `xdev hook` 的方式 |
| Lua 插件安全风险 | 沙箱化 Lua VM，限制文件/网络访问 |
| Lockfile 与 `.xdev.toml` 不同步 | `xdev install` 时自动更新 lockfile |
| 跨平台测试覆盖不足 | GitHub Actions CI 多平台测试 |
| 服务编排与现有架构耦合 | 独立 `internal/service/` 模块，可选安装 |
