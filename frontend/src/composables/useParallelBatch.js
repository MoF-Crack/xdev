// useParallelBatch — Spec 037 阶段 1：批量更新的并行安装执行引擎。
//
// 设计（spec.md 阶段 1）：
// 1. 确认阶段串行（FR-018 逐项 unverified 确认由 useBatchUpdates 保留），
//    确认后的清单交给本引擎并行执行。
// 2. 每项一个独立安装会话（注入的 sessionFactory 每次产出一个
//    useInstall 形状的实例）：会话按 taskId 路由事件
//    （belongsToCurrentTask），N 个实例各看各的流互不串扰。
// 3. 等待 = Promise.allSettled(waitForCompletion)——单项失败/取消
//    不中断其他项（取消语义：每项独立取消 + 引擎级 cancelAll）。
// 4. items 是响应式行列表（tool/version/phase/percent/active/error），
//    供 InstallProgressDialog 多项模式与工具栏聚合指示器渲染。
// 5. 后端不动：dlMgr 天然并发；不同工具的解压/激活被
//    AcquireToolLock（按工具名）正确互斥——并行下载、串行落地。
//
// 单装路径不受影响（仍走 AppContent 的单会话）。

import { computed, effectScope, reactive } from 'vue';

export function useParallelBatch({ sessionFactory, message, t }) {
  const items = reactive([]);
  const running = computed(() => items.some((it) => it.active));

  function makeRow(tool, version, formFilter) {
    return reactive({
      tool, version, formFilter,
      phase: '', percent: 0, speed: '', eta: '',
      bytes: 0, totalBytes: 0,
      active: true, paused: false, error: null,
      session: null,
      // effect scope owning the session's watchers/disposers (G-01)
      _scope: null,
      // internal promise plumbing
      _started: null, _waiter: null,
    });
  }

  // disposeRow releases one row's subscriptions: the effect scope (which
  // stops the mirror watchers and triggers useInstall's onScopeDispose →
  // EventsOn unsubscription) plus a direct session dispose for good
  // measure (both are idempotent). Row STATE (phase/percent/error) stays
  // visible for the dialog until clear()/run() resets the list.
  function disposeRow(row) {
    row._scope?.stop?.();
    row.session?.dispose?.();
  }

  function disposeAll() {
    for (const row of items) disposeRow(row);
  }

  // run executes the confirmed list in parallel. Returns summary
  // { done, failed, cancelled } — one entry per outcome per item.
  async function run(confirmedList) {
    if (!confirmedList?.length) return { done: 0, failed: 0, cancelled: 0 };
    // Ticket 12 (G-02 layer 3): never reset a running batch — a second
    // entry must not wipe the live rows.
    if (items.some((it) => it.active)) return { done: 0, failed: 0, cancelled: 0 };
    // Ticket 12 (G-01): dispose leftover rows from the previous batch
    // before resetting the list (subscriptions leaked forever before).
    disposeAll();
    items.length = 0;

    const waiters = confirmedList.map((entry) => {
      const row = makeRow(entry.tool, entry.version, entry.formFilter || '');
      items.push(row);

      // Ticket 12 (G-01): each session (and its mirror watchers) is
      // created inside a DEDICATED effect scope. Outside a scope the
      // watchers attach to nothing and useInstall's onScopeDispose
      // never registers — every batch row leaked 2 EventsOn
      // subscriptions + 5 watchers on every run. The scope is stored on
      // the row so disposeRow can stop everything later.
      const scope = effectScope();
      const session = scope.run(() => sessionFactory(row));
      row._scope = scope;
      row.session = session;

      // Start the install; capture the completion waiter.
      row._started = (async () => {
        const started = await session.installConfirmed(entry.tool, entry.version, entry.formFilter || '');
        if (!started) {
          // Start failure is already surfaced by the session's error path.
          row.active = false;
          return 'failed';
        }
        const result = await session.waitForCompletion();
        // Terminal results deactivate the row regardless of whether the
        // session's callbacks already did (mocked sessions may not).
        row.active = false;
        row.paused = false;
        return result; // 'completed' | 'error' | 'cancelled' | 'disposed' | 'idle'
      })();

      return row._started;
    });

    const results = await Promise.allSettled(waiters);

    let done = 0;
    let failed = 0;
    let cancelled = 0;
    for (const r of results) {
      const value = r.status === 'fulfilled' ? r.value : 'error';
      if (value === 'completed') done++;
      else if (value === 'cancelled' || value === 'disposed') cancelled++;
      else failed++; // 'error' | 'idle' | 'failed' | exception
    }
    // Ticket 12 (G-01): the batch has settled — release the row
    // subscriptions now. The rows themselves stay visible (final state)
    // until the user closes the dialog / clear() / the next run.
    for (const row of items) disposeRow(row);
    // Rows keep their final state (completed/error/cancelled) visible in
    // the dialog until the user closes it.
    return { done, failed, cancelled };
  }

  async function cancelAll() {
    await Promise.allSettled(items.map((row) => row.session?.cancel?.().catch(() => {})));
  }

  async function pause(row) {
    if (row?.session) await row.session.pause();
  }

  async function resume(row) {
    if (row?.session) await row.session.resume();
  }

  function clear() {
    // Ticket 12 (G-01): release subscriptions before dropping the rows.
    disposeAll();
    items.length = 0;
  }

  return { items, running, run, cancelAll, pause, resume, clear };
}
