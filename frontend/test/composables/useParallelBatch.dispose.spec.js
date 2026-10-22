// Ticket 12 (audit-fixes / P0-14, G-01 + G-02) — 会话生命周期守卫。
//
// B-1：批量行必须释放订阅。createBatchSession 曾把会话建在组件作用域外
//      （无 effectScope）→ useInstall 的 onScopeDispose 不注册、5 个
//      mirror watch 无归属 → 每次批量泄漏 2 个 EventsOn 订阅 + 5 个
//      watcher。本 spec 用**真实 useInstall 会话** + 订阅计数 mock 断言
//      两轮 run 后活跃订阅数回到 0（修复前每轮 +2 单调增长）。
// B-4：run() 的 items-active 防线——有活动行时不得重置运行中的批次。
import { afterEach, describe, expect, it, vi } from 'vitest';
import { getCurrentScope, nextTick, toRaw, watch } from 'vue';

import { cleanupWailsMocks, installWailsMocks } from '../mocks/wails.js';
import { useInstall } from '../../src/composables/useInstall.js';
import { useParallelBatch } from '../../src/composables/useParallelBatch.js';

afterEach(cleanupWailsMocks);

// realSessionFactory mirrors AppContent.createBatchSession: a REAL
// useInstall session plus the five state-mirroring watchers.
function realSessionFactory(row) {
  // G-01 root cause probe: a session created OUTSIDE an effect scope gets
  // no onScopeDispose registration, and its mirror watchers belong to
  // nothing (they can never be stopped).
  row._hadScope = getCurrentScope() != null;
  const session = useInstall({
    onComplete: () => { row.active = false; row.paused = false; },
    onError: (data) => { row.error = { message: data?.error || 'err' }; row.active = false; },
    onCancel: () => { row.active = false; row.paused = false; },
  });
  watch(() => session.phase.value, (v) => { row.phase = v; }, { immediate: true });
  watch(() => session.progress.value, (v) => { row.percent = v?.percent || 0; }, { immediate: true });
  watch(() => session.active.value, (v) => { row.active = v; }, { immediate: true });
  watch(() => session.paused.value, (v) => { row.paused = v; }, { immediate: true });
  watch(() => session.error.value, (v) => { if (v) row.error = v; }, { immediate: true });
  return session;
}

// makeBatch wires the counting mock. Completions are MANUAL
// (completeAll()) so every assertion is deterministic — nothing settles
// behind the test's back.
function makeBatch() {
  const completions = [];
  const app = {
    InstallToolConfirmed: vi.fn(async (tool, version) => {
      completions.push(() => window.__wailsEmit('installProgress', {
        taskId: `${tool}@${version}`, tool, version, phase: 'Completed', percent: 100,
      }));
      return null;
    }),
    InstallToolWithForm: vi.fn(async () => null),
  };
  installWailsMocks(app);
  const message = { info: vi.fn(), warning: vi.fn(), success: vi.fn(), error: vi.fn() };
  const t = (key, params) => (params ? `${key}:${JSON.stringify(params)}` : key);
  const parallelBatch = useParallelBatch({ sessionFactory: realSessionFactory, message, t });
  const completeAll = () => {
    const fns = completions.splice(0);
    fns.forEach((fn) => fn());
  };
  return { parallelBatch, app, message, t, completeAll };
}

describe('useParallelBatch session lifecycle (ticket 12)', () => {
  it('B-1: releases every EventsOn subscription after each batch (no growth across runs)', async () => {
    const { parallelBatch, app, completeAll } = makeBatch();

    const first = parallelBatch.run([{ tool: 'node', version: '22.0.0' }, { tool: 'go', version: '1.26.0' }]);
    await vi.waitFor(() => expect(app.InstallToolConfirmed).toHaveBeenCalledTimes(2));
    const duringFirst = window.runtime.__subscriptions();
    expect(duringFirst.active).toBe(4); // 2 live sessions × 2 events
    completeAll();
    await first;

    const afterFirst = window.runtime.__subscriptions();
    expect(afterFirst.total).toBe(4);
    expect(afterFirst.active, `subscriptions leaked after run 1: ${JSON.stringify(afterFirst)}`).toBe(0);

    const row = parallelBatch.items[0];
    // G-01 root-cause assertion: the session was created inside a
    // dedicated effect scope (otherwise onScopeDispose never registers).
    expect(row._hadScope).toBe(true);

    // G-01 leak surface: the five mirror watchers must be stopped with the
    // scope. Mutating a disposed session's ref must no longer reach the
    // row (a leaked watcher would mirror it again). toRaw bypasses the
    // reactive proxy that unwraps ref properties.
    const rawSession = toRaw(row.session);
    const phaseAfterBatch = row.phase;
    rawSession.phase.value = 'Downloading';
    await nextTick();
    expect(row.phase, 'leaked mirror watcher still syncing a disposed session').toBe(phaseAfterBatch);

    const second = parallelBatch.run([{ tool: 'node', version: '22.0.0' }, { tool: 'go', version: '1.26.0' }]);
    await vi.waitFor(() => expect(app.InstallToolConfirmed).toHaveBeenCalledTimes(4));
    completeAll();
    await second;

    const afterSecond = window.runtime.__subscriptions();
    // Total grows (each run creates fresh sessions) but the LIVE count must
    // stay at zero — before the fix it grew by 2 per run.
    expect(afterSecond.active, `subscriptions leaked after run 2: ${JSON.stringify(afterSecond)}`).toBe(0);
  });

  it('B-1b: clear() also releases the live subscriptions', async () => {
    const { parallelBatch, app, completeAll } = makeBatch();
    const p = parallelBatch.run([{ tool: 'node', version: '22.0.0' }]);
    await vi.waitFor(() => expect(app.InstallToolConfirmed).toHaveBeenCalledTimes(1));
    completeAll();
    await p;

    parallelBatch.clear();
    expect(parallelBatch.items.length).toBe(0);
    expect(window.runtime.__subscriptions().active).toBe(0);
  });

  it('B-4: run() refuses to reset a batch that still has active rows', async () => {
    const { parallelBatch, app, completeAll } = makeBatch();
    // First run stays active (completion is manual and not yet fired).
    const first = parallelBatch.run([{ tool: 'node', version: '22.0.0' }]);
    await vi.waitFor(() => expect(app.InstallToolConfirmed).toHaveBeenCalledTimes(1));
    expect(parallelBatch.items[0].active).toBe(true);

    const summary = await parallelBatch.run([{ tool: 'go', version: '1.26.0' }]);
    expect(summary).toEqual({ done: 0, failed: 0, cancelled: 0 });
    // The live row survived untouched (a second batch did NOT start).
    expect(parallelBatch.items.length).toBe(1);
    expect(parallelBatch.items[0].tool).toBe('node');
    expect(app.InstallToolConfirmed).toHaveBeenCalledTimes(1);

    // Let the original batch finish cleanly.
    completeAll();
    await first;
    expect(parallelBatch.items[0].active).toBe(false);
  });
});
