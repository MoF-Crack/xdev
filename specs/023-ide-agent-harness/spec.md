# Spec 023: IDE / Agent / Harness 管理支持

> **文档类型**：Feature Spec
> **状态**：Approved
> **文档版本**：1.0.0
> **创建日期**：2026-08-02
> **最后更新**：2026-08-20
> **所有者**：xinyan <zuijiuhjd@foxmail.com>
> **权威范围**：IDE/Agent/Harness 管理支持的目标、范围和验收
> **替代文档**：无
> **Constitution**：`1.0.0`
## 一、目标

xdev 新增对 **IDE / Agent / Harness** 三类开发工具的管理支持：

| 类别 | 示例 |
|------|------|
| IDE | JetBrains（IntelliJ/PyCharm/GoLand 等）、VS Code、Cursor、Windsurf、Zed |
| Agent（编码代理） | Claude Code、OpenCode、Cursor Agent CLI、Continue |
| Harness（工具链/执行框架） | GitHub Copilot CLI、Bazel、Nix 等（可扩展） |

用户可通过 xdev：
1. **下载/安装** 目标 IDE/Agent/Harness（复用现有 manifest + 下载器 + 安装器管线）
2. 查看该类工具清单与安装状态（复用动态分类页）
3. **安装前依赖检查**：结合工具声明的平台/依赖环境与 xdev 当前执行环境，提示"是否满足依赖条件"

## 二、现状（复用组件调研结论）

### 可直接复用
| 组件 | 位置 | 说明 |
|------|------|------|
| Manifest 体系 | `internal/manifest/manifest.go:66-177` | Tool/Version/Archive 泛化，shims/env_vars 可空（android-platform 先例 `shims: []`） |
| 下载器 | `internal/downloader/downloader.go` | 代理/镜像/SHA/TOFU/签名全部泛化 |
| 安装器 | `internal/installer/installer.go` | InstallWithProgress / installByKind / runInstaller / ExtractAndActivate / Activate 全部可复用 |
| 动态分类页 | `app_tool_query.go:50-147` + `ToolCatalogPage.vue` | 新 category 无需改页面（数据驱动） |
| 版本检测模式 | `app_local_detect.go:442-646` detectPackageManager | 依赖探测的现成模板 |

### 需补齐的差距
| # | 差距 | 位置 |
|---|------|------|
| G1 | **npm remote 类型未实现**（Claude Code/OpenCode/Copilot CLI 走 npm 分发） | `internal/remote/remote.go:356-369`；RemoteSource 缺 `package` 字段 |
| G2 | **dmg 未在 extractor switch 支持**（macOS Zed/Cursor DMG） | `internal/extractor/extractor.go:74-97` |
| G3 | **无版本化依赖声明**（如"需 Node ≥ 20"） | Tool 结构无 requires 字段；required_plugins 仅存在性 |
| G4 | **无安装前依赖提示 UI**（缺失依赖只以安装报错形式出现） | GUI 无 pre-install 状态 |
| G5 | **register.go 硬编码插件清单**（新 manifest 默认不注册） | `internal/cli/register.go:48-66` |
| G6 | 无 `{{install}}` 目标目录模板（JetBrains `/D=` 需要） | runInstaller InstallArgs 仅支持 `{{version}}`/`{{tool}}` |

## 三、功能需求

### F1: 新分类与 Manifest 支持
- 新增分类：`ide`、`agent`、`harness`
- 首批 manifest（形态与下载源已联网核实，见下表）：
  - IDE: `vscode`（zip）、`cursor`（zip/installer）、`zed`（zip/tar.gz）、`jetbrains-idea-community`（exe installer）
  - Agent: `claude-code`（npm/native）、`opencode`（npm/native + 桌面 app）、`codex`（npm/native + ChatGPT 桌面 app）
  - Harness: `copilot-cli`（npm/native）
- `GetCategories` 固定顺序加入三类（`app_tool_query.go:79`）；i18n 加 `category.ide/agent/harness`；导航分组自动注入

### F1a: 工具形态与下载源核实表（联网检索，2026-08-02）

