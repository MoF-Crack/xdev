﻿# Spec 023 Task 清单

> **状态**：已完成
> **前置**：Spec 023 + Plan 已获批
> **日期**：2026-08-02
> **下一阶段**：本 task 获批后 → checklist → 全部获批才实施

## 使用说明

- 每项 task 标注：预期产出（Deliverable）、验收标准（Acceptance）、依赖（Depends）
- 实施顺序：按编号；同一 Phase 内无依赖项可并行（由实施方决定）
- 全部 task 获批后才可开始实施

---

## Phase A — 平台能力

### T-A1 `depends` 依赖声明（G3/F4）
- **内容**：manifest 支持可选依赖声明
- **涉及**：`internal/manifest/manifest.go`（Tool.Depends + Dependency 结构）、`internal/manifest/manifest_test.go`
- **预期产出**：
  - `Tool.Depends []Dependency`（yaml:"depends,omitempty"）
  - `Dependency{Tool string; MinVersion string; Platforms []string; InstallPath string}`（omitempty）
  - 加载校验：tool 非空、min_version 可 ParseSemverParts、platforms ∈ {windows,darwin,linux,any}（非法值警告不报错）
  - 新增测试：合法 depends 解析、非法值容错、现有 manifest 无 depends 正常加载
- **验收**：`go test ./internal/manifest/ -count=1 -short` 通过；现有 39 manifest 加载无回归

### T-A2 npm 远程类型（G1/F2）
- **内容**：RemoteSource 支持 npm registry 版本发现
- **涉及**：`internal/manifest/manifest.go`（RemoteSource.Package）、`internal/remote/remote.go`（DiscoverVersions npm case）、`internal/remote/remote_test.go`
- **预期产出**：
  - `RemoteSource.Package string`（yaml:"package"）
  - npm 解析：GET `https://registry.npmjs.org/<pkg>` → `dist-tags.latest` + `versions` keys → semver 降序截断 20 个
  - typescript.yaml 的 `remote: type: npm` 作为回归用例（修复现有失效）
  - 新增测试：httptest mock registry（versions 对象 + dist-tags + latest）、错误路径、非 npm 类型不受影响
- **验收**：`go test ./internal/remote/ ./internal/manifest/ -count=1 -short` 通过；typescript 版本刷新走 npm 成功

### T-A3 dmg 解包（G2/F3）
- **内容**：extractor 支持 macOS dmg
- **涉及**：`internal/extractor/extractor.go`（case "dmg"）、`internal/extractor/extractor_test.go`
- **预期产出**：
  - `case "dmg"`：macOS 用 `hdiutil attach -nobrowse -readonly` → 复制 .app → `hdiutil detach`；非 macOS 返回"dmg 仅支持 macOS"
  - 测试 hook（同 zst 模式）注入 mock hdiutil；非 macOS 断言明确错误
- **验收**：`go test ./internal/extractor/ -count=1 -short` 通过

### T-A4 `{{install}}` 模板（G6/F6）
- **内容**：runInstaller InstallArgs 支持目标目录模板
- **涉及**：`internal/installer/installer.go`（runInstaller 模板替换）、`internal/installer/run_installer_sidecar_test.go`
- **预期产出**：`{{install}}` → installPath 替换（现有 {{version}}/{{tool}} 保留）
- **验收**：新增模板测试（mock runner 断言 args 含真实 installPath）；`go test ./internal/installer/ -count=1 -short -run "RunInstaller|Template"` 通过

### T-A5 插件注册泛化（G5/F7）
- **内容**：register.go 消除硬编码清单
- **涉及**：`internal/cli/register.go`（genericTools → catalog 遍历）、`internal/cli/register_test.go`（如存在）
- **预期产出**：
  - 从 `cat.Tools()` 全量生成 Generic 插件（保留分类过滤或全部注册，二选一实施期定）
  - 移除 hardcoded 清单后现有工具注册行为不变
  - 新增测试：临时 catalog 条目自动注册
