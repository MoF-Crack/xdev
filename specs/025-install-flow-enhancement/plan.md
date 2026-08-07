# Spec 025 Plan: 安装流程增强

> **状态**：待批准
> **前置**：Spec 025 已获批
> **日期**：2026-08-04
> **下一阶段**：本 plan 获批后 → task → checklist → loop → 全部获批才实施

## 总原则

- **增量优先**：pmDef 扩展 + Archive.Form 新字段，不重构现有安装管线
- **能力先行**：先做后端能力（script install + form filter），再挂前端 UI
- **向后兼容**：现有 50 manifest 无 Form 字段 → 缺省空 = 所有形态共用；现有 pmDef 的 brew/scoop/choco installCommand="" → 保持现有"访问网站"行为（升级后才有自动安装）
- **验证门**：每个 Phase 结束跑对应测试；C 阶段做端到端验证

## Phase 划分与依赖

```
A 包管理器 script install（A1 pmDef 扩展 + A2 InstallPackageManager script 路径 + A3 前端安装按钮）
   │  （A1→A2 串行；A3 依赖 A2 API 定型）
   │
├─ B 形态区分下载（B1 Archive.Form + B2 HasFormSpecificArchives + B3 InstallToolWithForm + B4 前端选择器 + B5 OpenCode manifest 标注）
   │  （B1→B2→B3 串行；B4 依赖 B3 API；B5 依赖 B1 字段）
   │
└─ C 集成验证（依赖 A+B 全部）
```

A 和 B 文件不重叠（A 在 `app_local_detect.go`/前端包管理器组件；B 在 `manifest.go`/`installer.go`/前端安装对话框），可并行推进。

## Phase A — 包管理器 script install

### A1（F1b）：pmDef 扩展

**涉及**：`internal/gui/app_local_detect.go`

- `pmDef` struct 新增字段：`installCommand string` / `requiresAdmin bool` / `executionEnv string` / `platform string`
- `GetPackageManagers()` 的 pms 列表更新：
  - scoop：`installCommand="irm get.scoop.sh | iex"`, `requiresAdmin=false`, `executionEnv="powershell"`, `platform="windows"`
  - brew：`installCommand='/bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"'`, `requiresAdmin=false`, `executionEnv="bash"`, `platform="any"`
  - choco：`installCommand="Set-ExecutionPolicy Bypass -Scope Process -Force; iex ((New-Object System.Net.WebClient).DownloadString('https://community.chocolatey.org/install.ps1'))"`, `requiresAdmin=true`, `executionEnv="powershell"`, `platform="windows"`
- `detectPackageManager`：`canInstall = pm.toolName != "" || pm.installCommand != ""`（扩展可安装判定）
- `PackageManagerInfo` 新增 `RequiresAdmin bool` json 字段（前端用于按钮文案）
- **测试**：pmDef 扩展后现有包管理器检测无回归；canInstall 对 brew/scoop/choco 为 true
- **验收**：`go test ./internal/gui/ -short -count=1 -run "PackageManager"`

### A2（F1c）：InstallPackageManager script 路径

**涉及**：`internal/gui/app_local_detect.go`（`InstallPackageManager` 函数）

- `InstallPackageManager(toolName)` 扩展：
  - 查 pmDef，若 `installCommand != ""` → 走 script install 路径
  - 平台检查：`platform != "any"` 且 `runtime.GOOS != platform` → 返回明确错误
  - 管理员检查（Windows）：`requiresAdmin=true` 且非管理员 → 返回错误"Chocolatey 需要管理员权限，请以管理员重启 xdev 或手动执行：<命令>"
  - 执行：
    - powershell：`exec.Command("powershell", "-NoProfile", "-ExecutionPolicy", "Bypass", "-Command", installCommand)`
    - bash：`exec.Command("bash", "-c", installCommand)`（Homebrew 设 `NONINTERACTIVE=1` 环境变量跳过交互确认）
  - 后台执行 + 捕获 stdout/stderr 到 GUI log
  - `cmd.Wait()` 防僵尸（沿 Spec 023 LaunchTool 模式）
  - 成功后重新检测 `detectPackageManager` 确认
- **测试**：mock exec（注入 exec func）验证命令构造正确；平台不匹配返回错误；管理员检查返回错误
- **验收**：`go test ./internal/gui/ -short -count=1 -run "InstallPackageManager"`

### A3（F1d）：前端包管理器安装按钮

**涉及**：`frontend/src/components/ToolTable.vue` 或包管理器列表组件、`AppContent.vue`、i18n

- 包管理器列表项：`CanInstall=true` 时显示「安装」按钮（已有 npm/pip 等的安装按钮模式）
- `RequiresAdmin=true` 时按钮文案「以管理员安装」+ tooltip
- 点击 → 确认对话框（显示完整安装命令 + 安全提示文案）→ 确认 → 调 `InstallPackageManager`
- i18n：`pm.install_confirm_title` / `pm.install_confirm_content` / `pm.install_admin_required` / `pm.install_command_label`（zh/en）
- **验收**：`npx vite build` + 相关 vitest