| 工具 | 形态 | Win | macOS | Linux | npm 包 | 静默安装 | 运行时依赖 |
|------|------|-----|-------|-------|--------|----------|-----------|
| VS Code | GUI+CLI（同一 `code` 二进制；另有独立 headless CLI zip） | `VSCodeSetup-x64-*.exe` / `VSCodeUserSetup-x64-*.exe` / `VSCode-win32-x64-*.zip`（官方 `/sha/download?build=stable&os=win32-x64[-archive]`） | `VSCode-darwin-universal.dmg`（**新版为 dmg，旧 zip 参数已 404**） | `code-stable-x64-*.tar.gz` / .deb / .rpm | 无 | Inno：`/VERYSILENT /NORESTART /MERGETASKS=!runcode` | 仅 OS |
| Cursor | GUI；CLI 由安装脚本提供 | `CursorSetup-x64-*.exe`（官方 JSON API `cursor.com/api/download?platform=win32-x64`） | `Cursor-darwin-universal.dmg` | `.AppImage`/.deb/.rpm | 无 | **未官方文档化 → 待核实** | Electron 自包含 |
| Zed | GUI+CLI（`zed`/`zed.exe`） | `Zed-x86_64.exe`/`Zed-aarch64.exe`（GitHub releases；winget） | `Zed-x86_64.dmg`/`-aarch64.dmg` | `zed-linux-x86_64.tar.gz`（官方 install.sh） | 无 | **未官方文档化 → 待核实** | Linux: glibc ≥2.31/2.35, Vulkan |
| IntelliJ IDEA Community | GUI+CLI（`idea64.exe`/`idea.sh`/mac `idea`） | `idea-2025.3.exe`（NSIS）+ `.win.zip`（官方 `download.jetbrains.com/idea/`；旧 `ideaIC-*` 命名已 404） | `idea-2025.3.dmg` / `-aarch64.dmg` | `idea-2025.3.tar.gz` / `-aarch64.tar.gz` | 无 | `/S /CONFIG=<path> /D=<dir>`（D 最后且不带引号；silent.config 官方提供） | **自带 JBR 21**，无需外部 JDK |
| Claude Code | CLI/TUI（`claude` 交互；`claude -p` 脚本模式） | native `irm claude.ai/install.ps1 | iex` / winget `Anthropic.ClaudeCode` | native `curl -fsSL claude.ai/install.sh | bash` / brew | 同左 | **`@anthropic-ai/claude-code`**（README 标 npm 已弃用，native 优先） | npm 方式需 **Node ≥22**（registry engines）；native 自包含 |
| OpenCode (anomalyco/opencode, 原 sst/opencode) | **GUI（Desktop App BETA）** + CLI（`run`/`serve`）+ TUI（默认交互）——**三形态确认**；另有 `opencode web`（浏览器 GUI）与 IDE 扩展 | 桌面 app：`opencode-desktop-win-x64.exe`/`-arm64.exe`（NSIS，GitHub releases）；CLI：scoop/choco/脚本 | 桌面 app：`opencode-desktop-mac-*.dmg`（brew cask `opencode-desktop`）；CLI：brew/脚本 | 桌面 app：.deb/.rpm/**.AppImage**；CLI：脚本 | **`opencode-ai`**（无 engines 字段，独立二进制） | **无 `--silent`**；非交互用 `opencode run`；`--format json` | 独立二进制（npm 仅引导）；Docker `ghcr.io/anomalyco/opencode` |
| **OpenAI Codex**（openai/codex，**新增**） | **CLI/TUI**（`codex` 交互；`codex exec` 脚本模式）+ **GUI = `codex app` 拉起 ChatGPT 桌面应用（Win/mac 专属，Linux 无）** + Web（chatgpt.com/codex）+ IDE 扩展 | native：PS1 安装脚本 / winget `OpenAI.Codex` / exe/zip；官方源 `releases.openai.com/codex` + GitHub `rust-v*` releases | native：install.sh / brew cask `codex` / dmg | native：install.sh / musl tar.gz | **`@openai/codex`**（**Node ≥16**，JS 引导 Rust 二进制） | `codex exec` 非交互；`--json` / `--output-last-message` | npm 路径 Node ≥16；Rust 二进制；macOS 12+/Win11/WSL2；winget 依赖 ripgrep+VC redist |
| JetBrains Toolbox（可选） | GUI（无 CLI） | `jetbrains-toolbox-*.exe`（官方 API） | `.dmg` | `.tar.gz` | 无 | **未文档化 → 待核实** | Linux 需 libxi6/libgtk 等 |

**关键结论**：
1. **GUI+CLI 同一二进制**：VS Code/JetBrains/Zed/Cursor 的 CLI 是 GUI 的进程启动器（opener），非独立工具——`form_factor: [gui, cli]` + `gui_binary` 即可，无需额外下载
2. **Agent 三类全有 CLI/TUI 双模式**：`claude`/`copilot`/`codex` 交互 TUI + `-p`/`exec`/`run` 脚本 CLI；**OpenCode 与 Codex 另有独立 GUI**：
   - OpenCode：`form_factor: [gui, cli, tui]`——桌面 App（BETA）独立下载（dmg/exe/deb/rpm/AppImage），与 CLI 同版本线
   - Codex：`form_factor: [gui, cli, tui]`——GUI 是 `codex app` 拉起的 **ChatGPT 桌面应用**（Win/mac 专属，Linux 无独立桌面 GUI）
3. **下载源关键变化**：VS Code mac 已转 dmg（旧 zip 404）；JetBrains `ideaIC-*` 命名已废弃；Windsurf 已并入 Devin Desktop（**从首批清单移除**，待 Devin 稳定）；OpenCode 仓库 `sst/opencode` → **`anomalyco/opencode`**
4. **依赖核查结论**：Claude Code 与 Copilot CLI 走 npm 需 Node ≥22；Codex npm 需 Node ≥16；OpenCode npm 无 Node 要求（独立二进制）；native 安装（install.sh/ps1）免 Node——manifest 的 `depends` 应区分安装路径
5. **待核实项**（实施前冻结）：Cursor/Zed Windows 静默参数、Claude Code README 18 vs registry 22、Windsurf→Devin 归属、Codex 桌面 GUI 是否纳入 xdev 管理（其安装器由 ChatGPT 桌面应用提供，xdev 仅管理 CLI/TUI 部分）

### F1b: 工具形态（CLI / TUI / GUI）模型

同一工具可能同时存在多种使用形态，需在 manifest 中显式声明，并贯穿完整性检查、启动入口与 UI 展示：

```yaml
name: vscode
category: ide
form_factor: [gui, cli]     # 可选，缺省由 shims 推断为 [cli]
gui_binary: bin/code.exe    # GUI 主二进制（相对 install 根；供启动/完整性）
```

- `form_factor` 取值：`gui`、`cli`、`tui`（可组合）
  - `cli`：命令行工具（由现有 `shims` 声明命令名，如 code、claude）
  - `tui`：终端交互界面（本质也是 CLI 命令，复用 shims；标注以便 UI 区分提示"交互式终端界面"）
  - `gui`：桌面图形应用（新增 `gui_binary` 声明入口可执行文件）
- 缺省规则：未声明 `form_factor` 时，按 `shims` 推断为 `[cli]`（向后兼容所有现有 manifest）；`gui_binary` 仅当含 `gui` 时有效
- **完整性检查**（`integrity.go`）：含 `gui` 时校验 `gui_binary` 存在；含 `cli/tui` 时沿用 shims 检查；两类中至少一类通过即完整
- **启动入口**（F8）：`gui` → 拉起 `gui_binary`；`cli/tui` → 不自动执行，提供"复制命令"（如 `code .` / `claude`）到终端
- **UI 展示**：工具卡片/详情显示形态徽标（GUI / CLI / TUI），详情页"启动"按钮仅对含 `gui` 的工具显示
- **依赖检查**（F5）：与形态无关（如 claude-code 无论 CLI/TUI 都需 Node ≥ 20）；GUI 工具可能额外声明运行时依赖（如 JetBrains 需 JDK）

### F2: npm 远程类型支持（G1）
- `RemoteSource` 加 `Package string` 字段
- `remote.go` 增加 npm registry 解析（GET `https://registry.npmjs.org/<pkg>` → dist-tags.latest + versions 列表）
- 支持 `type: npm, package: xxx` 的 manifest 版本刷新

### F3: dmg 支持（G2）
- `extractor.go` 增加 `dmg` 分支（macOS 下用 hdiutil 挂载复制，或 7z 解包 dmg——按实现难度选择；Linux/Windows 报不支持）

### F4: 依赖环境声明与检查（G3）
- Manifest Tool 增加可选依赖声明（向后兼容）：
  ```yaml
  depends:
    - tool: node        # 依赖的工具名（与 registry 对齐）
      min_version: "22" # 可选：最低版本（semver 比较）
      install_path: npm # 可选：仅某安装路径需要（如 npm 安装需 Node，native 免）
    - tool: java
      min_version: "17"
    - platform: windows # 可选：限定平台
  ```
  或复用/扩展 `required_plugins`（保留二进制存在性语义 + 新增 version 约束字段）
- 实现：`internal/manifest/manifest.go` Tool 加 `Depends []Dependency`；`Dependency{Tool, MinVersion, Platforms []string, InstallPath string}`
- **核实结论落地**：Claude Code / Copilot CLI 的 npm 安装声明 `depends: node ≥22`（`install_path: npm`），native 安装路径不声明；OpenCode 的 npm 引导同理（Node 需求待最终核实）

### F5: 安装前依赖检查 + 提示（G4）
- 后端新增 `CheckToolDependencies(toolName string) []DependencyStatus`：
  - 逐项检查：平台匹配（os/arch）→ 工具存在（LookPath / xdev 已装）→ 版本满足（getVersion + CompareSemver）
  - 返回 `{tool, satisfied, currentVersion, requiredVersion, platform, reason}`
- 前端：
  - 工具详情抽屉与安装按钮旁显示依赖状态徽标（✅ 满足 / ⚠️ 缺失：需要 Node ≥ 20）
  - 不满足时安装按钮仍可点击（用户自行决定），但显示明确提示；或提供"先安装依赖"快捷按钮
- 复用 `detectPackageManager`/`getVersion` 探测模式

### F6: runInstaller 目标目录模板（G6）
- InstallArgs 支持 `{{install}}` 模板（替换为 installPath，配合 JetBrains `/D={{install}}`）

### F7: 插件注册泛化（G5）
- `register.go` 改为遍历完整 catalog 生成插件（或新增分类白名单 ide/agent/harness/language/build/mobile），消除硬编码清单

### F8: 安装后体验（轻量）
- 依据工具形态（F1b）提供差异化入口：
  - 含 `gui`：详情页显示「启动」按钮 → `LaunchTool(toolName)` 解析并拉起 `gui_binary`（exec，不隐藏窗口）
  - 仅 `cli`/`tui`：显示「复制命令」按钮（`code .` / `claude` / `opencode` 等，来自 shims），用户自行在终端运行
- 不实现：桌面快捷方式、卸载器注册、系统级集成（标注为未来工作）

## 四、验证

1. `go test ./internal/manifest/ ./internal/remote/ ./internal/installer/ ./internal/gui/ -short`
2. npm remote：`typescript.yaml` 或新 agent manifest 版本刷新成功
3. dmg：macOS（如可用）或单元测试 mock hdiutil
4. 依赖检查：node 未装时 `claude-code` 显示"需要 Node ≥ 20"；装上后显示满足
5. **形态**：vscode 显示 GUI+CLI 双徽标、完整性校验 `gui_binary`、详情页出现「启动」按钮；claude-code 仅 CLI/TUI 徽标、显示「复制命令」而非「启动」
6. 前端 `vite build` + vitest；`wails build`
7. 手动：安装 VS Code（zip）成功并可「启动」；JetBrains exe 静默安装到 install dir；无 gui 的 agent 只提供复制命令

## 五、范围边界

- **不做**：桌面快捷方式、开始菜单、卸载器注册、自动更新守护、系统服务
- **不做**：IDE 内部插件生态管理（VS Code extensions 等）
- **不做**：TUI 的嵌入运行（在 xdev 内嵌终端跑交互界面）——仅提供复制命令，用户在自有终端运行
- Agent 运行时的隔离执行（isolator 已有，未来可接，本次不接）
- 首批 manifest 数量：3 类各 1-2 个代表性工具起步，后续扩充

## 六、风险

| 风险 | 缓解 |
|------|------|
| npm registry 大版本列表体积 | 版本列表截断（只保留 stable + 近 N 个） |
| 静默安装参数因版本而异 | manifest 内 per-archive install_args，失败时错误信息提示 |
| **下载源变化**（VS Code mac 已转 dmg、JetBrains `ideaIC-*` 命名废弃、Windsurf 并入 Devin） | manifest URL 实施期逐个实测确认（含 HEAD 验证），spec 中的核实表为基线 |
| **静默参数未文档化**（Cursor/Zed Windows、Toolbox） | 实施期用 `winget`/官方脚本替代或实测参数；不确定时降级为"下载到目录+提示手动安装" |
| **Node 版本口径不一致**（Claude Code README 18 vs registry 22） | 以 registry `engines` 为准（≥22），manifest 按此声明 |
| IDE 安装后目录无 shim → 完整性检查 | 含 gui 时校验 gui_binary；无 shims 时非空目录即完整（现有逻辑已支持）；gui_binary 缺失但目录非空 → 提示修复 |
| `gui_binary` 路径随版本变化 | manifest per-version 声明；未命中时按"目录完整"降级处理并提示 |
| Windows 权限（Program Files 外安装） | 默认装到 xdev install dir，无需管理员 |
| GUI 启动需正确工作目录/环境 | LaunchTool 复用 Activate 注入的 env_vars 与 installPath 作为工作目录 |
| npm 安装的 agent 引导 stub 需 postinstall | manifest 标注 npm 安装需 Node；实施期验证 postinstall 在 xdev 安装流程中可完成（或改走 native 安装脚本） |

## 七、审批请求

请审批本 spec。获批后依次产出：
1. **plan**（实施阶段划分，含每个差距 G1-G6 的落点与顺序）
2. **task**（细化到可执行单元）
3. **checklist**（逐项验收标准）
每份文档单独获批后才进入下一阶段，全部获批后开始实施。


## 批准记录 (Approval History)

| 阶段 | 状态 | 批准者 | 日期 | 文档版本 | 批准范围 |
|---|---|---|---|---|---|
| Spec 批准 | Approved | `xinyan <zuijiuhjd@foxmail.com>` | 2026-08-08 | `1.0.0` | `2026-08-20-spec-023-approved-001` IDE/Agent/Harness 管理支持的目标、范围和验收 |
