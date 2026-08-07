# Spec 024 Plan: 开发工具扁平化分类与列表并行加载

> **状态**：已完成
> **前置**：Spec 024 已获批
> **日期**：2026-08-04
> **下一阶段**：本 plan 获批后 → task → checklist → loop → 全部获批才实施

## 总原则

- **归并优先于字段重命名**：后端先实现归并层（F2），再做 manifest 字段调整（F1），保证迁移过程零断点
- **能力先行**：先做后端能力（并行加载 + 归并层），再改 manifest，最后改前端预取
- **向后兼容**：磁盘覆盖 manifest 残留旧 category 时归并层兜底；i18n 旧 key 保留
- **验证门**：每个 Phase 结束跑对应测试；D 阶段做端到端验证

## Phase 划分与依赖

```
A 后端能力地基（A1 归并层 + A2 并行加载，可并行，文件不重叠）
   │
   ├─ B 分类迁移（B1 manifest category 字段 + B2 i18n + B3 测试 fixture）
   │     （依赖 A1 归并层完成，确保归并兜底；B1/B2/B3 可并行）
   │
   └─ C 集成验证（依赖 A+B 全部）
```

> **范围调整（2026-08-04）**：原 Phase C「前端并行预取」已移除（避免 N 倍 RPC 负载与前端 cache 一致性复杂度）。前端按需加载保持不变。原 Phase D 集成验证改名为 Phase C。

## Phase A — 后端能力地基

### A1（F1+F2）：分类归并层

**涉及**：`internal/gui/app_tool_query.go`

- 定义归并映射：`developmentToolsCategories = map[string]bool{"ide":true,"agent":true,"harness":true,"development_tools":true}`
- `GetCategories()`：
  - `order` 改为 `[]string{"language", "build", "development_tools", "mobile", "system", "local", "package-manager"}`
  - 统计时，把 `ide/agent/harness` 的计数累加到 `development_tools`
  - 不再单独输出 `ide/agent/harness` 条目（除非有残留且未归并——但归并后必然为 0，不输出）
- `computeToolsByCategory("development_tools")`：返回 `category ∈ {ide, agent, harness, development_tools}` 的全部工具
- 保留 `computeToolsByCategory("ide"/"agent"/"harness")` 分支（向后兼容，前端不再触发但测试可能用）
- **测试**：
  - `GetCategories` 返回 `development_tools`，不含 `ide/agent/harness`
  - `GetToolsByCategory("development_tools")` 包含所有原 ide/agent/harness 工具
  - 兼容性：模拟残留旧 category 的工具仍被归并
- **验收**：`go test ./internal/gui/ -count=1 -short -run "Categor"`

### A2（F3）：manifest 并行加载

**涉及**：`internal/manifest/manifest.go`

- 新增 helper：`loadOneFile(readFunc func() ([]byte, error), filename string) (*Tool, error)` 封装"读字节 + 解析 + 校验"
- `loadFromDir`：用 `errgroup.WithContext` 并发处理每个 entry，并发度限制到 8（`semaphore.NewWeighted(8)`）或用 worker pool
  - 每个 goroutine 独立 `Tool` 变量，通过 channel 或 `sync.Map` 收集
  - 错误处理：第一个非 nil 错误触发 ctx cancel，其他 goroutine 快速退出；返回该错误
  - 无错误时按原顺序填入 map（顺序不影响，map 本身无序）
- `loadFromEmbedded`：同样并行
- `loadMergedCatalog` 的 embedded 步骤并行（disk 步骤也并行，但量小可顺序）
- **保持错误语义**：原本 `loadFromDir` 在单文件解析失败时返回 `fmt.Errorf("parse %s: %w", ...)`，并行版需保留"至少返回第一个遇到的解析错误"
- **测试**：
  - 用临时目录写 N 个 manifest，并行加载后 catalog 大小 = N，工具名集合一致
  - 单个损坏 yaml：返回错误（与串行行为一致）
  - 大量 manifest（如 50 个临时文件）加载无 panic 无泄漏
- **验收**：`go test ./internal/manifest/ -count=1 -short` 通过；现有 50 manifest 加载无回归

**A1 与 A2 文件不重叠**（A1 在 `internal/gui/`，A2 在 `internal/manifest/`），可并行实施。

## Phase B — 分类迁移

### B1（F1）：manifest category 字段调整