## Phase B — 形态区分下载

### B1（F2a）：Archive.Form 字段

**涉及**：`internal/manifest/manifest.go`

- `Archive` struct 新增 `Form string`（yaml:"form,omitempty"）
- 校验：`Form` 值 ∈ {gui, cli, tui, ""}（非法值仅警告不报错，沿 ValidateFormFactor 宽松模式）
- **测试**：解析带 Form 的 yaml；缺省空向后兼容
- **验收**：`go test ./internal/manifest/ -short -count=1`

### B2（F2b）：HasFormSpecificArchives helper

**涉及**：`internal/manifest/manifest.go`

- `func (t *Tool) HasFormSpecificArchives() bool`：遍历所有 version 的所有 archive，有 `Form != ""` 返回 true
- `ToolInfo` 新增 `HasFormSpecificArchives bool` json 字段（`app_tool_query.go` 的 `pluginToToolInfo` 填充）
- **测试**：有 Form 标注的工具返回 true；无标注的返回 false
- **验收**：`go test ./internal/manifest/ ./internal/gui/ -short -count=1`

### B3（F2c）：InstallToolWithForm API

**涉及**：`internal/gui/app_install.go`、`internal/installer/installer.go`（`ArchiveForCurrent`）

- `ArchiveForCurrent` 新增 `formFilter string` 参数：
  - `formFilter=""`：现有行为
  - `formFilter!=" "`：优先匹配 `Form==formFilter`，降级到 `Form==""`
- `InstallTool` 新增可选 formFilter（或新增 `InstallToolWithForm(toolName, version, formFilter)` 方法）
- `InstallWithProgress` 传递 formFilter 到 archive 选择
- **测试**：formFilter 筛选正确；降级到 Form=="" 兼容
- **验收**：`go test ./internal/installer/ ./internal/gui/ -short -count=1 -run "Form|InstallToolWithForm"`

### B4（F2d）：前端形态选择器

**涉及**：`frontend/src/components/InstallProgressDialog.vue` 或新增 FormSelectorDialog、`AppContent.vue`、`useInstall.js`、i18n

- `GetToolDetail` 返回 `hasFormSpecificArchives` → 前端缓存
- 点「安装」时检查 `hasFormSpecificArchives`：
  - true → 弹出形态选择器（复选框来自 formFactor：GUI/CLI/TUI）→ 用户选 → 调 `InstallToolWithForm`
  - false → 现有行为直接安装
- i18n：`install.form_selector_title` / `install.form_selector_hint` / `install.form.gui/cli/tui`（zh/en）
- **验收**：`npx vite build` + vitest

### B5（F3）：OpenCode manifest 标注 Form

**涉及**：`manifests/opencode.yaml`

- Desktop App archive：`form: gui`
- CLI archive：`form: cli`
- 其他工具不动
- **验收**：`HasFormSpecificArchives()` 对 opencode 返回 true；对 vscode 返回 false

## Phase C — 集成验证

### C1：全量回归 + 构建

- `go test ./... -short -count=1` 全绿
- `wails build` 产出 xdev-gui.exe
- 前端 `npx vitest run` + `npx vite build`
- e2e 新用例：包管理器安装确认对话框 + 形态选择器

### C2：手动场景

- Scoop 安装（Windows 非管理员）：点「安装」→ 确认 → 后台执行 → 成功
- Chocolatey 安装（Windows 非管理员）：点「以管理员安装」→ 提示需管理员
- OpenCode 安装：点「安装」→ 形态选择器 → 选 CLI → 只下载 CLI archive
- VS Code 安装：点「安装」→ 直接安装（无选择器）

### C3：文档收尾

- spec/plan/task/checklist/loop 标记完成
- HANDOFF.md 更新

## 关键风险与缓解

| 风险 | 缓解 |
|------|------|
| 执行远程脚本安全 | 确认对话框显示完整命令 + 安全提示 |
| Chocolatey 需管理员 | 检测 + 提示，不自动提权 |
| PowerShell ExecutionPolicy | 命令带 `-ExecutionPolicy Bypass -Scope Process` |
| Homebrew 交互式确认 | 设 `NONINTERACTIVE=1` 环境变量 |
| Archive.Form 向后兼容 | 缺省空 = 所有形态共用 |
| 形态选择器 UX 复杂 | 只对多包工具显示 |

## 里程碑

| Phase | 交付 | 验证门 |
|-------|------|--------|
| A | 包管理器 script install | go test（gui）+ vite build |
| B | 形态区分下载 | go test（manifest/installer/gui）+ vite build |
| C | 集成验证 | 全量 go test + wails build + e2e + 手动场景 |

---

**审批请求**：请审批本 plan。获批后产出 task，task 获批后产出 checklist，checklist 获批后产出 loop，全部获批后才开始实施。

## 批准记录 (Approval History)

> **批准ID**: `2026-08-08-plan-025-001`