- **验收**：`go test ./internal/cli/ -count=1 -short` 通过；现有工具全部仍在

### T-A6 形态模型（F1b）
- **内容**：form_factor / gui_binary 字段 + 完整性 + ToolInfo 输出
- **涉及**：`internal/manifest/manifest.go`（FormFactor/GUIBinary + EffectiveFormFactor）、`internal/installer/integrity.go`（gui_binary 候选）、`internal/gui/app_tool_query.go`（ToolInfo 输出）、对应测试
- **预期产出**：
  - `Tool.FormFactor []string`（∈ gui/cli/tui）+ `Tool.GUIBinary string`；`EffectiveFormFactor()` 缺省推断（shims→[cli]）
  - integrity：含 gui 且 gui_binary 声明 → `<install>/<gui_binary>` 校验
  - `ToolInfo.FormFactor []string` + `GUIBinary string`（json）
  - 测试：推断规则、gui_binary 命中/缺失、JSON 字段
- **验收**：`go test ./internal/manifest/ ./internal/installer/ ./internal/gui/ -count=1 -short` 通过；现有工具（无 form_factor）行为不变

---

## Phase B — 依赖检查

### T-B1 后端 CheckToolDependencies（F5）
- **内容**：依赖状态查询
- **涉及**：`internal/gui/app_dependencies.go`（新）、`internal/gui/app_dependencies_test.go`（新）
- **预期产出**：
  - `DependencyStatus{Tool, RequiredVersion, CurrentVersion, Platform, InstallPath string; Satisfied bool; Reason string}`
  - `CheckToolDependencies(toolName string) []DependencyStatus`：平台匹配 → 存在性（xdev 已装优先，否则 LookPath 跳过 shim）→ 版本（CompareSemver ≥ MinVersion）
  - 测试：node 缺失/在 PATH/xdev 已装、platform 不匹配、未知工具
- **验收**：`go test ./internal/gui/ -count=1 -short -run "Dependency|CheckToolDependencies"` 通过

### T-B2 前端依赖提示（F5）
- **内容**：详情页依赖徽标
- **涉及**：`ToolDetailDrawer.vue`、`AppContent.vue`、`i18n/{en,zh}.ts`
- **预期产出**：
  - 打开详情时调 `CheckToolDependencies`，安装区显示 ✅/⚠️ 徽标（"需要 Node ≥ 22"）
  - i18n：`detail.deps_satisfied` / `detail.deps_missing` / `detail.deps_platform` / `detail.deps_install_hint`
- **验收**：`npx vite build` + 相关 vitest 通过

---

## Phase C — 启动与形态 UI

### T-C1 后端 LaunchTool / ToolCommand（F8）
- **内容**：启动与命令查询
- **涉及**：`internal/gui/app_launch.go`（新）、`internal/gui/app_launch_test.go`（新）
- **预期产出**：
  - `LaunchTool(toolName) error`：current/<tool> → gui_binary → exec（Dir=installPath、Env=Activate 注入）
  - `ToolCommand(toolName) string`：返回 shims[0]（复制命令用）
  - 测试：gui_binary 缺失报错、ToolCommand 返回首 shim
- **验收**：`go test ./internal/gui/ -count=1 -short -run "Launch|ToolCommand"` 通过

### T-C2 形态徽标与启动/复制按钮（F8）
- **内容**：前端形态展示与操作
- **涉及**：`ToolCard.vue`、`ToolTable.vue`、`ToolDetailDrawer.vue`、`AppContent.vue`、`i18n/{en,zh}.ts`
- **预期产出**：
  - 卡片/表格形态徽标（GUI/CLI/TUI）
  - 详情页：含 gui → 「启动」；仅 cli/tui → 「复制命令」（clipboard）
  - i18n：`detail.launch` / `detail.copy_command` / `detail.copied` / `form.gui/cli/tui`
