# Spec 026 Plan: 附属工具动态安装

> **状态**：待批准
> **前置**：Spec 026 已获批
> **日期**：2026-08-05
> **下一阶段**：本 plan 获批后 → task → checklist → loop → 全部获批才实施

## 总原则

- **增量优先**：pmDef 扩展 + detectPackageManager 两阶段检测，不重构现有安装管线
- **复用扫描结果**：附属工具的 CanInstall 复用同批次 GetPackageManagers 的检测结果，不新增扫描
- **向后兼容**：现有 pmDef（npm/uvx/cargo 等）不受影响
- **验证门**：每个 Phase 结束跑对应测试；C 阶段做端到端验证

## Phase 划分与依赖

```
A 后端能力（A1 pmDef 扩展 + A2 两阶段检测 + A3 InstallSubTool API）
   │  （A1→A2→A3 串行，同文件）
   │
├─ B 前端动态提示 + 选项对话框（依赖 A 后端 API 定型）
   │
└─ C 集成验证（依赖 A+B 全部）
```

## Phase A — 后端能力

### A1（F1）：pmDef 扩展

**涉及**：`internal/gui/app_local_detect.go`

- `pmDef` 新增字段：`enableCommand string` / `parentTool string` / `installMethods []installMethod`
- 新增 `type installMethod struct { name, command, detectCmd string }`
- `packageManagerDefs()` 更新：
  - yarn：`enableCommand="corepack enable yarn"`, `parentTool="node"`
  - pnpm：`enableCommand="corepack enable pnpm"`, `parentTool="node"`
  - pipx：`installMethods=[{name:"pip",command:"pip install pipx",detectCmd:"pip"},{name:"uv",command:"uv tool install pipx",detectCmd:"uv"}]`
- `PackageManagerInfo` 新增：`EnableCommand string` json + `InstallMethods []InstallMethodInfo` json
- **测试**：pmDef 扩展后现有包管理器检测无回归
- **验收**：`go test ./internal/gui/ -short -count=1 -run "PmDef"`

### A2（F2）：detectPackageManager 两阶段检测

**涉及**：`internal/gui/app_local_detect.go`

- `GetPackageManagers()` 改为两阶段：
  1. 第一阶段：并发检测所有 pmDef 的基础状态（Installed/Version/LocalPath）——现有逻辑
  2. 第二阶段：遍历结果，对附属工具（enableCommand/installMethods 非空）根据同批次检测结果计算动态 CanInstall + InstallHint
- 附属工具逻辑：
  - `enableCommand != ""`：查同批次 `parentTool` 的 pmDef 结果 → 已安装则 CanInstall=true, InstallHint="Node 已安装，点击启用", EnableCommand=enableCommand
  - `installMethods` 非空：遍历 methods，查每个 method.detectCmd 的同批次结果 → 收集可用 methods → ≥1 个可用则 CanInstall=true
- **测试**：Node 已安装时 yarn CanInstall=true；Node 未安装时 false；pip+uv 都在时 pipx InstallMethods=[pip,uv]；仅 pip 时 [pip]；都不在时 false
- **验收**：`go test ./internal/gui/ -short -count=1 -run "Detect|SubTool"`

### A3（F3）：InstallSubTool API

**涉及**：`internal/gui/app_local_detect.go`

- 新增 `func (a *App) InstallSubTool(toolName, method string) error`
  - 查 pmDef：enableCommand 非空 → 走 enable 路径（exec corepack enable）
  - installMethods 非空 + method 指定 → 走 method 路径（exec 对应 command）
  - method="" + enableCommand 非空 → enable 路径
  - method="" + installMethods 长度==1 → 用唯一 method
  - method="" + installMethods 长度>1 → 返回错误"请指定安装方式"
- exec 构造：
  - enable：`exec.Command("corepack", "enable", subtoolName)`（PATH 含 node 环境）
  - method：解析 command 字符串为 args（简单 split，命令硬编码无注入风险）
- 成功后 `InvalidateToolsCache`
- **测试**：mock exec 验证命令构造；method 不匹配返回错误
- **验收**：`go test ./internal/gui/ -short -count=1 -run "InstallSubTool"`

## Phase B — 前端动态提示 + 选项对话框

### B1：前端动态提示 + 选项对话框

**涉及**：`frontend/src/AppContent.vue`、`frontend/src/components/ToolTable.vue`、i18n

- 包管理器列表：
  - `CanInstall=true` + `EnableCommand` 非空 → 显示「启用」按钮（非「安装」）
  - `CanInstall=true` + `InstallMethods` 长度 > 1 → 显示「安装」按钮，点击弹选项对话框
  - `CanInstall=true` + `InstallMethods` 长度 == 1 → 显示「安装」按钮，直接调 InstallSubTool
- 选项对话框：每个 InstallMethod 一个选项（pip / uv），用户选 → 调 `InstallSubTool(toolName, selectedMethod)`
- 启用确认对话框：显示 EnableCommand + 安全提示
- i18n：`pm.enable_button` / `pm.enable_confirm_title` / `pm.enable_confirm_content` / `pm.install_method_selector_title` / `pm.install_method_selector_hint`（zh/en）
- **验收**：`npx vite build` + vitest

## Phase C — 集成验证

### C1：全量回归 + 构建
- `go test ./... -short -count=1` 全绿
- `wails build` 产出 xdev-gui.exe
- 前端 vitest + vite build
- e2e 新用例

### C2：手动场景
- Node 已安装 → yarn 显示「启用」→ 点击 → corepack enable yarn → 成功
- Node 未安装 → yarn 显示提示"需先安装 Node.js"
- pip+uv 都在 → pipx 点「安装」→ 弹选项 → 选 pip → pip install pipx → 成功
- 仅 pip → pipx 点「安装」→ 直接 pip install pipx
- 都不在 → pipx 显示提示"需先安装 pip 或 uv"

### C3：文档收尾
- spec/plan/task/checklist/loop 标记完成 + HANDOFF.md

## 关键风险与缓解

| 风险 | 缓解 |
|------|------|
| 同批次检测结果竞态 | 两阶段：先并发检测收齐，再串行计算附属工具 CanInstall |
| corepack 不在 PATH | 检测 corepack 可用性，不可用时提示 Node 版本过旧 |
| method 参数注入 | method 硬编码在 pmDef，前端只传 name，后端查 pmDef 确认 |
| command split 解析 | 命令硬编码在 pmDef，简单 split 即可，无注入风险 |

## 里程碑

| Phase | 交付 | 验证门 |
|-------|------|--------|
| A | pmDef 扩展 + 两阶段检测 + InstallSubTool | go test（gui） |
| B | 前端动态提示 + 选项对话框 | vite build + vitest |
| C | 集成验证 | 全量 go test + wails build + e2e + 手动场景 |

---

**审批请求**：请审批本 plan。获批后产出 task，task 获批后产出 checklist，checklist 获批后产出 loop，全部获批后才开始实施。

## 批准记录 (Approval History)

> **批准ID**: `2026-08-08-plan-026-001`
