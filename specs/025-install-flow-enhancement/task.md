# Spec 025 Task 清单

> **状态**：待批准
> **前置**：Spec 025 + Plan 已获批
> **日期**：2026-08-04
> **下一阶段**：本 task 获批后 → checklist → loop → 全部获批才实施

## 使用说明

- 每项 task 标注：预期产出（Deliverable）、验收标准（Acceptance）、依赖（Depends）
- 实施顺序：按 Phase；同一 Phase 内无依赖项可并行
- 全部 task 获批后才可开始实施

---

## Phase A — 包管理器 script install

### T-A1 pmDef 扩展（F1b）
- **内容**：pmDef 新增 installCommand/requiresAdmin/executionEnv/platform 字段 + brew/scoop/choco 填值
- **涉及**：`internal/gui/app_local_detect.go`、`internal/gui/app.go`（PackageManagerInfo 加 RequiresAdmin）
- **预期产出**：
  - pmDef 新增 4 字段；scoop/brew/choco 填官方安装命令
  - `canInstall = pm.toolName != "" || pm.installCommand != ""`
  - PackageManagerInfo 新增 `RequiresAdmin bool` json 字段
  - 测试：brew/scoop/choco canInstall=true；现有包管理器无回归
- **验收**：`go test ./internal/gui/ -short -count=1 -run "PackageManager"` 通过
- **依赖**：无

### T-A2 InstallPackageManager script 路径（F1c）
- **内容**：InstallPackageManager 支持 installCommand 后台执行
- **涉及**：`internal/gui/app_local_detect.go`（InstallPackageManager 函数）
- **预期产出**：
  - installCommand != "" → script install 路径
  - 平台检查（platform != "any" && GOOS != platform → 错误）
  - 管理员检查（Windows requiresAdmin=true && 非管理员 → 错误带命令文案）
  - powershell：`exec.Command("powershell","-NoProfile","-ExecutionPolicy","Bypass","-Command",cmd)`
  - bash：`exec.Command("bash","-c",cmd)` + NONINTERACTIVE=1 环境变量（brew）
  - stdout/stderr 捕获到 GUI log；cmd.Wait() 防僵尸
  - 成功后重新检测确认
  - 测试：mock exec 验证命令构造；平台/管理员检查返回错误
- **验收**：`go test ./internal/gui/ -short -count=1 -run "InstallPackageManager"` 通过
- **依赖**：T-A1

### T-A3 前端包管理器安装按钮（F1d）
- **内容**：包管理器列表显示安装按钮 + 确认对话框
- **涉及**：包管理器列表组件、`AppContent.vue`、i18n
- **预期产出**：
  - CanInstall=true 显示「安装」按钮；RequiresAdmin=true 文案「以管理员安装」+ tooltip
  - 点击 → 确认对话框（显示完整命令 + 安全提示）→ 调 InstallPackageManager
  - i18n：pm.install_confirm_title/content/admin_required/command_label（zh/en）
  - vitest：确认对话框渲染 + 安装调用
- **验收**：`npx vite build` + vitest 通过
- **依赖**：T-A2（API 定型）

---

## Phase B — 形态区分下载

### T-B1 Archive.Form 字段（F2a）
- **内容**：Archive 新增 Form 字段 + 校验
- **涉及**：`internal/manifest/manifest.go`
- **预期产出**：
  - `Archive.Form string`（yaml:"form,omitempty"）
  - 校验 ∈ {gui,cli,tui,""}（宽松模式）
  - 测试：解析带 Form 的 yaml；缺省空向后兼容
- **验收**：`go test ./internal/manifest/ -short -count=1` 通过
- **依赖**：无

### T-B2 HasFormSpecificArchives helper（F2b）
- **内容**：Tool.HasFormSpecificArchives() + ToolInfo 输出
- **涉及**：`internal/manifest/manifest.go`、`internal/gui/app_tool_query.go`
- **预期产出**：
  - `func (t *Tool) HasFormSpecificArchives() bool`
  - ToolInfo 新增 `HasFormSpecificArchives bool` json
  - 测试：有 Form 标注 true；无标注 false