- **验收**：`npx vite build` + 相关 vitest 通过

---

## Phase D — 首批 manifest

每项含：manifest 编写（form_factor/gui_binary/depends/install_args）→ URL 实测（HEAD/下载算 SHA）→ 同步运行时 → 冒烟。**验收统一**：`go test ./internal/manifest/ -count=1 -short` + URL HEAD 200 + SHA 回填。

### T-D1 vscode
- **预期产出**：`manifests/vscode.yaml`——Win zip（`/sha/download?build=stable&os=win32-x64-archive`）、mac dmg、linux tar.gz；form [gui,cli]；gui_binary bin/code.exe；latest + 1 旧版

### T-D2 jetbrains-idea-community
- **预期产出**：`manifests/jetbrains-idea-community.yaml`——Win exe（`/S /CONFIG=... /D={{install}}`，install_kind executable）、mac dmg、linux tar.gz；form [gui,cli]；gui_binary bin/idea64.exe（Win）/ bin/idea.sh（Linux）；自带 JBR 无需 depends

### T-D3 cursor + zed
- **预期产出**：`manifests/cursor.yaml`（官方 JSON API URL；form [gui,cli]；静默参数未文档化 → 用安装器或 zip 降级说明）、`manifests/zed.yaml`（GitHub releases zip/tar.gz/dmg；form [gui,cli]；gui_binary 核实）

### T-D4 claude-code
- **预期产出**：`manifests/claude-code.yaml`——native 安装优先（install.sh/ps1），npm 备选（`@anthropic-ai/claude-code`）；form [cli,tui]；depends: node≥22 @install_path=npm

### T-D5 opencode
- **预期产出**：`manifests/opencode.yaml`——CLI（native/npm `opencode-ai`）+ Desktop App 路径；form [gui,cli,tui]；gui_binary 桌面 app 路径（实施期核实安装布局）；npm 免 Node

### T-D6 codex
- **预期产出**：`manifests/codex.yaml`——CLI native（install.sh/ps1 / winget）+ npm `@openai/codex`；form [gui,cli,tui]（gui 由 ChatGPT 桌面应用提供，manifest 注释说明 xdev 仅管理 CLI/TUI）；depends: node≥16 @install_path=npm

### T-D7 copilot-cli
- **预期产出**：`manifests/copilot-cli.yaml`——npm `@github/copilot` + native（gh.io/copilot-install）；form [cli,tui]；depends: node≥22 @install_path=npm

---

## Phase E — 集成验证

### T-E1 全量验证与手动场景
- **内容**：回归 + 端到端
- **预期产出**：
  - 全量 `go test ./... -short` 通过（26+ 包）
  - `wails build` 产出新 xdev-gui.exe
  - 手动场景记录：vscode 安装+启动、claude-code 依赖提示（node 缺/满足）、codex 三形态徽标、复制命令
  - 更新 spec/plan/task/checklist 状态为完成
- **验收**：无 FAIL；手动场景截图/日志留存

---

## 里程碑产出汇总

| Task | 预期产出 |
|------|---------|
| T-A1 | depends 字段+校验+测试 |
| T-A2 | npm 版本发现+typescript 回归修复 |
| T-A3 | dmg 解包（macOS/mock） |
| T-A4 | {{install}} 模板 |
| T-A5 | 注册泛化+测试 |
| T-A6 | 形态模型（manifest+integrity+ToolInfo） |
| T-B1 | CheckToolDependencies API+测试 |
| T-B2 | 依赖徽标 UI |
| T-C1 | LaunchTool/ToolCommand API+测试 |
| T-C2 | 形态徽标+启动/复制按钮 |
| T-D1..D7 | 8 个 manifest（实测 URL/SHA，同步运行时） |
| T-E1 | 全量回归+构建+手动场景 |

---

**审批请求**：请审批本 task 清单。获批后产出 **checklist**（逐项验收标准，勾选执行记录），全部获批后才开始实施。
