# Spec 024 Task 清单

> **状态**：已完成
> **前置**：Spec 024 + Plan 已获批
> **日期**：2026-08-04
> **下一阶段**：本 task 获批后 → checklist → loop → 全部获批才实施

## 使用说明

- 每项 task 标注：预期产出（Deliverable）、验收标准（Acceptance）、依赖（Depends）
- 实施顺序：按 Phase；同一 Phase 内无依赖项可并行（由实施方决定）
- 全部 task 获批后才可开始实施

---

## Phase A — 后端能力地基

### T-A1 分类归并层（F1+F2）
- **内容**：`GetCategories` / `computeToolsByCategory` 归并 ide/agent/harness 到 development_tools
- **涉及**：`internal/gui/app_tool_query.go`、`internal/gui/app_tool_query_test.go`（新增或扩展）
- **预期产出**：
  - 归并常量/映射：`devToolsCategoryNames = map[string]bool{"ide":true,"agent":true,"harness":true,"development_tools":true}`
  - `GetCategories()`：`order = []string{"language","build","development_tools","mobile","system","local","package-manager"}`；统计时把 ide/agent/harness 计数累加到 development_tools；不输出 ide/agent/harness 条目
  - `computeToolsByCategory("development_tools")`：返回 category ∈ 归并集合的全部工具
  - 保留 `computeToolsByCategory("ide"/"agent"/"harness")` 旧分支（向后兼容）
  - 测试：归并计数正确；development_tools 包含全部旧 ide/agent/harness 工具；残留旧 category 仍归并
- **验收**：`go test ./internal/gui/ -count=1 -short -run "Categor"` 通过
- **依赖**：无

### T-A2 manifest 并行加载（F3）
- **内容**：`loadFromDir` / `loadFromEmbedded` 用 errgroup 并行解析
- **涉及**：`internal/manifest/manifest.go`、`internal/manifest/manifest_test.go`（扩展）
- **预期产出**：
  - helper `loadOneFile(name string, read func() ([]byte, error)) (*Tool, error)`
  - `loadFromDir`：errgroup + semaphore(8) 并发；channel/Map 收集；第一个错误 cancel ctx 并返回
  - `loadFromEmbedded`：同样并行
  - 错误语义：单文件解析失败返回第一个遇到的错误（与串行一致）
  - 测试：临时目录 N 个 manifest 加载后 catalog 一致；单损坏 yaml 返回错误；50 个文件无 panic
- **验收**：`go test ./internal/manifest/ -count=1 -short` 通过；现有 50 manifest 加载无回归
- **依赖**：无（与 T-A1 文件不重叠，可并行）

---

## Phase B — 分类迁移

### T-B1 manifest category 字段迁移（F1）
- **内容**：13 个 manifest 的 category 改为 development_tools
- **涉及**：`manifests/{vscode,cursor,zed,jetbrains-idea-community,pycharm-community,webstorm,clion,goland,rubymine,claude-code,codex,opencode,copilot-cli}.yaml`
- **预期产出**：
  - 9 个 IDE manifest：`category: ide` → `category: development_tools`
  - 3 个 agent manifest：`category: agent` → `category: development_tools`
  - 1 个 harness manifest：`category: harness` → `category: development_tools`
  - 其他字段不动
  - 同步 `E:\XDev\manifests\` 运行时副本（如存在）
- **验收**：`grep "^category: " manifests/*.yaml` 无 ide/agent/harness；`go test ./internal/manifest/ -count=1 -short` 通过
- **依赖**：T-A1（归并层先就绪，残留可兜底）

### T-B2 i18n 新增 development_tools key（F5）
- **内容**：zh/en 新增 category.development_tools，保留旧 key
- **涉及**：`frontend/src/i18n/locales/zh.ts`、`frontend/src/i18n/locales/en.ts`
- **预期产出**：
  - zh: `'category.development_tools': '开发工具'`
  - en: `'category.development_tools': 'Development Tools'`
  - 保留 `category.ide/agent/harness` keys
- **验收**：`npx vite build` 通过；无 i18n key 缺失告警
- **依赖**：无

### T-B3 测试 fixture 与 e2e 同步
- **内容**：排查并同步测试中引用旧分类名的 fixture
- **涉及**：`internal/gui/*_test.go`、`frontend/e2e/*.spec.js`、可能存在的 mock manifest
- **预期产出**：
  - gui 测试中硬编码 `category: ide` 的 mock：改为 `development_tools` 或保留（归并兜底）
  - e2e 中 `data-view="ide"` 等选择器：改为 `development_tools`
  - 测试断言更新：分类数量、工具归属
- **验收**：`go test ./internal/gui/ -count=1 -short` + `npx playwright test` 通过
- **依赖**：T-A1（归并层）、T-B1（字段已改）

---

## Phase C — 集成验证（原 Phase D，合并因前端预取移除）

### T-C1 全量回归与构建
- **内容**：回归 + 构建
- **预期产出**：
  - `go test ./... -short -count=1` 通过（26+ 包）
  - `wails build` 产出 xdev-gui.exe
  - 前端 `npx vitest run` + `npx vite build`
  - e2e `npx playwright test e2e/app.spec.js` 18/18
- **验收**：无 FAIL

### T-C2 手动场景
- **内容**：端到端体验验证
- **预期产出**：
  - 侧栏「工具」组下出现「开发工具」，无 IDE/AI 代理/命令工具
  - 「开发工具」展示 13 个工具，形态徽标/启动/复制命令/依赖提示正常
  - 构建工具分类下只剩 9 个真构建工具
  - 截图/日志留存
- **验收**：场景全部通过

### T-C3 文档收尾
- **内容**：更新五份文档与 HANDOFF
- **预期产出**：
  - spec/plan/task/checklist/loop 标记完成
  - checklist 执行记录表填满
  - HANDOFF.md 更新到 Spec 024 完成状态
- **验收**：文档齐全

---

## 里程碑产出汇总

| Task | 预期产出 |
|------|---------|
| T-A1 | 归并层 + 测试 |
| T-A2 | 并行加载 + 测试 |
| T-B1 | 13 manifest category 迁移 |
| T-B2 | i18n 新增 |
| T-B3 | 测试 fixture 同步 |
| T-C1 | 全量回归 + 构建 |
| T-C2 | 手动场景 |
| T-C3 | 文档收尾 |

---

**审批请求**：请审批本 task 清单。获批后产出 checklist（逐项验收标准），checklist 获批后产出 loop（实施循环），全部获批后才开始实施。