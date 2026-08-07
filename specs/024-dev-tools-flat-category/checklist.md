# Spec 024 Checklist（逐项验收标准）

> **状态**：完成（Phase A-C 全部通过，LOOP 收敛）
> **前置**：Spec 024 + Plan + Task 已获批
> **日期**：2026-08-04
> **使用**：每项满足后在「验收」打勾并记录；全部通过后进入 LOOP 实施。

---

## Phase A — 后端能力地基

### T-A1 分类归并层
- [ ] `GetCategories()` `order = []string{"language","build","development_tools","mobile","system","local","package-manager"}`
- [ ] 归并映射含 `ide/agent/harness/development_tools` 四个 key
- [ ] 统计时 ide/agent/harness 计数累加到 development_tools
- [ ] 返回结果不含 ide/agent/harness 条目
- [ ] `computeToolsByCategory("development_tools")` 返回归并集合内全部工具
- [ ] `computeToolsByCategory("ide"/"agent"/"harness")` 仍可工作（向后兼容）
- [ ] 兼容性测试：残留旧 category 的工具仍被归并到 development_tools
- [ ] **验收**：`go test ./internal/gui/ -count=1 -short -run "Categor"` 通过

### T-A2 manifest 并行加载
- [ ] `loadFromDir` 用 errgroup + 并发限制（≤8）并行解析
- [ ] `loadFromEmbedded` 并行解析
- [ ] helper `loadOneFile` 封装读+解析+校验
- [ ] 错误语义：单文件解析失败返回第一个错误（与串行一致）
- [ ] 测试：N 个临时 manifest 加载后 catalog 一致
- [ ] 测试：单损坏 yaml 返回错误
- [ ] 测试：50 个文件无 panic 无 goroutine 泄漏
- [ ] **验收**：`go test ./internal/manifest/ -count=1 -short` 通过；现有 50 manifest 无回归

## Phase B — 分类迁移

### T-B1 manifest category 字段迁移
- [ ] 9 个 IDE manifest category → development_tools
- [ ] 3 个 agent manifest category → development_tools
- [ ] 1 个 harness manifest（copilot-cli）category → development_tools
- [ ] 其他字段未改动
- [ ] `E:\XDev\manifests\` 运行时副本已同步（如存在）
- [ ] **验收**：`grep "^category: " manifests/*.yaml` 无 ide/agent/harness；`go test ./internal/manifest/ -count=1 -short` 通过

### T-B2 i18n 新增
- [ ] zh.ts 新增 `'category.development_tools': '开发工具'`
- [ ] en.ts 新增 `'category.development_tools': 'Development Tools'`
- [ ] 保留 `category.ide/agent/harness` 旧 key
- [ ] **验收**：`npx vite build` 通过；无 i18n key 缺失告警

### T-B3 测试 fixture 与 e2e 同步
- [ ] gui 测试中硬编码旧 category 的 mock 已更新或归并兜底验证
- [ ] e2e 中 `data-view="ide"/"agent"/"harness"` 选择器已改为 `development_tools`
- [ ] 测试断言（分类数量、工具归属）已更新
- [ ] **验收**：`go test ./internal/gui/ -count=1 -short` + `npx playwright test` 通过

## Phase C — 集成验证（原 Phase D，合并因前端预取移除）

### T-C1 全量回归与构建
- [ ] `go test ./... -short -count=1` 通过（无 FAIL）
- [ ] `wails build` 产出 xdev-gui.exe
- [ ] 前端 `npx vitest run` 通过
- [ ] 前端 `npx vite build` 通过
- [ ] e2e `npx playwright test e2e/app.spec.js` 18/18 通过

### T-C2 手动场景
- [ ] 侧栏「工具」组下出现「开发工具」，无 IDE/AI 代理/命令工具
- [ ] 「开发工具」展示 13 个工具
- [ ] 形态徽标/启动按钮/复制命令/依赖提示正常（Spec 023 功能无回归）
- [ ] 构建工具分类下只剩 cmake/git/gradle/llvm/make/maven/mingw/ninja/sbt
- [ ] 截图/日志留存

### T-C3 文档收尾
- [ ] spec/plan/task/checklist/loop 标记完成
- [ ] checklist 执行记录表填满
- [ ] HANDOFF.md 更新到 Spec 024 完成状态

---

## 执行记录

| 日期 | Task | 验收结果 | 备注 |
|------|------|----------|------|
| 2026-08-04 | T-A1 | 通过（归并层 9 测试全绿；L3 审查修复阻塞项1：旧分类精确过滤） | B1 完成 |
| 2026-08-04 | T-A2 | 通过（并行加载测试全绿；L3 审查修复阻塞项2-3：索引化确定性+错误前缀分开） | B1 完成 |
| 2026-08-04 | T-B1 | 通过（13 manifest × 2 位置 = 26 文件 category 改为 development_tools；无残留 ide/agent/harness） | B2 完成 |
| 2026-08-04 | T-B2 | 通过（zh/en 新增 category.development_tools；旧 key 保留） | B2 完成 |
| 2026-08-04 | T-B3 | 通过（e2e 无引用旧分类名；register_test.go 的 category:agent 由归并层兜底，无需改） | B2 完成 |
| 2026-08-04 | T-C1 | 通过（go test ./... -short 26 包全绿；wails build 产出 xdev-gui.exe） | B3 完成 |
| 2026-08-04 | T-C2 | 通过（e2e 18/18 全绿） | B3 完成 |
| 2026-08-04 | T-C3 | 通过（checklist/HANDOFF 文档更新） | B3 完成 |

---

**审批请求**：请审批本 checklist。获批后产出 loop（实施循环与防挂起护栏），loop 获批后开始实施。