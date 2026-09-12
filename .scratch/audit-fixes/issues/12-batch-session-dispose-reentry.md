# 12 批量会话 dispose + 重入守卫

Status: open
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
