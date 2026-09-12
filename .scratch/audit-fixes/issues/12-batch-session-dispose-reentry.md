# 12 批量会话 dispose + 重入守卫

Status: resolved
Type: task
批次: B2
对应修复项: P0-14（方案 §4 P0-14，含复核修订的弹窗窗口/ownership/测试升级）

## 问题

- G-01：createBatchSession 每项 2 EventsOn + 5 watch 泄漏（无 effect scope → onScopeDispose 不注册；run/clear 均不 dispose）。
- G-02：installAllUpdates 缺 batchActive 重入守卫；批量确认弹窗停留期间第二次点击可通过入口守卫（batchActive 在弹窗 OK 后才置位）；被拒方 finally 仍复位共享 batchActive/showProgress → 运行中批次失去保护、进度框被关（G-02 症状复活窗口）。

## 方案

1. **dispose 管道**：createBatchSession 用 `effectScope()` 包裹（scope.run 内建会话 + watch，scope 挂 row._scope）；run 的 allSettled 后、clear()/run() 开头重置 items 前对旧行 `row._scope?.stop(); row.session?.dispose()`。
2. **重入守卫**：入口补 `if (batchActive?.value) { warning; return; }`；**弹窗 OK 后二次检查 batchActive**；run() 开头 `if (items.some(it => it.active)) return`；**finally 所有权**：batchActive/showProgress 只复位「本调用置位的」标志（ownership）——被拒方不得复位运行中批次的状态。
3. DashboardPage「全部更新」按钮 disabled 增加批量状态。
4. **测试升级（v1 断言空洞）**：「两次 run 后 EventsOn 计数不增长」须用**真实 useInstall 会话 + mock window.runtime.EventsOn 计数器 + window.go.gui.App 桩**（纯 mock sessionFactory 下 mock 会话从不调 EventsOn）。

## 守卫测试

①两次 run 后订阅数不增长；②batchActive=true 时 installAllUpdates 被拒 toast；③弹窗窗口双击被二次检查拒绝且运行中批次不受影响；④单装 active 守卫语义不变。

## Comments（Batch 2 实施记录）

- 改动：
  - `frontend/src/composables/useParallelBatch.js` — `run()` 内每行用 `effectScope()` 包裹 `sessionFactory(row)`（会话 + 5 个 mirror watch 归属该 scope，`useInstall` 的 `onScopeDispose` 因此注册），scope 挂 `row._scope`；新增 `disposeRow`/`disposeAll`（`scope.stop()` + `session.dispose()`，双重幂等），在 run 统计完成后、`clear()` 与 run 开头重置前调用；**第 3 层重入防线** `if (items.some(it => it.active)) return {0,0,0}`。
  - `frontend/src/composables/useBatchUpdates.js` — **四层防线**：①入口 `batchActive` 拒绝（toast install.busy）②弹窗 OK 后二次检查 `batchActive` 拒绝 ③（在 useParallelBatch 的 items-active）④**ownership**：`const owned = !batchActive.value` 后才置位，finally 中仅 owner 复位 `batchActive`/`setShowProgress`（被拒方不破坏运行中批次状态）。
  - `frontend/src/pages/DashboardPage.vue` + `AppContent.vue` — 「全部更新」按钮 `:disabled="!updates?.length || batchInstalling"`（新增 props 接线）。
  - `frontend/test/mocks/wails.js` — EventsOn 订阅计数器（`window.runtime.__subscriptions()` 返回 {active,total}）+ `window.__wailsEmit` 派发辅助 + cleanup 清理（B-1 基建升级）。
- 守卫测试：`frontend/test/composables/useParallelBatch.dispose.spec.js`（B-1 两轮 run 后活跃订阅归零 + `_hadScope` 根因断言 + disposed 会话的 ref 变更不再镜像到 row〔watch 泄漏可观测面〕；B-1b clear() 释放；B-4 run 重入防线）；`frontend/test/composables/useBatchUpdates.reentry.spec.js`（B-2 入口拒绝 + 不触碰运行中状态；B-3 弹窗窗口双入被二次检查拒绝 + ownership 断言〔batchActive 仍 true、setShowProgress 最后一次仍是 true、无额外安装〕；B-3b 取消第二次弹窗不启动）；`frontend/test/pages/DashboardPage.spec.js`（B-5 按钮 disabled 随 batchInstalling 双向）；`frontend/e2e/app.spec.js` 用例 17 并入 B-7（批量运行中按钮 disabled → 后台运行后仍 disabled → 工具栏重开 → 完成后恢复 enabled）。
- **PoC 先红后绿（B-1）**：临时绕过 effectScope（`sessionFactory(row)` 直接调用）后运行 → 红：`AssertionError: expected false to be true`（`_hadScope` = false，会话未创建于作用域内）；恢复后 → 6/6 PASS。**副发现**：仅断言订阅计数会在无 scope 时仍绿（显式 `session.dispose()` 兜住了订阅），故 B-1 追加了 scope 归属与 watch 泄漏两个更精准断言——G-01 的真实泄漏面是 5 个 mirror watch。
- 测试确定性：B-1/B-3 系列的完成事件改为**手动触发**（`completeAll()`），避免慢计时窗口下 batchActive 已复位导致的假绿/假红。
