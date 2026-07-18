> **文档类型**：Backlog
> **状态**：Frozen（待转换）
> **来源**：IMPROVEMENT-PLAN.md
> **P4B 迁移目标**：docs/backlog.md
> **替代文档**：无
> **治理记录**：P4A 元数据治理，2026-07-16

# XDev 改进建议文档（执行级）

> 版本: v1.0 | 日期: 2026-07-01 | 当前版本: v1.2.2-alpha
>
> 本文档整合了功能增强、Bug 修复、UI 风格、UI 布局、操作逻辑五个维度的改进建议，
> 按 6 个批次组织优先级，每项包含目标、方案、涉及文件、工作量估算和验收标准。

---

## 目录

- [第一批：已有功能缺陷修复](#第一批已有功能缺陷修复)
- [第二批：常用功能补齐](#第二批常用功能补齐)
- [第三批：UI 布局重构与体验提升](#第三批ui-布局重构与体验提升)
- [第四批：操作逻辑与交互增强](#第四批操作逻辑与交互增强)
- [第五批：UI 风格与视觉完善](#第五批ui-风格与视觉完善)
- [第六批：长期功能拓展](#第六批长期功能拓展)
- [附录 A：Bug 修复清单（已完成）](#附录-abug-修复清单已完成)
- [附录 B：代码重构成果（已完成）](#附录-b代码重构成果已完成)

---

## 第一批：已有功能缺陷修复

> 优先级: 高 | 风险: 低 | 目标: 修复已有功能中的逻辑缺陷和集成缺口

### 1.1 GUI 隔离功能修复

**问题**: `IsolateExec` / `IsolateTeardown` 硬编码 `ModeNone`，容器/Nix 隔离在 GUI 中完全不工作。

**方案**:
1. 在 `App` 结构体增加 `isolatorMode IsolationMode` 和 `currentIsolator isolator.Isolator` 字段
2. `IsolateSetup` 保存创建的 isolator 实例到 App
3. `IsolateExec` 读取保存的 isolator 执行命令，而非硬编码 `ModeNone`
4. `IsolateTeardown` 调用保存的 isolator 的 Teardown
5. 新增 `IsolateStatus` 返回当前活跃的隔离模式和状态

**涉及文件**:
| 文件 | 行号 | 改动 |
|------|------|------|
| `internal/gui/app.go` | 233-263 | App 结构体增加 isolator 字段 |
| `internal/gui/app_project.go` | 395-430 | IsolateSetup/Exec/Teardown/Status 重写 |

**工作量**: 4h | **验收**: GUI 中选择 container 模式 → Setup → Exec 命令在容器内执行 → Teardown 容器被删除

---

### 1.2 uninstall 清理 lockfile

**问题**: `xdev uninstall` 不更新 `.xdev.lock`，导致 lockfile 中残留已卸载的工具。

**方案**: 在 `newUninstallCmd` 的 RunE 中，成功卸载后调用 `lockfile.Load()` → `RemoveTool(name)` → `Save()`。

**涉及文件**:
| 文件 | 行号 | 改动 |
|------|------|------|
| `internal/cli/install.go` | 106-130 | uninstall RunE 增加 lockfile 清理逻辑 |

**工作量**: 1h | **验收**: 安装工具 → lockfile 生成 → 卸载 → lockfile 中对应条目被移除

---

### 1.3 签名验证接入安装流程

**问题**: `signer` 包生成签名，但 `xdev install` 时不验证。

**方案**:
1. 确保 `installer.downloadWithProgress` 传递 `ManifestSignature` 给 downloader
2. 在 `downloader.verifyManifestSignature` 中，当签名存在但匹配失败时返回 error（当前只 log warning）
3. 当签名文件不存在时，打印明确警告但不阻止安装

**涉及文件**:
| 文件 | 行号 | 改动 |
|------|------|------|
| `internal/downloader/downloader.go` | 335-382 | verifyManifestSignature 改为返回 error |
| `internal/installer/installer.go` | 184-250 | 确保传递签名 |

**工作量**: 3h | **验收**: 签名文件存在时 → 篡改的归档被拒绝；签名文件不存在时 → 打印警告但继续安装

---

### 1.4 `refresh --all` 批量刷新

**问题**: 只能逐个工具刷新远程版本列表。

**方案**:
- 添加 `--all` 标志，遍历 catalog 中所有有 `RemoteSource` 的工具
- 并发刷新，限流 3 个并发（避免 GitHub API 速率限制）
- 打印进度：`[3/15] Refreshing python...`

**涉及文件**:
| 文件 | 行号 | 改动 |
|------|------|------|
| `internal/cli/refresh.go` | 全文 | 增加 `--all` 标志和并发逻辑 |

**工作量**: 3h | **验收**: `xdev refresh --all` 刷新所有有远程源的工具，3 个并发，打印进度

---

### 1.5 `plugin remove` 命令

**问题**: 可以安装和创建插件，但无法删除。

**方案**: 新增 `xdev plugin remove <name>` 命令，删除 `~/.xdev/plugins/<name>.lua` 文件。

**涉及文件**:
| 文件 | 行号 | 改动 |
|------|------|------|
| `internal/cli/plugin.go` | 全文 | 新增 remove 子命令 |

**工作量**: 1h | **验收**: `xdev plugin remove testtool` → 文件被删除 → `xdev plugin list` 不再显示

---

### 1.6 GUI 远程开发 SSH 支持

**问题**: GUI 中 `ListRemoteInstances` 和 `CreateRemoteInstance` 硬编码 Docker 适配器。

**方案**:
1. `CreateRemoteInstance` 增加 `adapter` 参数（`docker` / `ssh`）
2. SSH 模式下接收 host/user/port/keyPath 参数
3. `ListRemoteInstances` 同时列出 Docker 容器和已保存的 SSH 实例标记文件

**涉及文件**:
| 文件 | 行号 | 改动 |
|------|------|------|
| `internal/gui/app_project.go` | 440-470 | CreateRemoteInstance 增加 adapter 分支 |
| `internal/gui/app_project.go` | 452-460 | ListRemoteInstances 读取 SSH 实例标记 |
| `frontend/src/AppContent.vue` | 831-880 | 远程页面增加适配器选择和 SSH 表单 |

**工作量**: 4h | **验收**: GUI 中选择 SSH → 填写连接信息 → Create → List 显示 → Exec 执行命令

---

### 1.7 跨平台 doctor PATH 诊断

**问题**: `doctor` 的 PATH 诊断使用 `;` 分隔符，在 Unix 上不工作。

**方案**:
- 将 `diagnosePathEntries` 中的 `;` 替换为 `string(os.PathListSeparator)`
- 增加 Unix 常见问题检测：`/usr/local/bin` 权限、shell RC 文件存在性、`/etc/profile.d/` 检查

**涉及文件**:
| 文件 | 行号 | 改动 |
|------|------|------|
| `internal/cli/doctor.go` | 200-260 | 路径分隔符 + Unix 诊断逻辑 |

**工作量**: 2h | **验收**: 在 Linux/macOS 上运行 `xdev doctor` 正确诊断 PATH 问题

---

## 第二批：常用功能补齐

> 优先级: 高 | 风险: 中 | 目标: 补齐用户期望的常用功能

### 2.1 `xdev upgrade` 命令

**问题**: 没有一键升级已安装工具到最新版本的命令。

**方案**:
```
xdev upgrade [tool]          # 升级单个工具到最新
xdev upgrade --all           # 升级所有已安装工具
xdev upgrade --check         # 仅检查可升级项，不执行
```
逻辑：遍历已安装工具 → 查询 manifest 的 `Latest` → 对比当前版本（用已修复的 `compareVersions`） → 安装新版本 → 激活 → 可选清理旧版本（`--cleanup` 标志）。

**涉及文件**:
| 文件 | 改动 |
|------|------|
| `internal/cli/upgrade_cmd.go` | 新建文件，实现 upgrade 命令 |
| `internal/cli/root.go` | 注册 `newUpgradeCmd()` |
| `internal/installer/installer.go` | 增加 `IsUpgradeAvailable(tool) (string, bool)` 方法 |

**工作量**: 6h | **验收**: 安装 python@3.11.0 → manifest 最新为 3.12.0 → `xdev upgrade python` → 激活 3.12.0

---

### 2.2 `service logs` 和 `service restart`

**问题**: 服务管理缺少日志查看和重启功能。

**方案**:
- `xdev service logs <name>` — 读取 `logs/<name>/stdout.log`，支持 `-f` follow 模式（tail -f 效果）
- `xdev service restart <name>` — 先 stop 再 start

**涉及文件**:
| 文件 | 行号 | 改动 |
|------|------|------|
| `internal/cli/service.go` | 全文 | 增加 logs 和 restart 子命令 |
| `internal/service/manager.go` | 全文 | 增加 `ReadLogs(name) (io.Reader, error)` 和 `Restart(name)` 方法 |

**工作量**: 4h | **验收**: 启动服务 → `xdev service logs myservice` 输出日志 → `xdev service restart myservice` 重启

---

### 2.3 `xdev completion <shell>` 生成补全脚本

**问题**: 无 shell 自动补全。

**方案**: 利用 Cobra 内置的 `GenBashCompletion` / `GenZshCompletion` / `GenPowerShellCompletion` / `GenFishCompletion`：
```
xdev completion bash > /etc/bash_completion.d/xdev
xdev completion zsh > "${fpath[1]}/_xdev"
xdev completion fish > ~/.config/fish/completions/xdev.fish
xdev completion powershell | Out-String | Invoke-Expression
```

**涉及文件**:
| 文件 | 改动 |
|------|------|
| `internal/cli/completion.go` | 新建文件，实现 completion 命令 |
| `internal/cli/root.go` | 注册 `newCompletionCmd()` |

**工作量**: 2h | **验收**: `xdev completion bash` 输出有效的 bash 补全脚本

---

### 2.4 AI 配置可定制化

**问题**: Ollama 模型和端点硬编码，无法通过配置文件修改。

**方案**:
1. 在 `config.yaml` 增加 `[ai]` 段：
   ```yaml
   ai:
     ollama_url: "http://localhost:11434"
     model: "codellama"
     timeout: 60s
   ```
2. `NewAssistant()` 读取配置而非硬编码
3. CLI 增加 `xdev ai config` 子命令设置/查看 AI 配置
4. GUI 设置页面增加 AI 配置区

**涉及文件**:
| 文件 | 行号 | 改动 |
|------|------|------|
| `internal/config/config.go` | 35-60 | Config 增加 `AI AIConfig` 字段 |
| `internal/ai/assistant.go` | 27-35 | NewAssistant 读取配置 |
| `internal/cli/ai_cmd.go` | 全文 | 增加 config 子命令 |
| `internal/gui/app_config.go` | 全文 | 增加 AI 配置区 |
| `frontend/src/AppContent.vue` | 254-376 | 设置页面增加 AI 配置 |

**工作量**: 5h | **验收**: 修改 config.yaml 中 model 为 `llama3` → `xdev ai suggest python` 使用 llama3

---

### 2.5 Profile 导出包含 GUI 数据

**问题**: `xdev profile export` 不包含收藏、自定义工具、环境变量设置。

**方案**: 扩展 profile JSON 格式：
```json
{
  "tools": {...},
  "env": {...},
  "proxy": {...},
  "mirror": {...},
  "favorites": [...],
  "custom_tools": [...]
}
```
导入时恢复这些数据到对应文件。

**涉及文件**:
| 文件 | 行号 | 改动 |
|------|------|------|
| `internal/cli/profile.go` | 全文 | 扩展导出/导入逻辑 |
| `internal/gui/app_favorites.go` | 全文 | 增加 `GetFavoritesForExport()` / `SetFavoritesFromImport()` |
| `internal/gui/app_custom_tools.go` | 全文 | 增加 `GetCustomToolsForExport()` / `SetCustomToolsFromImport()` |

**工作量**: 4h | **验收**: 导出 profile → 在新机器导入 → 收藏和自定义工具恢复

---

### 2.6 Task 运行器增强

**问题**: 无依赖、无变量插值、无并行、无 dry-run。

**方案**（分阶段实施）:
- **变量插值**: 支持 `{{xdev:version tool}}` 和 `$XDEV_TOOL_VERSION` 在 task 命令中使用
- **依赖链**: `.xdev.toml` 中增加 `depends_on = ["build"]` 字段，执行前自动先运行依赖
- **并行执行**: `xdev task run --parallel task1 task2`
- **`--dry-run`**: 打印将执行的命令但不运行
- **`--list --json`**: JSON 格式输出任务列表

**涉及文件**:
| 文件 | 行号 | 改动 |
|------|------|------|
| `internal/task/task.go` | 全文 | 增强 Runner 实现 |
| `internal/project/project.go` | 42-47 | Task 结构增加 `DependsOn []string` 字段 |
| `internal/cli/task.go` | 全文 | 增加 `--dry-run` / `--json` / `--parallel` 标志 |

**工作量**: 8h | **验收**: 定义 task A 依赖 task B → `xdev task run A` 先执行 B 再执行 A

---

### 2.7 `xdev init` 交互式向导

**问题**: `xdev init` 仅初始化目录结构，缺少引导新用户选择工具的交互流程。

**方案**: 扩展为多步骤交互向导：
```
$ xdev init
Welcome to xdev!

? Select your primary language:
  ❯ Python
    Node.js
    Go
    Rust
    Java
? Select version: 3.12.0
? Add more tools? (y/N)
? Inject into shell RC? (Y/n)

Installing python@3.12.0...
Done! Run `xdev doctor` to verify.
```

**涉及文件**:
| 文件 | 行号 | 改动 |
|------|------|------|
| `internal/cli/init.go` | 全文 | 重写为交互式向导 |

**工作量**: 6h | **验收**: 首次运行 `xdev init` → 选择 Python → 自动安装 → shell RC 注入 → doctor 通过

---

## 第三批：UI 布局重构与体验提升

> 优先级: 高 | 风险: 中 | 目标: 从 3057 行单体组件拆分为可维护的组件架构

### 3.1 前端组件拆分

**问题**: `AppContent.vue` 是 3057 行单体文件，16 个视图通过 `v-if` 链切换，极难维护。

**方案**: 拆分为页面组件 + 公共组件 + composables：
```
frontend/src/
  layouts/
    AppShell.vue              # 标题栏 + 侧边栏 + 内容区（~200行）
  pages/
    DashboardPage.vue         # 仪表盘（~150行）
    ToolListPage.vue          # 工具列表（~200行）
    ToolDetailDrawer.vue      # 工具详情抽屉（~300行）
    SettingsPage.vue          # 设置（~250行）
    EnvVarsPage.vue           # 环境变量（~200行）
    FavoritesPage.vue         # 收藏（~150行）
    CustomToolsPage.vue       # 自定义工具（~100行）
    ProjectPage.vue           # 项目配置（~100行）
    TasksPage.vue             # 任务（~80行）
    PluginsPage.vue           # 插件（~80行）
    ServicesPage.vue          # 服务（~100行）
    AIPage.vue                # AI 助手（~150行）
    IsolatePage.vue           # 隔离（~100行）
    RemotePage.vue            # 远程开发（~120行）
    AboutPage.vue             # 关于（~50行）
  components/
    ToolCard.vue              # 工具卡片
    VersionBadge.vue          # 版本标签
    InstallProgressModal.vue  # 安装进度弹窗
    EmptyState.vue            # 空状态
    SearchBar.vue             # 搜索栏
  composables/
    useTools.ts               # 工具列表逻辑
    useInstall.ts             # 安装逻辑
    useFavorites.ts           # 收藏逻辑
    useEnvVars.ts             # 环境变量逻辑
```
使用 `currentPage` ref + `<component :is>` 动态组件替代 `v-if` 链。

**涉及文件**: `frontend/src/AppContent.vue`（拆分源）、上述新文件

**工作量**: 24h | **验收**: 拆分后所有页面功能不变，无 console error，单文件不超过 300 行

---

### 3.2 侧边栏分组与折叠

**问题**: 侧边栏是扁平的 16 项菜单列表，无分组。

**方案**:
```
工具
  ├─ 仪表盘
  ├─ 所有工具
  ├─ 收藏
  └─ 自定义工具
环境
  ├─ 环境变量
  ├─ 项目配置
  ├─ 任务
  └─ 服务
开发
  ├─ 隔离
  ├─ 远程开发
  └─ AI 助手
设置
  ├─ 设置
  ├─ 插件
  └─ 关于
```
- 使用 `n-menu` 的 `children` 属性实现分组
- 支持折叠/展开分组
- 侧边栏可整体折叠为图标模式（48px 宽），通过顶部按钮切换

**涉及文件**: `frontend/src/layouts/AppShell.vue`

**工作量**: 4h | **验收**: 侧边栏显示 4 个分组，可展开/折叠，可切换为图标模式

---

### 3.3 全局搜索 + 命令面板（Ctrl+K）

**问题**: 搜索仅在工具列表页面可用。

**方案**:
1. 在内容头部放置全局搜索框（所有页面可见）
2. `Ctrl+K` 快捷键唤出命令面板（类似 VS Code Command Palette）
3. 搜索结果分类显示：工具、设置项、已安装版本、命令
4. 支持搜索操作（如输入 "install python" 直接跳转安装页面）

**涉及文件**:
| 文件 | 改动 |
|------|------|
| `frontend/src/components/CommandPalette.vue` | 新建，命令面板组件 |
| `frontend/src/layouts/AppShell.vue` | 增加全局搜索框和快捷键注册 |
| `frontend/src/composables/useTools.ts` | 暴露搜索接口 |

**工作量**: 8h | **验收**: 按 Ctrl+K → 输入 "python" → 显示工具、已安装版本、相关命令 → 选择跳转

---

### 3.4 窗口尺寸记忆

**问题**: 每次启动使用默认尺寸，不记忆上次窗口大小/位置。

**方案**:
1. 在 `startup()` 时从 `localStorage` 读取上次窗口尺寸
2. 调用 Wails `WindowSetSize` / `WindowSetPosition` 恢复
3. 在 `shutdown()` 时调用 `WindowGetSize` / `WindowGetPosition` 保存
4. 用 500ms 节流监听窗口 resize 事件

**涉及文件**:
| 文件 | 行号 | 改动 |
|------|------|------|
| `internal/gui/app.go` | 286-340 | startup/shutdown 增加窗口状态保存/恢复 |
| `frontend/src/layouts/AppShell.vue` | 全文 | 增加 resize 监听 |

**工作量**: 3h | **验收**: 调整窗口大小 → 关闭 → 重新打开 → 窗口恢复上次尺寸

---

### 3.5 响应式适配

**问题**: 零 media query，窄窗口下布局错乱。

**方案**:
- `n-grid` 使用 `responsive` 属性（`screen` 断点）
- 侧边栏在窗口宽度 < 800px 时自动折叠为图标模式
- 表格在窄屏时隐藏次要列（使用 `n-data-table` 的 `responsive` 配置）
- Dashboard 统计卡片从 4 列变为 2 列

**涉及文件**: `frontend/src/layouts/AppShell.vue` + 各页面组件

**工作量**: 4h | **验收**: 拖动窗口到 600px 宽 → 侧边栏折叠为图标 → 卡片变为 2 列 → 表格隐藏次要列

---

### 3.6 骨架屏替代 Spinner

**问题**: 多个页面加载时显示全屏 `n-spin`，用户体验差。

**方案**: 使用 `n-skeleton` 显示占位内容骨架。

**涉及文件**: 各页面组件（ProjectPage、PluginsPage、ServicesPage、AIPage、IsolatePage、RemotePage）

**工作量**: 3h | **验收**: 打开项目页面 → 加载期间显示骨架屏而非 spinner → 数据到达后平滑过渡到内容

---

## 第四批：操作逻辑与交互增强

> 优先级: 中 | 风险: 中 | 目标: 提升操作效率和用户体验

### 4.1 批量操作

**问题**: 无法批量安装/卸载/切换版本。

**方案**:
1. 工具列表支持多选（`n-data-table` 增加 `type: "selection"` 列）
2. 批量安装：勾选多个工具 → "批量安装"按钮 → 队列式安装
3. 批量卸载：勾选 → "卸载选中" → 确认对话框
4. 批量切换版本：选中多个工具 → 统一选版本 → 批量激活

**涉及文件**: `frontend/src/pages/ToolListPage.vue` + `internal/gui/app_install.go`

**工作量**: 6h | **验收**: 勾选 3 个工具 → 点击"批量安装" → 依次安装 → 全部完成

---

### 4.2 安装队列

**问题**: GUI 一次只能下载一个工具。

**方案**:
1. 下载管理器支持队列（扩展已有 `dlMgr`）
2. 显示队列状态：排队中 / 下载中 / 已完成 / 失败
3. 支持拖拽调整队列顺序
4. 失败自动重试（3 次）

**涉及文件**:
| 文件 | 行号 | 改动 |
|------|------|------|
| `internal/downloader/manager.go` | 全文 | 增加 Queue 方法 |
| `internal/gui/app_install.go` | 全文 | 增加 `InstallToolQueue` 方法 |
| `frontend/src/components/InstallProgressModal.vue` | 全文 | 显示队列列表 |

**工作量**: 8h | **验收**: 同时选择 3 个工具安装 → 显示队列 → 依次下载安装 → 全部完成

---

### 4.3 右键菜单

**问题**: 工具列表仅支持点击打开详情。

**方案**: `n-data-table` 行右键菜单（`@contextmenu` 或 Naive UI 的 dropdown）：
- 安装最新版
- 切换版本 → 子菜单显示可用版本
- 设为收藏
- 复制安装路径
- 在文件管理器中打开
- 卸载

**涉及文件**: `frontend/src/pages/ToolListPage.vue`

**工作量**: 4h | **验收**: 右键工具行 → 显示菜单 → 选择"复制安装路径" → 剪贴板包含路径

---

### 4.4 键盘快捷键

**问题**: 几乎无键盘快捷键（仅 Ctrl+Enter 发送 AI 消息）。

**方案**:
| 快捷键 | 功能 |
|--------|------|
| `Ctrl+K` | 命令面板 |
| `Ctrl+Shift+A` | 全局搜索 |
| `Ctrl+I` | 安装选中工具 |
| `Ctrl+U` | 卸载选中工具 |
| `Ctrl+D` | 切换收藏 |
| `Ctrl+1~9` | 切换侧边栏页面 |
| `Esc` | 关闭弹窗/抽屉 |
| `F5` | 刷新当前页面数据 |

**涉及文件**: `frontend/src/layouts/AppShell.vue`

**工作量**: 3h | **验收**: 按 Ctrl+1 跳转仪表盘 → 按 Ctrl+K 打开命令面板 → 按 Esc 关闭

---

### 4.5 操作撤销

**问题**: 卸载、切换版本、删除环境变量等操作不可撤销。

**方案**:
1. **卸载工具**: 先归档到 `cache/trash/`，30 天后自动清理。Toast 显示"已卸载" + "撤销"按钮（5 秒倒计时）
2. **切换版本**: 记录上一个激活版本，GUI 提供"回退"按钮
3. **删除环境变量**: Toast 显示"已删除" + "撤销"按钮

**涉及文件**:
| 文件 | 改动 |
|------|------|
| `internal/installer/installer.go` | Uninstall 增加归档逻辑 |
| `internal/gui/app_env_vars.go` | DeleteEnvVar 返回旧值供撤销 |
| `frontend/src/composables/useToast.ts` | 新建，支持操作链接的 toast |

**工作量**: 6h | **验收**: 卸载工具 → toast 显示"撤销" → 点击撤销 → 工具恢复

---

### 4.6 引导式首次使用

**问题**: SetupWizard 仅选择目录，不引导安装工具。

**方案**: 扩展 SetupWizard 为多步骤引导：
1. 选择安装目录（现有）
2. 自动检测已安装的本地工具 → 建议迁移
3. 选择主要开发语言（Python/Node/Go/Rust/Java）
4. 选择是否注入 Shell RC
5. 一键安装选中的工具
6. 完成 → 跳转 Dashboard

**涉及文件**:
| 文件 | 改动 |
|------|------|
| `frontend/src/SetupWizard.vue` | 重写为多步骤向导 |
| `internal/gui/app.go` | 新增 `GetInitialSetupData()` 方法返回检测数据 |

**工作量**: 8h | **验收**: 首次启动 → 向导检测到本地 Python → 建议迁移 → 选择安装 Node → 完成

---

### 4.7 Toast 操作反馈统一

**问题**: 所有操作反馈用 toast，但格式不统一。

**方案**: 统一 toast 格式：
```
✅ python@3.12.0 安装成功
❌ 安装失败：网络超时
⏳ 正在安装 node@20.10.0...
```
关键操作的 toast 包含操作链接（如"查看日志"、"撤销"）。

**涉及文件**: `frontend/src/composables/useToast.ts`（新建）+ 各页面组件

**工作量**: 3h | **验收**: 安装工具 → toast 格式统一 → 包含"查看日志"链接

---

## 第五批：UI 风格与视觉完善

> 优先级: 中 | 风险: 低 | 目标: 视觉一致性和主题支持

### 5.1 浅色模式支持

**问题**: 仅暗色模式，硬编码 `darkTheme`，无切换机制。

**方案**:
1. 提取颜色为 CSS 变量（`--xdev-bg-primary`、`--xdev-text-primary` 等）
2. 定义两套变量集：`[data-theme="dark"]` 和 `[data-theme="light"]`
3. `App.vue` 根据配置动态选择 `darkTheme` 或 `lightTheme`
4. 在 `config.yaml` 增加 `gui.theme: "dark" | "light" | "auto"`
5. GUI 设置页面增加主题切换器
6. `auto` 模式跟随系统（`prefers-color-scheme`）

**涉及文件**:
| 文件 | 行号 | 改动 |
|------|------|------|
| `frontend/src/App.vue` | 1-218 | 动态主题 + CSS 变量提取 |
| `frontend/src/styles/theme.css` | 新建 | CSS 变量定义 |
| `internal/config/config.go` | 35-60 | 增加 `GUI GUIConfig` 字段 |
| `internal/gui/app_config.go` | 全文 | 增加 `GetTheme()` / `SetTheme()` |
| `frontend/src/pages/SettingsPage.vue` | 全文 | 增加主题切换器 |

**工作量**: 8h | **验收**: 设置中切换为浅色模式 → 所有页面切换为浅色主题 → 重启后保持

---

### 5.2 统一 Setup Wizard 风格

**问题**: `SetupWizard.vue` 用紫色主题（`#7D56F4`），与主应用赛博朋克青色主题完全不一致。

**方案**: 将 SetupWizard 的颜色替换为主应用的颜色体系：
- `#7D56F4` → `#00E5FF`（主色）
- `#252540` → `#0F2035`（背景）
- `#1a1a2e` → `#0A1628`（深背景）
- 圆角统一为 `6px` / `4px`

**涉及文件**: `frontend/src/SetupWizard.vue`（`<style scoped>` 部分）

**工作量**: 1h | **验收**: SetupWizard 视觉风格与主应用一致

---

### 5.3 微交互动画

**问题**: 几乎无动画，页面切换无过渡。

**方案**:
1. 页面切换加 `<Transition name="fade-slide">` 过渡（150ms）
2. 卡片悬浮加 `transform: translateY(-2px)` 微动效
3. 安装进度条完成时加 success 脉冲动画
4. 按钮点击加 ripple 效果（Naive UI 内置）

**涉及文件**: `frontend/src/layouts/AppShell.vue` + `frontend/src/styles/transitions.css`（新建）

**工作量**: 3h | **验收**: 页面切换有淡入淡出 → 卡片悬浮有微动效 → 安装完成有脉冲

---

### 5.4 字体可配置

**问题**: 硬编码系统字体栈，代码区域无等宽字体。

**方案**:
1. GUI 设置增加"字体"选项（系统默认 / JetBrains Mono / Cascadia Code）
2. 代码展示区域（命令、版本号、路径）使用等宽字体
3. 字体配置存入 `config.yaml` 的 `gui.font_family` 字段

**涉及文件**: `frontend/src/App.vue` + `internal/config/config.go` + `frontend/src/pages/SettingsPage.vue`

**工作量**: 2h | **验收**: 设置中选择 JetBrains Mono → 代码区域字体变更

---

### 5.5 i18n 修复

**问题**: 标题栏按钮 tooltip 硬编码中文（`title="最小化"`），未使用 i18n。

**方案**: 将硬编码中文替换为 `t('window.minimize')` 等 i18n key。

**涉及文件**: `frontend/src/layouts/AppShell.vue`

**工作量**: 0.5h | **验收**: 切换到英文 → 标题栏按钮 tooltip 显示英文

---

## 第六批：长期功能拓展

> 优先级: 低 | 风险: 中-高 | 目标: 扩展功能边界

### 6.1 工具依赖管理

**问题**: 工具间无依赖关系（如 Flutter 依赖 Java/Android SDK）。

**方案**:
1. Manifest 增加 `requires: [java, android-sdk]` 字段
2. 安装时自动检查依赖，提示安装缺失依赖
3. 卸载时检查是否被其他工具依赖，给出警告
4. GUI 工具详情页显示依赖关系图

**涉及文件**:
| 文件 | 改动 |
|------|------|
| `internal/manifest/manifest.go` | Tool 增加 `Requires []string` 字段 |
| `internal/installer/installer.go` | Install 前检查依赖 |
| `internal/plugins/java/java.go` | 无需改动（依赖在 manifest 声明） |
| `manifests/flutter.yaml` | 增加 `requires: [java, android-sdk]` |

**工作量**: 6h | **验收**: 安装 flutter → 提示"需要 java，是否同时安装?" → 安装 java 后继续 flutter

---

### 6.2 版本别名

**问题**: 版本只能是精确版本号，不支持别名。

**方案**:
1. Manifest 增加 `aliases: [{ name: "lts", version: "3.12.0" }]` 字段
2. `resolver.Parse` 识别别名并解析为对应版本
3. `xdev install python@lts` → 解析为 `3.12.0`
4. GUI 版本选择器显示别名标签

**涉及文件**:
| 文件 | 改动 |
|------|------|
| `internal/manifest/manifest.go` | Tool 增加 `Aliases []VersionAlias` 字段 |
| `internal/resolver/resolver.go` | Resolve 支持别名查找 |
| `internal/plugin/generic.go` | ResolveVersion 增加别名处理 |

**工作量**: 4h | **验收**: `xdev install python@lts` → 安装别名对应的版本

---

### 6.3 工具使用统计

**问题**: 无法知道哪些工具最常用。

**方案**:
1. Shim 调用时记录工具使用次数（写入 `~/.xdev/stats.jsonl`，每行一条 JSON）
2. Dashboard 显示"最常用工具" Top 5（按调用次数排序）
3. 建议清理长期未使用的工具（30 天未调用）

**涉及文件**:
| 文件 | 改动 |
|------|------|
| `internal/shim/shim.go` | run() 中记录使用统计 |
| `internal/gui/app_local_detect.go` | 增加 `GetUsageStats()` 方法 |
| `frontend/src/pages/DashboardPage.vue` | 增加 Top 5 卡片 |

**工作量**: 5h | **验收**: 使用 python 数次 → Dashboard 显示 python 为最常用工具

---

### 6.4 通知中心

**问题**: 更新检查结果只在 Dashboard 手动触发时显示。

**方案**:
1. 启动时后台检查：xdev 更新、已安装工具新版本、manifest 更新
2. 侧边栏底部显示通知角标（红点 + 数字）
3. 通知中心页面：显示所有通知，可逐条处理（更新/忽略）

**涉及文件**:
| 文件 | 改动 |
|------|------|
| `internal/gui/app.go` | startup 增加后台检查 goroutine |
| `internal/gui/app_notifications.go` | 新建，通知管理 |
| `frontend/src/pages/NotificationsPage.vue` | 新建 |
| `frontend/src/layouts/AppShell.vue` | 侧边栏增加通知入口 |

**工作量**: 8h | **验收**: 有工具新版本 → 侧边栏显示红点 → 打开通知中心 → 点击更新

---

### 6.5 日志查看器

**问题**: 日志只能通过 `GetLogPath()` 获取路径后手动打开。

**方案**:
1. GUI 增加日志查看页面（实时滚动 + 级别过滤 + 搜索）
2. 安装失败时自动跳转日志页面并高亮错误行
3. CLI 增加 `xdev logs` 命令输出最近日志

**涉及文件**:
| 文件 | 改动 |
|------|------|
| `internal/gui/app_logs.go` | 新建，日志读取接口 |
| `frontend/src/pages/LogsPage.vue` | 新建，日志查看组件 |
| `internal/cli/logs.go` | 新建，CLI logs 命令 |
| `internal/cli/root.go` | 注册 `newLogsCmd()` |

**工作量**: 6h | **验收**: 安装失败 → 自动跳转日志页面 → 高亮错误行

---

### 6.6 环境快照

**问题**: 无法保存和恢复完整环境状态。

**方案**:
- `xdev snapshot save <name>` — 保存当前所有工具版本到快照文件
- `xdev snapshot restore <name>` — 恢复到快照状态（自动安装缺失版本）
- `xdev snapshot list` — 列出所有快照
- `xdev snapshot diff <name>` — 对比当前环境与快照差异
- GUI 增加"快照管理"页面

**涉及文件**:
| 文件 | 改动 |
|------|------|
| `internal/snapshot/snapshot.go` | 新建包，快照管理 |
| `internal/cli/snapshot.go` | 新建，CLI 命令 |
| `internal/cli/root.go` | 注册 `newSnapshotCmd()` |
| `internal/gui/app_snapshot.go` | 新建，GUI 接口 |
| `frontend/src/pages/SnapshotPage.vue` | 新建 |

**工作量**: 10h | **验收**: 保存快照 → 卸载某工具 → 恢复快照 → 工具自动重装

---

### 6.7 TUI 功能补齐

**问题**: TUI 仅支持安装/卸载/列表，缺少大量功能。

**方案**（分阶段）:
- Phase 1: 项目配置查看、任务运行、搜索增强
- Phase 2: 服务管理、插件列表
- Phase 3: AI 建议、隔离状态

**涉及文件**: `internal/tui/` 下所有文件

**工作量**: 16h | **验收**: TUI 中按 `t` 运行任务 → 按 `p` 查看项目配置 → 按 `s` 管理服务

---

### 6.8 asdf 插件兼容层

**问题**: vfox 兼容已有，但 asdf 插件（Bash 脚本）不兼容。

**方案**:
1. 新增 `internal/plugin/asdf_plugin.go`，解析 asdf 插件的 `bin/install`、`bin/list-all` 等脚本
2. 通过 `sh -c` 执行 Bash 脚本并解析输出
3. 在 `register.go` 中扫描 `~/.xdev/plugins/asdf/` 目录
4. 支持 `.tool-versions` 文件中的 asdf 工具名

**涉及文件**:
| 文件 | 改动 |
|------|------|
| `internal/plugin/asdf_plugin.go` | 新建，asdf 插件适配器 |
| `internal/cli/register.go` | 增加 asdf 插件扫描 |
| `internal/cli/plugin.go` | 增加 `xdev plugin install-asdf <url>` |

**工作量**: 12h | **验收**: 安装 asdf 插件 → `xdev available <tool>` 显示版本 → `xdev install <tool>@<ver>` 安装

---

### 6.9 缓存自动清理策略

**问题**: 下载缓存无限增长，需手动清理。

**方案**:
1. 配置增加 `cache.max_size: "5GB"` 和 `cache.max_age: "30d"`
2. 安装成功后自动清理超过 max_age 的旧缓存
3. `xdev doctor` 检查缓存大小并建议清理
4. GUI 设置页面显示缓存大小和清理按钮（增强已有功能为自动策略）

**涉及文件**:
| 文件 | 改动 |
|------|------|
| `internal/config/config.go` | CoreConfig 增加 CacheMaxSize / CacheMaxAge |
| `internal/installer/installer.go` | 安装后调用 `cleanOldCache()` |
| `internal/gui/app_config.go` | `GetCacheBreakdown` 增加自动清理建议 |

**工作量**: 4h | **验收**: 缓存超过 5GB → 安装新工具后自动清理最旧缓存

---

### 6.10 `xdev use --global` vs `--local` 区分

**问题**: `xdev use` 只修改全局激活版本，无法方便地在项目级别切换。

**方案**:
- `xdev use python@3.12` — 全局切换（现有行为）
- `xdev use --local python@3.12` — 写入 `.xdev.toml`（等同于 `xdev local python@3.12`）
- `xdev use --global python@3.12` — 显式全局切换
- `xdev use --project python@3.12` — 仅当前 shell 会话生效

**涉及文件**: `internal/cli/install.go`（newUseCmd 增加 `--local` / `--global` / `--project` 标志）

**工作量**: 3h | **验收**: `xdev use --local python@3.12` → `.xdev.toml` 更新 → 不影响全局激活版本

---

## 附录 A：Bug 修复清单（已完成）

> 以下 20 个 Bug 已在 2026-07-01 全部修复，`go build` + `go vet` + `go test`（27 包全绿）+ `wails build` 通过。

### 安全修复（6 项）

| # | Bug | 文件 | 修复方式 |
|---|-----|------|---------|
| B1 | 下载续传数据损坏 — 服务器忽略 Range 时 `.part` 文件未截断 | `downloader.go:222` | `fromBytes==0` 时加 `O_TRUNC` |
| B4 | 7z 解压无 Zip Slip 防护 | `extractor.go:153` | 新增 `validateExtraction()` 后置检查 |
| B5 | 无 Zip Bomb 防护 | `extractor.go:104,221` | `io.LimitReader` 限制 1GiB/文件 |
| B6 | SSH 适配器禁用主机密钥验证 | `ssh_adapter.go:108` | 改用 `accept-new` + 支持 `KnownHosts` |
| B7 | Nix 表达式注入 — env var 值未转义 | `nix.go:120` | 新增 `escapeNixString()` |
| B8 | Shell 注入 — 路径/env 值未转义 | `env.go:148` / `hook.go:113` | POSIX 用单引号 + PowerShell 用 `psEscape` |

### 逻辑修复（4 项）

| # | Bug | 文件 | 修复方式 |
|---|-----|------|---------|
| B2 | SemVer 预发布比较错误 — 字典序而非数值 | `resolver.go:329` | 新增 `comparePreRelease()` 数值比较 |
| B3 | `findOtherVersion` 字典序排序 | `installer.go:556` | 改用 `compareVersions()` |
| B12 | `sortVersions` 归一化版本字符串 | `resolver.go:338` | 保留原始字符串 |
| B20 | SHA256 日志 panic | `installer.go:215` | 边界检查后截断 |

### 并发修复（4 项）

| # | Bug | 文件 | 修复方式 |
|---|-----|------|---------|
| B9 | `shim.run()` 直接 `os.Exit()` 跳过 defer | `shim.go:277` | 改为返回 `ExitCodeError` |
| B11 | `Config.Save()` 无互斥锁 | `config.go:171` | 新增 `sync.Mutex` + 原子写入 |
| B17 | `ScanLocalTools` 持锁调用子进程 | `app_local_detect.go:218` | 改为快照模式 |
| B18 | 并发注册表写入无锁 | `app_migration.go:534` | 新增 `regMu` 互斥锁 |

### 中等修复（6 项）

| # | Bug | 文件 | 修复方式 |
|---|-----|------|---------|
| B10 | `SuggestVersions` 版本号被截断 | `assistant.go:141` | `TrimLeft` 移除数字 + 编号列表剥离 |
| B13 | `InstallSelf` 新鲜度检查用 `Size==Size` | `installer.go:677` | 改为仅 `ModTime` 比较 |
| B14 | `ListContainers` 标签过滤但 `Create` 未设置标签 | `docker_adapter.go:39` | 添加 `--label xdev-managed` |
| B15 | 服务管理器用 `os.Interrupt` 作存活探测 | `manager.go:272` | 改用 `syscall.Signal(0)` |
| B16 | Windows 常量注释错误 | `manager.go:202` | 修正为 `CREATE_NEW_CONSOLE` |
| B19 | `self update` 失败返回 nil | `extra.go:51` | 改为返回 `fmt.Errorf` |

### 安全修复（新增，本次会话）

| # | Bug | 文件 | 修复方式 |
|---|-----|------|---------|
| B21 | 下载器 SSRF — remote/ai/update 的 URL 无 scheme 和 IP 过滤 | `remote.go`, `ai/assistant.go`, `update.go` | 新增 `util.IsSafeExternalURL` + `CheckRedirect` 降级保护 |
| B22 | GUI 白名单不一致 — SetEnvVar 有白名单但 Delete/Clean 没有 | `gui/app_env_vars.go` | 统一使用 `util.IsProtectedSystemEnvVar` + `util.IsAllowedEnvVarName` |
| B23 | HMAC 非常量时间比较 | `util/hmac_key.go:81` | 改用 `crypto/hmac.Equal` |
| B24 | 自定义 HMAC 实现替代标准库 | `util/hmac_key.go:86` | 改用 `crypto/hmac` 标准库 |

### 逻辑修复（新增，本次会话）

| # | Bug | 文件 | 修复方式 |
|---|-----|------|---------|
| B25 | `search` 无匹配时 exit 0 | `cli/available.go:35` | 返回 `fmt.Errorf` → Cobra 自动 exit 1 |
| B26 | `install --locked`/`--from-local` 跳过未注册工具时 exit 0 | `cli/install.go:134,432` | 统计 skipped/failed 计数 → exit 非 0 |
| B27 | `init` 不创建 `current/` 目录 | `config/config.go:354` | `EnsureDirs` 添加 `current/` 目录 |

---

## 附录 B：代码重构成果（已完成）

> 以下重构已在 2026-07-01 完成，`go build` + `go vet` + `go test` + `wails build` 全部通过。
> 新增重构于 2026-07-08（本次会话）完成。

### 插件层重构

| 改动 | 效果 |
|------|------|
| Manifest 增加 `InstallRoot` 字段 | 消除 6 个 Android 插件的 `InstallDir` 覆写 |
| 删除 34 个冗余插件文件 | 35 个插件目录 → 1 个（java），~800 行删除 |
| 合并 `LuaPlugin` + `VfoxPlugin` 为 `LuaBasePlugin` | ~200 行重复消除 |
| Java 插件死代码清理 | 删除未使用的 map，`isNFTC` 改为 `>=21` |

### 跨包 util 扩展

| 新增工具 | 替换的重复代码 |
|---------|--------------|
| `util.FileSHA256` | installer + update 中的 `fileSHA256` |
| `util.IsAllZeroHash` | installer + downloader 中的 `isAllZeroHash`/`isAllZeroHex` |
| `util.CreateJunction` | installer + env + gui 中的 4 处 junction 创建 |
| `util.ExitCode` + `util.ExecResult` + `util.RunCommand` | isolator + orchestrator 中的 3 个 `exitCode` 函数 |
| `util.ExeName` | 13+ 处内联 `.exe` 后缀检查 |
| 统一 `HostOS`/`HostArch` | installer 中过时的实现 |
| `util.CopyFile`/`util.CopyDir` 复用 | env/migrate.go 中的自定义实现 |
| `util.HumanSpeedStr`/`util.HumanETAStr` 复用 | installer 中的 `humanSpeed`/`humanETA` |

### CLI 层精简

| 改动 | 效果 |
|------|------|
| `loadCatalogAndRegistry()` | 替代 22 处重复加载 |
| `activeVersion()` / `activeInstallPath()` | 统一 10+ 处版本查找 |
| `runTaskE` 共享 | task run / run 命令合并 |
| `unsupportedToolErr` | 统一 10 处错误消息 |
| `forwardExecResult` / `warnOllamaUnavailable` | 提取重复逻辑 |

### GUI 层重构

| 改动 | 效果 |
|------|------|
| `refreshAndSaveLocalCache()` | 替代 11 处两行调用 |
| `mustGetPlugin()` | 替代 8 处 registry.Get + 错误处理 |
| `newInstaller()` | 替代 9 处 installer.New 构造 |
| `currentLink()` | 替代 12+ 处路径构造 |
| `saveJSONToCache` / `loadJSONFromCache` | 替代 3 套 save/load 对 |
| `detectToolVersion()` | 替代 6 处版本检测链 |
| `pluginToToolInfo()` | 替代 4 处 ToolInfo 构造 |

### 注册表层合并（新增，本次会话）

| 改动 | 效果 |
|------|------|
| 新增 `env.SetEnvVar/GetEnvVar/DeleteEnvVar/EnumEnvVars` | 统一 user/machine 两级访问，GUI 删除重复 syscall 层（~200 行） |

### util 层扩展（新增，本次会话）

| 新增工具 | 替换的重复代码 |
|---------|--------------|
| `util.AtomicWriteFile` | config/lockfile/manifest/refresh 中 6 处 tmp+rename 模式 |
| `util.CompareSemver` / `util.ParseSemverParts` | installer + update 中重复的版本比较/解析 |
| `util.ActiveVersion` / `util.ActiveInstallPath` | cli/installer/gui 中 4 处 current 链接读取 |
| `util.IsSafeExternalURL` | remote/ai/update 中的 URL SSRF 防护 |
| `util.IsProtectedSystemEnvVar` | GUI 中 3 份系统变量保护名单 |

---

## 附录 C：新增功能（本次会话）

> 以下功能于 2026-07-08 新增，`go build` + `go vet` + `go test`（22 个新测试）+ `wails build` 全部通过。

### 新命令

| 命令 | 说明 | 文件 |
|------|------|------|
| `xdev lock` | 从 `.xdev.toml` 重新生成 `.xdev.lock` | `internal/cli/lock.go` |
| `xdev lock --check` | 校验 `.xdev.lock` 与 `.xdev.toml` 一致性 | `internal/cli/lock.go` |
| `xdev lock --json` | JSON 格式输出 lockfile 状态 | `internal/cli/lock.go` |

### 新架构特性

| 特性 | 说明 | 文件 |
|------|------|------|
| 分层信任模型 | 内置 manifest Ed25519 签名 + 第三方 manifest TOFU 降级 | `internal/downloader/downloader.go`, `internal/installer/installer.go` |
| RequiredPlugin 依赖检查 | 安装时检查依赖、卸载时检查被依赖 | `internal/manifest/manifest.go`, `internal/plugin/plugin.go`, `internal/installer/installer.go` |
| InstallMany 批量安装 | 批量安装循环抽象，统一已装/跳过/失败处理 | `internal/installer/installer.go` |

### 新安全特性

| 特性 | 说明 | 文件 |
|------|------|------|
| 私钥安全存储 | `keys/priv.key` 已加入 `.gitignore`，公钥可公开 | `keys/`, `.gitignore` |
| 生产构建签名 | `build.ps1`/`build.sh` 自动注入公钥 | `build.ps1`, `build.sh` |

### 新测试

| 测试 | 数量 | 文件 |
|------|:----:|------|
| Generic.RequiredPlugin | 2 | `internal/plugin/required_plugin_test.go` |
| Manifest RequiredPlugin 解析 | 2 | `internal/manifest/required_plugin_test.go` |
| lock 命令 | 5 | `internal/cli/lock_test.go` |
| InstallMany 批量安装 | 4 | `internal/installer/install_many_test.go` |
| 依赖检查 | 7 | `internal/installer/dependency_test.go` |

---

## 实施总览

| 批次 | 项数 | 工作量估算 | 优先级 | 风险 |
|------|------|-----------|--------|------|
| 第一批：功能缺陷修复 | 7 | ~20h | 高 | 低 |
| 第二批：常用功能补齐 | 7 | ~35h | 高 | 中 |
| 第三批：UI 布局重构 | 6 | ~46h | 高 | 中 |
| 第四批：操作逻辑增强 | 7 | ~38h | 中 | 中 |
| 第五批：UI 风格完善 | 5 | ~14.5h | 中 | 低 |
| 第六批：长期功能拓展 | 10 | ~74h | 低 | 中-高 |
| **总计** | **42** | **~227.5h** | | |
