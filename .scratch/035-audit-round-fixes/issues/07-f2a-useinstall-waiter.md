# 07 — F2a: useInstall 等待器（waitForCompletion + 竞态防护）

Status: done
Spec: 035（F2 前置，P0）
Files: `frontend/src/composables/useInstall.js`、`frontend/test/composables/useInstall.spec.js`
Blocks: 08（批量 composable 依赖本 ticket 的 waitForCompletion）

## 改动（最小）

```js
let completionWaiter = null;
let lastResult = null;          // 事件先于 waiter 注册的竞态兜底（已装快路径同步 emit Completed）
let cancelRequested = false;    // 取消竞态标记

function notifyCompletion(result) {
  lastResult = result;
  const w = completionWaiter; completionWaiter = null;
  if (w) w(result);
}
```

- `installInternal` 开头：`cancelRequested = false`（每项重置）。
- `onProgress` 的 `phase === 'Completed'` 分支（现 :29-33）：`onComplete?.(data)` 后追加 `notifyCompletion('completed')`。
- `onError`（现 :35-42）：belongsToCurrentTask/active 守卫**之后**插入：
```js
if (cancelRequested) {
  phase.value = 'Cancelled'; active.value = false; paused.value = false;
  notifyCompletion('cancelled'); return;   // 吞「下载已取消」toast
}
```
原逻辑末尾追加 `notifyCompletion('error')`。
- `cancel()`（现 :124-134）：`await CancelDownload` **之前**置 `cancelRequested = true`；成功分支在既有 `onCancel?.(...)` 后追加 `notifyCompletion('cancelled')`；catch 内复位 cancelRequested=false（允许重试，不 notify——等待后续真实事件）。

时序闭环（拆分复审确认，现 :129 `active.value=false` 在 IPC 返回后）：IPC 返回成功 → cancel() 自身 notify('cancelled')（active 已 false，晚到的「下载已取消」installError 被 onError 的 `!active` 守卫丢弃）；IPC 期间事件先到 → onError 走 cancelRequested 分流 notify('cancelled')（cancel() 返回后自身的 notify 单槽 no-op）；IPC throw 且事件永不来 → cancelRequested 复位、waiter 挂起，用户重试 cancel 或最终由 F11 超时 emit 兜底。
- `dispose()`（现 :135-141）：unsubscribe 前追加 `notifyCompletion('disposed')`。
- 新增导出：
```js
function waitForCompletion() {
  if (!active.value) { const r = lastResult; lastResult = null; return Promise.resolve(r ?? 'idle'); }
  lastResult = null;
  return new Promise((resolve) => { completionWaiter = resolve; });
}
```
返回对象加 `waitForCompletion`。返回值集合：`'completed' | 'error' | 'cancelled' | 'disposed' | 'idle'`。

单槽幂等性（已核验）：cancel() 与 onError(cancelRequested) 双路径 notify 'cancelled' 时第二个 no-op；CancelDownload throw 不 notify（等待后续真实事件）。

## 验收

- [ ] `npx vitest run test/composables/useInstall.spec.js` 全绿，新增用例：
  - 四路 resolve：手动 emit Completed / installError / cancel 流程 / dispose
  - lastResult 竞态：installInternal 挂起期间先 emit Completed（快路径模拟）→ waitForCompletion 返回 'completed' 非 'idle'
  - cancelRequested：cancel() 后手动 emit installError「下载已取消」（IPC 返回前时序）→ waitForCompletion 得 'cancelled'、error.value 保持 null、reportError 未被调
  - 晚到事件丢弃：cancel() 完成后（active=false）emit installError → onError 守卫直接 return、lastResult 不被覆盖
- [ ] 既有 10 用例零回归（install/repair/pause/resume/cancel 行为不变）
