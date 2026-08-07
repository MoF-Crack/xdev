# Spec 023 Plan: IDE / Agent / Harness 管理支持

> **状态**：已完成
> **前置**：Spec 023 已获批
> **日期**：2026-08-02
> **下一阶段**：本 plan 获批后 → task → checklist → 全部获批才实施

## 总原则

- **增量优先**：全部改动 additive（新字段/新函数/新 manifest），不重构现有路径
- **能力先行**：先做平台能力（npm remote / dmg / depends / 形态模型 / 注册泛化），再挂功能（依赖检查 / 启动），最后填内容（manifest）
- **向后兼容**：现有 39 个 manifest 不因新字段受影响（YAML 未声明的字段默认零值）
- **验证门**：每个 Phase 结束跑对应测试；E 阶段做端到端验证

## Phase 划分与依赖

```
A 平台能力（A1-A6，可并行部分）
  │
  ├─ B 依赖检查（依赖 A1/A6）
  ├─ C 启动与形态 UI（依赖 A6）
  └─ D 首批 manifest（依赖 A2/A3/A4/A6，部分依赖 B 的 depends）
      │
      └─ E 集成验证（依赖 B/C/D 全部）
```

## Phase A — 平台能力（G1-G6 落点）

### A1（G3/F4）：依赖声明 `depends`
- `internal/manifest/manifest.go`：
  - `Tool` 加 `Depends []Dependency`（`yaml:"depends"`）
  - `type Dependency struct { Tool string; MinVersion string; Platforms []string; InstallPath string }`（omitempty）
- 校验：tool 名非空、min_version 可解析（ParseSemverParts）、platforms 值合法（windows/darwin/linux/any）
- 测试：解析合法/非法 depends、向后兼容（现有 manifest 无 depends 正常加载）
- 验证：`go test ./internal/manifest/ -count=1 -short`

### A2（G1/F2）：npm 远程类型
- `internal/manifest/manifest.go`：`RemoteSource` 加 `Package string`（yaml:"package"）
- `internal/remote/remote.go`：`DiscoverVersions` 增加 `case "npm"`：
  - GET `https://registry.npmjs.org/<package>`
  - 解析 `dist-tags.latest` + `versions`（keys），按 semver 降序截断（stable + 近 20 个）
  - 失败降级：registry 不可达 → 返回错误（沿用现有错误路径）
- `typescript.yaml` 的 `remote: type: npm` 作为回归用例（修复现有失效）
- 测试：httptest mock registry（versions 对象 + dist-tags）、非 npm 类型不受影响
- 验证：`go test ./internal/remote/ ./internal/manifest/ -count=1 -short`

### A3（G2/F3）：dmg 解包
- `internal/extractor/extractor.go`：`case "dmg"`：
  - macOS：`hdiutil attach -nobrowse -readonly` → 复制 .app → detach（exec 数组，HideWindow 不适用）
  - 非 macOS：返回明确错误"dmg 仅支持 macOS"
- 测试：macOS 用 mock hdiutil（test hook 同 zst 模式）；非 macOS 断言错误
- 验证：`go test ./internal/extractor/ -count=1 -short`

### A4（G6/F6）：`{{install}}` 模板
- `internal/installer/installer.go` `runInstaller`：InstallArgs 模板替换扩展：
  - `{{version}}`、`{{tool}}`（已有）→ 新增 `{{install}}`（→ installPath）
- 测试：模板替换测试补 `{{install}}` 用例（mock runner 断言）
- 验证：`go test ./internal/installer/ -count=1 -short -run "RunInstaller|Template"`

### A5（G5/F7）：插件注册泛化
- `internal/cli/register.go:48-66`：`genericTools` 硬编码清单 → 改为遍历完整 catalog（`cat.Tools()`）生成 Generic 插件
  - 保留现有分类白名单语义（language/build/mobile/system/ide/agent/harness），或直接全部注册（新分类工具无副作用）
  - 移除清单后确认现有工具注册行为不变（register 测试）
