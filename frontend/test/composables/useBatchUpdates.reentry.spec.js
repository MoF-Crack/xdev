// Ticket 12 (audit-fixes / P0-14, G-02) — 批量重入守卫。
//
// 历史上 installAllUpdates 只在确认弹窗 OK 之后才置 batchActive，因此
// 弹窗停留期间可再次进入；且被拒方（或迟到的第二次确认）的 finally 仍会
// 复位共享的 batchActive/showProgress → 运行中的批次失去保护、进度框被关。
//
// B-2：batchActive=true 时入口直接拒绝（toast install.busy）。
// B-3：弹窗窗口双入——第二次确认在 OK 后的二次检查被拒，且运行中批次的
//      batchActive/showProgress 不被被拒方破坏（ownership）。
import { ref } from 'vue';
import { afterEach, describe, expect, it, vi } from 'vitest';

import { cleanupWailsMocks, installWailsMocks } from '../mocks/wails.js';
import { useInstall } from '../../src/composables/useInstall.js';
import { useParallelBatch } from '../../src/composables/useParallelBatch.js';
import { useBatchUpdates } from '../../src/composables/useBatchUpdates.js';

afterEach(cleanupWailsMocks);

const UPDATES = [
  { tool: 'node', current: '20.0.0', latestVersion: '22.0.0' },
  { tool: 'go', current: '1.22.0', latestVersion: '1.24.0' },
];

// Completions are MANUAL (completeAll()) so the batch's active window is
// fully deterministic — nothing settles while the test asserts.
function setup() {
  const completions = [];
  const app = {
    InstallToolConfirmed: vi.fn(async (tool, version) => {
      completions.push(() => window.__wailsEmit('installProgress', {
        taskId: `${tool}@${version}`, tool, version, phase: 'Completed', percent: 100,
      }));
      return null;
    }),
    // unverified:false → no per-item confirmation dialogs (isolates the
    // batch-dialog re-entry window).
    PreviewInstall: vi.fn(async (tool, version) => ({ tool, version, unverified: false, hasFormSpecificArchives: false, availableForms: [] })),
  };
  installWailsMocks(app);

  function sessionFactory(row) {
    const session = useInstall({
      onComplete: () => { row.active = false; },
      onError: (data) => { row.error = { message: data?.error || 'err' }; row.active = false; },
      onCancel: () => { row.active = false; },
    });
    return session;
  }

  const message = { info: vi.fn(), warning: vi.fn(), success: vi.fn(), error: vi.fn() };
  const dialog = { warning: vi.fn((opts) => opts) };
  const t = (key, params) => (params ? `${key}:${JSON.stringify(params)}` : key);
  const parallelBatch = useParallelBatch({ sessionFactory, message, t });
  const availableUpdates = ref([...UPDATES]);
  const setShowProgress = vi.fn();
  const loadUpdates = vi.fn(async () => null);
  const active = ref(false);
  const batchActive = ref(false);
  const batch = useBatchUpdates({
    previewInstall: (tool, version) => app.PreviewInstall(tool, version),
    parallelBatch, dialog, message, t, loadUpdates, availableUpdates, setShowProgress,
    active, batchActive,
  });
  const completeAll = () => {
    const fns = completions.splice(0);
    fns.forEach((fn) => fn());
  };
  return { batch, parallelBatch, app, dialog, message, setShowProgress, batchActive, availableUpdates, completeAll };
}

describe('useBatchUpdates re-entry guards (ticket 12)', () => {
  it('B-2: refuses to start while batchActive is already set (toast install.busy)', async () => {
    const ctx = setup();
    ctx.batchActive.value = true;

    await ctx.batch.installAllUpdates();

    expect(ctx.message.warning).toHaveBeenCalledWith('install.busy');
    expect(ctx.dialog.warning).not.toHaveBeenCalled(); // no confirm dialog either
    expect(ctx.app.InstallToolConfirmed).not.toHaveBeenCalled();
    // The guard must not touch the running batch's state.
    expect(ctx.batchActive.value).toBe(true);
  });

  it('B-3: a second confirmation during the dialog window is rejected, running batch untouched', async () => {
    const ctx = setup();

    // Two entries both pass the entry guard (batchActive is still false)
    // and park on their own confirmation dialogs.
    const first = ctx.batch.installAllUpdates();
    const second = ctx.batch.installAllUpdates();
    await vi.waitFor(() => expect(ctx.dialog.warning).toHaveBeenCalledTimes(2));

    // Confirm the FIRST batch — it takes ownership and starts installing.
    ctx.dialog.warning.mock.calls[0][0].onPositiveClick();
    await vi.waitFor(() => expect(ctx.app.InstallToolConfirmed).toHaveBeenCalledTimes(UPDATES.length));
    expect(ctx.batchActive.value).toBe(true);
    expect(ctx.setShowProgress).toHaveBeenLastCalledWith(true);

    // The SECOND dialog is confirmed while the first batch is live: the
    // post-dialog re-check must reject it.
    ctx.dialog.warning.mock.calls[1][0].onPositiveClick();
    await second;
    expect(ctx.message.warning).toHaveBeenCalledWith('install.busy');
    // Ownership: the rejected invocation did NOT clear the live batch state.
    expect(ctx.batchActive.value).toBe(true);
    expect(ctx.setShowProgress).toHaveBeenLastCalledWith(true);
    // …and it started no additional installs.
    expect(ctx.app.InstallToolConfirmed).toHaveBeenCalledTimes(UPDATES.length);

    // The owning batch finishes normally and cleans up.
    ctx.completeAll();
    await first;
    expect(ctx.batchActive.value).toBe(false);
    expect(ctx.setShowProgress).toHaveBeenLastCalledWith(false);
  });

  it('B-3b: closing the second dialog without confirming starts nothing', async () => {
    const ctx = setup();
    const first = ctx.batch.installAllUpdates();
    const second = ctx.batch.installAllUpdates();
    await vi.waitFor(() => expect(ctx.dialog.warning).toHaveBeenCalledTimes(2));

    ctx.dialog.warning.mock.calls[0][0].onPositiveClick();
    await vi.waitFor(() => expect(ctx.app.InstallToolConfirmed).toHaveBeenCalledTimes(UPDATES.length));
    ctx.dialog.warning.mock.calls[1][0].onNegativeClick();
    await second;
    expect(ctx.app.InstallToolConfirmed).toHaveBeenCalledTimes(UPDATES.length);

    ctx.completeAll();
    await first;
  });
});