**涉及**：`manifests/*.yaml` 13 个文件

- 改 `category: ide` → `category: development_tools`（9 个：vscode, cursor, zed, jetbrains-idea-community, pycharm-community, webstorm, clion, goland, rubymine）
- 改 `category: agent` → `category: development_tools`（3 个：claude-code, codex, opencode）
- 改 `category: harness` → `category: development_tools`（1 个：copilot-cli）
- **不碰**其他字段（form_factor / depends / shims / archives 全部保留）
- **注意**：同步 `E:\XDev\manifests\` 运行时副本（如存在），避免磁盘覆盖回退（归并层兜底但不规范）
- **验收**：`grep "^category: " manifests/*.yaml` 不再出现 `ide/agent/harness`；`go test ./internal/manifest/ -count=1 -short` 通过

### B2（F5）：i18n 调整

**涉及**：`frontend/src/i18n/locales/zh.ts` / `en.ts`

- 新增：
  - zh: `'category.development_tools': '开发工具'`
  - en: `'category.development_tools': 'Development Tools'`
- 保留（不删）：`category.ide` / `category.agent` / `category.harness` keys 作为 fallback
- 可选：更新 `category.harness` 文案从「命令工具」改为「开发工具（旧）」避免误导——**非必须，保留即可**
- **验收**：前端 `npx vite build` 通过；无 i18n key 缺失告警

### B3：测试 fixture 同步

**涉及**：测试中引用 `ide/agent/harness` category 的 fixture

- 排查 `internal/gui/` 测试中硬编码 `category: ide` 的 mock manifest，改为 `development_tools` 或保留（归并层兜底）
- 排查 e2e 测试中引用旧分类名的选择器（`data-view="ide"` 等），改为 `development_tools`
- **验收**：`go test ./internal/gui/ -count=1 -short` + `npx playwright test` 通过

**B1/B2/B3 可并行**（manifest / i18n / 测试 fixture 文件不重叠），但都依赖 A1 归并层先完成（B1 改完后即使有残留也能兜底）。

## Phase C — 集成验证（原 Phase D，合并因前端预取移除）

### C1：全量回归 + 构建

- `go test ./... -short -count=1`（26+ 包全绿）
- `wails build` 产出 xdev-gui.exe
- 前端 `npx vitest run` + `npx vite build`
- e2e `npx playwright test e2e/app.spec.js` 18/18

### C2：手动场景

- 启动 xdev-gui，侧栏「工具」组下出现「开发工具」入口，不再出现 IDE/AI 代理/命令工具
- 点击「开发工具」→ 展示 13 个工具（vscode/cursor/zed/9 个 JetBrains/claude-code/codex/opencode/copilot-cli）
- 工具卡片形态徽标 / 启动按钮 / 复制命令 / 依赖提示全部正常（Spec 023 功能无回归）
- 构建工具分类下只剩 cmake/git/gradle/llvm/make/maven/mingw/ninja/sbt

### C3：文档收尾

- spec/plan/task/checklist/loop 全部标记完成
- checklist 执行记录表填满
- 更新 HANDOFF.md

## 关键风险与缓解

| 风险 | 缓解 |
|------|------|
| 并行加载改变错误语义 | A2 保持"返回第一个错误"；测试覆盖单文件损坏 |
| 归并层漏掉某个旧 category | 归并映射用 map 显式列举；测试覆盖每种旧 category |
| 磁盘 manifest 副本残留旧 category | F2 归并层兜底；B1 完成后同步 `E:\XDev\manifests\` |
| e2e fixture 引用旧分类名 | B3 同步更新 |
| 并行加载 goroutine 泄漏 | errgroup + context cancel；测试大量文件场景 |

## 里程碑

| Phase | 交付 | 验证门 |
|-------|------|--------|
| A | 归并层 + 并行加载 | go test（gui/manifest） |
| B | 13 manifest 迁移 + i18n + 测试 fixture | go test（manifest/gui）+ vite build |
| C | 集成验证 | 全量 go test + wails build + e2e + 手动场景 |

---

**审批请求**：请审批本 plan。获批后产出 task（细化到可执行单元），task 获批后产出 checklist，checklist 获批后产出 loop，全部获批后才开始实施。

## 批准记录 (Approval History)

> **批准ID**: `2026-08-08-plan-024-001`