- 测试：新 catalog 条目自动注册（临时 manifest + 断言 registry.Get 命中）；现有工具全部仍在
- 验证：`go test ./internal/cli/ -count=1 -short`

### A6（F1b）：形态模型 `form_factor` / `gui_binary`
- `internal/manifest/manifest.go`：
  - `Tool` 加 `FormFactor []string`（yaml:"form_factor,omitempty"）+ `GUIBinary string`（yaml:"gui_binary,omitempty"）
  - 校验：form_factor 值 ∈ {gui, cli, tui}；含 gui 时 gui_binary 非空（警告非错误）
  - `func (t *Tool) EffectiveFormFactor() []string`：空 → 按 len(Shims)>0 推断 `[cli]`，否则 `[gui]`（有 gui_binary）或 `[]`
- `internal/installer/integrity.go`：完整性检查扩展
  - 含 gui 且 gui_binary 声明 → 校验 `<install>/<gui_binary>` 存在（加入 binaryCandidates）
  - 现有"至少一个 shim"规则不变；gui 通过即完整
- `internal/gui/app_tool_query.go`：`ToolInfo` 加 `FormFactor []string` + `GUIBinary string`（json 输出给前端）
- 测试：推断规则（无 form_factor 向后兼容）、gui_binary 完整性命中/缺失、ToolInfo JSON 字段
- 验证：`go test ./internal/manifest/ ./internal/installer/ ./internal/gui/ -count=1 -short`

## Phase B — 依赖检查（F5，依赖 A1/A6）

### B1：后端 `CheckToolDependencies`
- `internal/gui/app_dependencies.go`（新）：
  - `type DependencyStatus struct { Tool, RequiredVersion, CurrentVersion string; Platform, InstallPath string; Satisfied bool; Reason string }`
  - `func (a *App) CheckToolDependencies(toolName string) []DependencyStatus`
  - 逻辑：读 manifest `Depends` → 逐项：
    - platform 声明不匹配当前 os → Satisfied=false（Reason: 平台不支持）
    - 工具存在性：xdev 已装（currentVersion）或 LookPath（跳过 shim 目录）
    - 版本满足：getVersion + util.CompareSemver（≥ MinVersion）
  - 依赖工具本身也是 xdev 管理工具时优先用 xdev 已装版本
- 测试：mock 环境（node 在 PATH / xdev 已装 / 缺失）三类场景；platform 不匹配
- 验证：`go test ./internal/gui/ -count=1 -short -run "Dependency|CheckToolDependencies"`

### B2：前端依赖提示
- `ToolDetailDrawer.vue`：安装区显示依赖状态徽标（✅ 满足 / ⚠️ 缺失：需要 Node ≥ 22）
- `ToolCard.vue` / `ToolTable.vue`：卡片可选显示依赖警告点（弱化，详情页为主）
- `AppContent.vue`：打开详情时调 `CheckToolDependencies(tool.name)` 缓存到 ref；安装流程不变
- i18n：`detail.deps_satisfied` / `detail.deps_missing` / `detail.deps_platform` / `detail.deps_install_hint`（zh/en）
- 验证：`npx vite build` + vitest（ToolDetailDrawer 相关）

## Phase C — 启动与形态 UI（F8，依赖 A6）

### C1：后端 `LaunchTool` / `CopyCommand`
- `internal/gui/app_launch.go`（新）：
  - `func (a *App) LaunchTool(toolName string) error`：解析 current/<tool> → gui_binary → exec（不隐藏窗口，Dir=installPath，Env=Activate env vars）
  - `func (a *App) ToolCommand(toolName string) string`：返回 CLI 命令（shims[0]，供"复制命令"）
- 测试：mock 环境（gui_binary 存在/缺失错误）；ToolCommand 返回首 shim
- 验证：`go test ./internal/gui/ -count=1 -short -run "Launch|ToolCommand"`

