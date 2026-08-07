# Spec 026 Task 清单

> **状态**：待批准
> **前置**：Spec 026 + Plan 已获批
> **日期**：2026-08-05
> **下一阶段**：本 task 获批后 → checklist → loop → 全部获批才实施

## 使用说明

- 每项 task 标注：预期产出（Deliverable）、验收标准（Acceptance）、依赖（Depends）
- 实施顺序：按 Phase；同一 Phase 内无依赖项可并行
- 全部 task 获批后才可开始实施

---

## Phase A — 后端能力

### T-A1 pmDef 扩展（F1）
- **内容**：pmDef 新增 enableCommand/parentTool/installMethods 字段 + installMethod 类型
- **涉及**：`internal/gui/app_local_detect.go`、`internal/gui/app.go`（PackageManagerInfo 新增字段）
- **预期产出**：
  - pmDef 新增 3 字段；installMethod struct
  - yarn：enableCommand="corepack enable yarn", parentTool="node"
  - pnpm：enableCommand="corepack enable pnpm", parentTool="node"
  - pipx：installMethods=[{pip,"pip install pipx","pip"},{uv,"uv tool install pipx","uv"}]
  - PackageManagerInfo 新增 EnableCommand + InstallMethods json 字段
  - 测试：pmDef 扩展后现有检测无回归
- **验收**：`go test ./internal/gui/ -short -count=1 -run "PmDef"` 通过
- **依赖**：无

### T-A2 detectPackageManager 两阶段检测（F2）
- **内容**：GetPackageManagers 两阶段——先并发检测收齐，再串行计算附属工具动态 CanInstall + InstallHint
- **涉及**：`internal/gui/app_local_detect.go`
- **预期产出**：
  - GetPackageManagers 第一阶段（现有并发检测）→ 收齐 results
  - 第二阶段：遍历 results，对 enableCommand/installMethods 非空的 pmDef 计算动态状态
  - 附属工具：查同批次 parentTool 结果 → 已安装则 CanInstall=true + 动态 InstallHint
  - 多方法工具：遍历 methods 查 detectCmd 结果 → 收集可用 methods → ≥1 则 CanInstall=true
  - 测试：Node 已安装 yarn CanInstall=true；Node 未安装 false；pip+uv 都在 pipx InstallMethods=[pip,uv]；仅 pip [pip]；都不在 false
- **验收**：`go test ./internal/gui/ -short -count=1 -run "Detect|SubTool"` 通过
- **依赖**：T-A1

### T-A3 InstallSubTool API（F3）
- **内容**：InstallSubTool(toolName, method) 支持 enable + method 路径
- **涉及**：`internal/gui/app_local_detect.go`
- **预期产出**：
  - InstallSubTool(toolName, method string) error
  - enableCommand 非空 → exec corepack enable（PATH 含 node）
  - installMethods 非空 + method 指定 → exec 对应 command
  - method="" + 单一 method → 用唯一 method
  - method="" + 多 method → 返回错误"请指定安装方式"
  - 成功后 InvalidateToolsCache
  - 测试：mock exec 验证命令构造；method 不匹配返回错误
- **验收**：`go test ./internal/gui/ -short -count=1 -run "InstallSubTool"` 通过
- **依赖**：T-A1

---

## Phase B — 前端动态提示 + 选项对话框

### T-B1 前端动态提示 + 选项对话框
- **内容**：包管理器列表动态按钮 + 选项对话框 + 启用确认对话框
- **涉及**：`frontend/src/AppContent.vue`、`frontend/src/components/ToolTable.vue`、i18n
- **预期产出**：
  - CanInstall=true + EnableCommand 非空 → 「启用」按钮 → 确认对话框 → InstallSubTool(toolName, "")
  - CanInstall=true + InstallMethods 长度>1 → 「安装」按钮 → 选项对话框 → InstallSubTool(toolName, selectedMethod)
  - CanInstall=true + InstallMethods 长度==1 → 「安装」按钮 → 直接 InstallSubTool(toolName, onlyMethod)
  - i18n：pm.enable_button / enable_confirm_title/content / install_method_selector_title/hint（zh/en）
  - vitest：动态按钮渲染 + 选项对话框
- **验收**：`npx vite build` + vitest 通过
- **依赖**：T-A2 + T-A3（API 定型）

---

## Phase C — 集成验证

### T-C1 全量回归与构建
- **内容**：回归 + 构建
- **预期产出**：
  - `go test ./... -short -count=1` 全绿
  - `wails build` 产出 xdev-gui.exe
  - 前端 vitest + vite build
  - e2e 新用例：附属工具安装
- **验收**：无 FAIL

### T-C2 手动场景
- **内容**：端到端体验
- **预期产出**：
  - Node 已安装 → yarn「启用」→ corepack enable yarn → 成功
  - Node 未安装 → yarn 提示"需先安装 Node.js"
  - pip+uv 都在 → pipx「安装」→ 弹选项 → 选 pip → pip install pipx → 成功
  - 仅 pip → pipx「安装」→ 直接 pip install pipx
  - 都不在 → pipx 提示"需先安装 pip 或 uv"
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
| T-A1 | pmDef 扩展 + installMethod 类型 |
| T-A2 | 两阶段检测 + 动态 CanInstall/InstallHint |
| T-A3 | InstallSubTool API |
| T-B1 | 前端动态提示 + 选项对话框 |
| T-C1 | 全量回归 + 构建 |
| T-C2 | 手动场景 |
| T-C3 | 文档收尾 |

---

**审批请求**：请审批本 task 清单。获批后产出 checklist，checklist 获批后产出 loop，全部获批后才开始实施。