- **验收**：`go test ./internal/manifest/ ./internal/gui/ -short -count=1` 通过
- **依赖**：T-B1

### T-B3 InstallToolWithForm API（F2c）
- **内容**：ArchiveForCurrent 加 formFilter + InstallToolWithForm
- **涉及**：`internal/installer/installer.go`、`internal/gui/app_install.go`
- **预期产出**：
  - ArchiveForCurrent 新增 formFilter 参数（优先 Form==formFilter，降级 Form==""）
  - InstallToolWithForm(toolName, version, formFilter) 方法
  - InstallWithProgress 传递 formFilter
  - 测试：formFilter 筛选正确；降级兼容
- **验收**：`go test ./internal/installer/ ./internal/gui/ -short -count=1 -run "Form|InstallToolWithForm"` 通过
- **依赖**：T-B1

### T-B4 前端形态选择器（F2d）
- **内容**：安装前形态选择（仅多包工具）
- **涉及**：`InstallProgressDialog.vue` 或新增 FormSelectorDialog、`AppContent.vue`、`useInstall.js`、i18n
- **预期产出**：
  - hasFormSpecificArchives=true → 弹形态选择器（GUI/CLI/TUI 复选框）→ 调 InstallToolWithForm
  - hasFormSpecificArchives=false → 现有行为
  - i18n：install.form_selector_title/hint + form.gui/cli/tui（zh/en）
  - vitest：选择器渲染 + 调用
- **验收**：`npx vite build` + vitest 通过
- **依赖**：T-B2 + T-B3

### T-B5 OpenCode manifest 标注 Form（F3）
- **内容**：opencode.yaml archives 标 form 字段
- **涉及**：`manifests/opencode.yaml`
- **预期产出**：
  - Desktop App archive：form: gui
  - CLI archive：form: cli
  - 同步运行时副本
- **验收**：HasFormSpecificArchives(opencode)=true；HasFormSpecificArchives(vscode)=false
- **依赖**：T-B1

---

## Phase C — 集成验证

### T-C1 全量回归与构建
- **内容**：回归 + 构建
- **预期产出**：
  - `go test ./... -short -count=1` 全绿
  - `wails build` 产出 xdev-gui.exe
  - 前端 vitest + vite build
  - e2e 新用例：包管理器安装确认 + 形态选择器
- **验收**：无 FAIL

### T-C2 手动场景
- **内容**：端到端体验
- **预期产出**：
  - Scoop 安装（Windows 非管理员）→ 确认 → 后台执行 → 成功
  - Chocolatey（非管理员）→ 提示需管理员
  - OpenCode 安装 → 形态选择器 → 选 CLI → 只下载 CLI archive
  - VS Code 安装 → 直接安装（无选择器）
  - 截图/日志留存
- **验收**：场景全部通过

### T-C3 文档收尾
- **内容**：更新五份文档与 HANDOFF
- **预期产出**：spec/plan/task/checklist/loop 标记完成 + checklist 执行记录 + HANDOFF.md
- **验收**：文档齐全

---

## 里程碑产出汇总

| Task | 预期产出 |
|------|---------|
| T-A1 | pmDef 扩展 + brew/scoop/choco 命令 |
| T-A2 | InstallPackageManager script 路径 |
| T-A3 | 前端安装按钮 + 确认对话框 |
| T-B1 | Archive.Form 字段 |
| T-B2 | HasFormSpecificArchives + ToolInfo |
| T-B3 | InstallToolWithForm API |
| T-B4 | 前端形态选择器 |
| T-B5 | OpenCode manifest 标注 |
| T-C1 | 全量回归 + 构建 |
| T-C2 | 手动场景 |
| T-C3 | 文档收尾 |

---

**审批请求**：请审批本 task 清单。获批后产出 checklist，checklist 获批后产出 loop，全部获批后才开始实施。