### C2：前端形态徽标与启动按钮
- `ToolCard.vue` / `ToolTable.vue`：形态徽标（GUI / CLI / TUI，来自 ToolInfo.FormFactor）
- `ToolDetailDrawer.vue`：
  - 含 gui → 「启动」按钮（调 LaunchTool）
  - 仅 cli/tui → 「复制命令」按钮（navigator.clipboard 复制 ToolCommand）
- `AppContent.vue`：`launchTool` / `copyToolCommand` 处理器 + 成功/失败提示
- i18n：`detail.launch` / `detail.copy_command` / `detail.copied` / `form.gui/cli/tui`（zh/en）
- 验证：`npx vite build` + vitest

## Phase D — 首批 manifest（F1，依赖 A2/A3/A4/A6）

每项含：写 manifest（含 form_factor/gui_binary/depends/install_args）→ 实测 URL（HEAD/下载验 SHA）→ 同步运行时 → 冒烟（如可离线验证则用 xdev CLI 装到临时目录）

- D1 `vscode`（zip；form [gui,cli]；gui_binary bin/code.exe；Win zip + mac dmg + linux tar.gz）
- D2 `jetbrains-idea-community`（exe NSIS；form [gui,cli]；install_args `/S /D={{install}}`；gui_binary bin/idea64.exe；depends 无——自带 JBR）
- D3 `cursor` + `zed`（zip/dmg/tar.gz；form [gui,cli]；静默参数未文档化 → zip/官方脚本优先）
- D4 `claude-code`（native install 优先，npm 备选；form [cli,tui]；depends: node≥22 @install_path=npm）
- D5 `opencode`（CLI native + Desktop App 分开记录；form [gui,cli,tui]；gui_binary 桌面 app 路径；npm 免 Node）
- D6 `codex`（CLI native + `codex app` 说明；form [gui,cli,tui]——gui 由 ChatGPT 桌面应用提供，xdev 管理 CLI/TUI；depends: node≥16 @install_path=npm）
- D7 `copilot-cli`（npm/native；form [cli,tui]；depends: node≥22 @install_path=npm）
- 验证：`go test ./internal/manifest/ -count=1 -short` + 各 manifest URL HEAD 200 + SHA 回填

## Phase E — 集成验证

- E1：全量 `go test ./... -short` + `wails build`
- E2：手动场景
  - 安装 vscode → 详情页显示 GUI+CLI 徽标 → 「启动」拉起
  - claude-code（native）→ 显示 CLI/TUI 徽标 + 复制命令；npm 安装路径显示 Node≥22 依赖提示（未装 node 时）
  - codex → 显示三形态徽标；GUI 说明文案
  - 依赖检查：node 缺失/满足两种状态切换
- E3：更新 spec/plan 状态为完成；输出 checklist 勾选记录

## 关键风险与缓解

| 风险 | 缓解 |
|------|------|
| npm registry 解析与 typescript 现状冲突 | A2 以 httptest 覆盖；typescript 作为回归用例 |
| JetBrains /S /D= 静默参数随版本变化 | A4 模板 + manifest per-archive install_args；实测后回填 |
| dmg 仅在 macOS 可测 | A3 用 mock hdiutil（同 zst test hook 模式） |
| Codex GUI 由 ChatGPT 桌面应用提供 | D6 明确 xdev 只管理 CLI/TUI；gui 形态标注为外部应用 |
| 首批 manifest 体积（VS Code ~100MB） | 每项独立实测 SHA（沿用 Spec 022 回填流程） |

## 里程碑

| Phase | 交付 | 验证门 |
|-------|------|--------|
| A | 平台能力 6 项 | go test（manifest/remote/extractor/installer/cli/gui） |
| B | 依赖检查 | go test + vite build |
| C | 启动与形态 UI | go test + vite build |
| D | 首批 manifest 8 个 | manifest 测试 + URL/SHA 实测 |
| E | 集成验证 | 全量测试 + 手动场景 + 文档 |

---

**审批请求**：请审批本 plan。获批后产出 task（细化到可执行单元），task 获批后产出 checklist（逐项验收标准），全部获批后才开始实施。


## 批准记录 (Approval History)

> **批准ID**: `2026-08-08-plan-023-001`
