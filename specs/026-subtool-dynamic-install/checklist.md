# Spec 026 Checklist（逐项验收标准）

> **状态**：待批准
> **前置**：Spec 026 + Plan + Task 已获批
> **日期**：2026-08-05
> **使用**：每项满足后在「验收」打勾并记录；全部通过后进入 LOOP 实施。

---

## Phase A — 后端能力

### T-A1 pmDef 扩展
- [ ] pmDef 新增 enableCommand string / parentTool string / installMethods []installMethod
- [ ] installMethod struct { name, command, detectCmd string }
- [ ] yarn：enableCommand="corepack enable yarn", parentTool="node"
- [ ] pnpm：enableCommand="corepack enable pnpm", parentTool="node"
- [ ] pipx：installMethods=[{pip,"pip install pipx","pip"},{uv,"uv tool install pipx","uv"}]
- [ ] PackageManagerInfo 新增 EnableCommand string + InstallMethods []InstallMethodInfo json
- [ ] 测试：pmDef 扩展后现有检测无回归
- [ ] **验收**：`go test ./internal/gui/ -short -count=1 -run "PmDef"` 通过

### T-A2 detectPackageManager 两阶段检测
- [ ] GetPackageManagers 第一阶段（现有并发检测）收齐 results
- [ ] 第二阶段：遍历 results，对 enableCommand/installMethods 非空的 pmDef 计算动态状态
- [ ] 附属工具：查同批次 parentTool 结果 → 已安装则 CanInstall=true + 动态 InstallHint
- [ ] 多方法工具：遍历 methods 查 detectCmd 结果 → 收集可用 methods → ≥1 则 CanInstall=true
- [ ] Node 已安装 → yarn CanInstall=true, InstallHint="Node 已安装，点击启用"
- [ ] Node 未安装 → yarn CanInstall=false, InstallHint="需先安装 Node.js"
- [ ] pip+uv 都在 → pipx CanInstall=true, InstallMethods=[pip,uv]
- [ ] 仅 pip → pipx CanInstall=true, InstallMethods=[pip]
- [ ] 都不在 → pipx CanInstall=false, InstallHint="需先安装 pip 或 uv"
- [ ] **验收**：`go test ./internal/gui/ -short -count=1 -run "Detect|SubTool"` 通过

### T-A3 InstallSubTool API
- [ ] InstallSubTool(toolName, method string) error
- [ ] enableCommand 非空 → exec corepack enable（PATH 含 node）
- [ ] installMethods 非空 + method 指定 → exec 对应 command
- [ ] method="" + 单一 method → 用唯一 method
- [ ] method="" + 多 method → 返回错误"请指定安装方式"
- [ ] 成功后 InvalidateToolsCache
- [ ] 测试：mock exec 验证命令构造；method 不匹配返回错误
- [ ] **验收**：`go test ./internal/gui/ -short -count=1 -run "InstallSubTool"` 通过

## Phase B — 前端动态提示 + 选项对话框

### T-B1 前端动态提示 + 选项对话框
- [ ] CanInstall=true + EnableCommand 非空 → 「启用」按钮
- [ ] 启用确认对话框：显示 EnableCommand + 安全提示 → InstallSubTool(toolName, "")
- [ ] CanInstall=true + InstallMethods 长度>1 → 「安装」按钮 → 选项对话框
- [ ] 选项对话框：每个 InstallMethod 一个选项 → InstallSubTool(toolName, selectedMethod)
- [ ] CanInstall=true + InstallMethods 长度==1 → 「安装」按钮 → 直接 InstallSubTool
- [ ] i18n：pm.enable_button / enable_confirm_title/content / install_method_selector_title/hint（zh/en）
- [ ] vitest：动态按钮渲染 + 选项对话框
- [ ] **验收**：`npx vite build` + vitest 通过

## Phase C — 集成验证

### T-C1 全量回归与构建
- [ ] `go test ./... -short -count=1` 通过（无 FAIL）
- [ ] `wails build` 产出 xdev-gui.exe
- [ ] 前端 `npx vitest run` 通过
- [ ] 前端 `npx vite build` 通过
- [ ] e2e 新用例：附属工具安装 通过

### T-C2 手动场景
- [ ] Node 已安装 → yarn「启用」→ corepack enable yarn → 成功
- [ ] Node 未安装 → yarn 提示"需先安装 Node.js"
- [ ] pip+uv 都在 → pipx「安装」→ 弹选项 → 选 pip → pip install pipx → 成功
- [ ] 仅 pip → pipx「安装」→ 直接 pip install pipx
- [ ] 都不在 → pipx 提示"需先安装 pip 或 uv"
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
| | T-C1 | | |
| | T-C2 | | |
| | T-C3 | | |

---

**审批请求**：请审批本 checklist。获批后产出 loop（实施循环与防挂起护栏），loop 获批后开始实施。