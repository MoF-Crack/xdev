# Spec 025 Checklist（逐项验收标准）

> **状态**：待批准
> **前置**：Spec 025 + Plan + Task 已获批
> **日期**：2026-08-04
> **使用**：每项满足后在「验收」打勾并记录；全部通过后进入 LOOP 实施。

---

## Phase A — 包管理器 script install

### T-A1 pmDef 扩展
- [ ] pmDef 新增 installCommand/requiresAdmin/executionEnv/platform 4 字段
- [ ] scoop：installCommand="irm get.scoop.sh | iex"，requiresAdmin=false，executionEnv="powershell"，platform="windows"
- [ ] brew：installCommand='/bin/bash -c "$(curl -fsSL .../install.sh)"'，requiresAdmin=false，executionEnv="bash"，platform="any"
- [ ] choco：installCommand="Set-ExecutionPolicy Bypass ...; iex (...)"，requiresAdmin=true，executionEnv="powershell"，platform="windows"
- [ ] canInstall = toolName != "" || installCommand != ""
- [ ] PackageManagerInfo 新增 RequiresAdmin bool json 字段
- [ ] 测试：brew/scoop/choco canInstall=true；现有包管理器无回归
- [ ] **验收**：`go test ./internal/gui/ -short -count=1 -run "PackageManager"` 通过

### T-A2 InstallPackageManager script 路径
- [ ] installCommand != "" → script install 路径
- [ ] 平台检查（platform != "any" && GOOS != platform → 明确错误）
- [ ] 管理员检查（Windows requiresAdmin=true && 非管理员 → 错误带命令文案）
- [ ] powershell：exec.Command("powershell","-NoProfile","-ExecutionPolicy","Bypass","-Command",cmd)
- [ ] bash：exec.Command("bash","-c",cmd) + NONINTERACTIVE=1 环境变量（brew）
- [ ] stdout/stderr 捕获到 GUI log
- [ ] cmd.Wait() 防僵尸
- [ ] 成功后重新检测确认
- [ ] 测试：mock exec 验证命令构造；平台/管理员检查返回错误
- [ ] **验收**：`go test ./internal/gui/ -short -count=1 -run "InstallPackageManager"` 通过

### T-A3 前端包管理器安装按钮
- [ ] CanInstall=true 显示「安装」按钮
- [ ] RequiresAdmin=true 文案「以管理员安装」+ tooltip
- [ ] 点击 → 确认对话框（显示完整命令 + 安全提示）
- [ ] 确认 → 调 InstallPackageManager
- [ ] i18n：pm.install_confirm_title/content/admin_required/command_label（zh/en）
- [ ] vitest：确认对话框渲染 + 安装调用
- [ ] **验收**：`npx vite build` + vitest 通过

## Phase B — 形态区分下载

### T-B1 Archive.Form 字段
- [ ] Archive.Form string（yaml:"form,omitempty"）
- [ ] 校验 ∈ {gui,cli,tui,""}（宽松模式，非法值警告不报错）
- [ ] 测试：解析带 Form 的 yaml；缺省空向后兼容
- [ ] **验收**：`go test ./internal/manifest/ -short -count=1` 通过

### T-B2 HasFormSpecificArchives helper
- [ ] func (t *Tool) HasFormSpecificArchives() bool
- [ ] ToolInfo 新增 HasFormSpecificArchives bool json
- [ ] pluginToToolInfo 填充该字段
- [ ] 测试：有 Form 标注 true；无标注 false
- [ ] **验收**：`go test ./internal/manifest/ ./internal/gui/ -short -count=1` 通过

### T-B3 InstallToolWithForm API
- [ ] ArchiveForCurrent 新增 formFilter 参数
- [ ] 优先匹配 Form==formFilter，降级 Form==""
- [ ] InstallToolWithForm(toolName, version, formFilter) 方法
- [ ] InstallWithProgress 传递 formFilter
- [ ] 测试：formFilter 筛选正确；降级兼容
- [ ] **验收**：`go test ./internal/installer/ ./internal/gui/ -short -count=1 -run "Form|InstallToolWithForm"` 通过

### T-B4 前端形态选择器
- [ ] hasFormSpecificArchives=true → 弹形态选择器
- [ ] 选择器显示 GUI/CLI/TUI 复选框（来自 formFactor）
- [ ] 用户选 → 调 InstallToolWithForm
- [ ] hasFormSpecificArchives=false → 现有行为直接安装
- [ ] i18n：install.form_selector_title/hint + form.gui/cli/tui（zh/en）
- [ ] vitest：选择器渲染 + 调用
- [ ] **验收**：`npx vite build` + vitest 通过

### T-B5 OpenCode manifest 标注 Form
- [ ] opencode.yaml Desktop App archive：form: gui
- [ ] opencode.yaml CLI archive：form: cli
- [ ] 同步 E:\XDev\manifests\ 运行时副本
- [ ] HasFormSpecificArchives(opencode)=true
- [ ] HasFormSpecificArchives(vscode)=false（回归保护）
- [ ] **验收**：manifest 解析通过

## Phase C — 集成验证

### T-C1 全量回归与构建
- [ ] `go test ./... -short -count=1` 通过（无 FAIL）
- [ ] `wails build` 产出 xdev-gui.exe
- [ ] 前端 `npx vitest run` 通过
- [ ] 前端 `npx vite build` 通过
- [ ] e2e 新用例：包管理器安装确认 + 形态选择器 通过

### T-C2 手动场景
- [ ] Scoop 安装（Windows 非管理员）→ 确认 → 后台执行 → 成功
- [ ] Chocolatey（非管理员）→ 提示需管理员
- [ ] OpenCode 安装 → 形态选择器 → 选 CLI → 只下载 CLI archive
- [ ] VS Code 安装 → 直接安装（无选择器）
- [ ] 截图/日志留存

### T-C3 文档收尾
- [ ] spec/plan/task/checklist/loop 标记完成
- [ ] checklist 执行记录表填满
- [ ] HANDOFF.md 更新

---

## 执行记录

| 日期 | Task | 验收结果 | 备注 |
|------|------|----------|------|
| | T-A1 | | |
| | T-A2 | | |
| | T-A3 | | |
| | T-B1 | | |
| | T-B2 | | |
| | T-B3 | | |
| | T-B4 | | |
| | T-B5 | | |
| | T-C1 | | |
| | T-C2 | | |
| | T-C3 | | |

---

**审批请求**：请审批本 checklist。获批后产出 loop（实施循环与防挂起护栏），loop 获批后开始实施。