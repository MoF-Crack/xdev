import { ref } from 'vue';
import { describe, expect, it, vi } from 'vitest';
import { useBatchUpdates } from '../../src/composables/useBatchUpdates.js';
import { useParallelBatch } from '../../src/composables/useParallelBatch.js';

// Spec 035 F2b + Spec 037 阶段 1：批量更新——串行逐项确认（FR-018）+
// 并行执行（useParallelBatch 每项独立会话）+ 真实汇总 + finally 兜底。

function setup({
  updates = [{ tool: 'node', current: '20.0.0', latestVersion: '22.0.0' }, { tool: 'go', current: '1.22.0', latestVersion: '1.24.0' }],
  previewResult = null, // null → PreviewInstall resolves this shape
  previewImpl,
  sessionStartImpl, // async (tool, version, form) => started boolean
  sessionWaitImpl,  // async () => 'completed' | 'error' | 'cancelled' | 'disposed' | 'idle'
} = {}) {
  const previewInstall = previewImpl || vi.fn(async () => (previewResult ?? { unverified: true, hasFormSpecificArchives: false, availableForms: [] }));
  const installConfirmed = vi.fn();
  const dialog = { warning: vi.fn() };
  const message = { info: vi.fn(), warning: vi.fn(), success: vi.fn(), error: vi.fn() };
  const t = (key, params) => (params ? `${key}:${JSON.stringify(params)}` : key);
  const availableUpdates = ref([...updates]);
  const setShowProgress = vi.fn();
  const loadUpdates = vi.fn(async () => null);
  const active = ref(false);
  const batchActive = ref(false);

  // Real parallel engine with mock sessions.
  const sessionFactory = (row) => ({
    installConfirmed: async (tool, version, formFilter) => {
      installConfirmed(tool, version, formFilter);
      row.phase = 'Downloading';
      const started = sessionStartImpl ? await sessionStartImpl(tool, version) : true;
      return started;
    },
    waitForCompletion: async () => {
      const result = sessionWaitImpl ? await sessionWaitImpl(tool0(row), ver0(row)) : 'completed';
      // Mirror the real session behavior for the dialog rows.
      if (result === 'completed' || result === 'cancelled' || result === 'error' || result === 'disposed') row.active = false;
      return result;
    },
    cancel: vi.fn(async () => true),
    pause: vi.fn(async () => true),
    resume: vi.fn(async () => true),
  });
  // helpers closed over row via closure-locals below
  function tool0(row) { return row?.tool; }
  function ver0(row) { return row?.version; }

  const parallelBatch = useParallelBatch({ sessionFactory, message, t });
  const batch = useBatchUpdates({
    previewInstall, parallelBatch,
    dialog, message, t, loadUpdates, availableUpdates, setShowProgress,
    active, batchActive,
  });
  return { batch, parallelBatch, previewInstall, installConfirmed, dialog, message, availableUpdates, setShowProgress, loadUpdates, active, batchActive };
}

function clickDialog(dialog, resolveWith, index = 0) {
  const call = dialog.warning.mock.calls[index];
  const opts = call[0];
  if (resolveWith === 'positive') opts.onPositiveClick?.();
  else if (resolveWith === 'negative') opts.onNegativeClick?.();
  else opts.onClose?.();
}

async function runBatch(ctx, { batchDialog = 'positive', itemDialogs = [] } = {}) {
  const p = ctx.batch.installAllUpdates();
  await vi.waitFor(() => expect(ctx.dialog.warning).toHaveBeenCalled());
  clickDialog(ctx.dialog, batchDialog);
  for (let i = 0; i < itemDialogs.length; i++) {
    await vi.waitFor(() => expect(ctx.dialog.warning.mock.calls.length).toBeGreaterThanOrEqual(i + 2));
    clickDialog(ctx.dialog, itemDialogs[i], i + 1);
  }
  await p;
}

describe('useBatchUpdates (Spec 037 phase 1: serial confirm + parallel run)', () => {
  it('installs each unverified item after per-item confirmation', async () => {
    const ctx = setup();
    await runBatch(ctx, { itemDialogs: ['positive', 'positive'] });
    expect(ctx.dialog.warning).toHaveBeenCalledTimes(3);
    expect(ctx.installConfirmed).toHaveBeenCalledTimes(2);
    expect(ctx.installConfirmed).toHaveBeenCalledWith('node', '22.0.0', '');
    expect(ctx.installConfirmed).toHaveBeenCalledWith('go', '1.24.0', '');
    expect(ctx.message.info).toHaveBeenCalledWith('updates.install_all_summary:' + JSON.stringify({ done: 2, failed: 0, skipped: 0 }));
  });

  it('skips a declined item and continues with the rest', async () => {
    const ctx = setup();
    await runBatch(ctx, { itemDialogs: ['negative', 'positive'] });
    expect(ctx.installConfirmed).toHaveBeenCalledTimes(1);
    expect(ctx.installConfirmed).toHaveBeenCalledWith('go', '1.24.0', '');
    expect(ctx.message.info).toHaveBeenCalledWith('updates.install_all_summary:' + JSON.stringify({ done: 1, failed: 0, skipped: 1 }));
  });

  it('treats a PreviewInstall failure as unverified and still asks', async () => {
    const ctx = setup({ previewImpl: vi.fn(async () => { throw new Error('old build'); }) });
    await runBatch(ctx, { itemDialogs: ['positive', 'negative'] });
    expect(ctx.dialog.warning).toHaveBeenCalledTimes(3);
    expect(ctx.installConfirmed).toHaveBeenCalledTimes(1);
    expect(ctx.message.info).toHaveBeenCalledWith('updates.install_all_summary:' + JSON.stringify({ done: 1, failed: 0, skipped: 1 }));
  });

  it('auto-selects the single available form', async () => {
    const ctx = setup({
      updates: [{ tool: 'opencode', current: '1.0.0', latestVersion: '1.2.0' }],
      previewResult: { unverified: true, hasFormSpecificArchives: true, availableForms: ['cli'] },
    });
    await runBatch(ctx, { itemDialogs: ['positive'] });
    expect(ctx.installConfirmed).toHaveBeenCalledWith('opencode', '1.2.0', 'cli');
  });

  it('counts a failed start as failed (all items still attempted)', async () => {
    const ctx = setup({ sessionStartImpl: vi.fn(async () => false) });
    await runBatch(ctx, { itemDialogs: ['positive', 'positive'] });
    expect(ctx.installConfirmed).toHaveBeenCalledTimes(2);
    expect(ctx.message.info).toHaveBeenCalledWith('updates.install_all_summary:' + JSON.stringify({ done: 0, failed: 2, skipped: 0 }));
  });

  it('counts an error result as failed', async () => {
    const ctx = setup({ sessionWaitImpl: vi.fn(async () => 'error') });
    await runBatch(ctx, { itemDialogs: ['positive', 'positive'] });
    expect(ctx.message.info).toHaveBeenCalledWith('updates.install_all_summary:' + JSON.stringify({ done: 0, failed: 2, skipped: 0 }));
    expect(ctx.setShowProgress).toHaveBeenCalledWith(false);
  });

  it('cancelling items counts them as skipped, others still complete', async () => {
    const ctx = setup({ sessionWaitImpl: vi.fn(async (tool) => (tool === 'node' ? 'cancelled' : 'completed')) });
    await runBatch(ctx, { itemDialogs: ['positive', 'positive'] });
    expect(ctx.message.info).toHaveBeenCalledWith('updates.install_all_summary:' + JSON.stringify({ done: 1, failed: 0, skipped: 1 }));
  });

  it('treats an idle wait (lost events) as failed, never as success', async () => {
    const ctx = setup({ sessionWaitImpl: vi.fn(async () => 'idle') });
    await runBatch(ctx, { itemDialogs: ['positive', 'positive'] });
    expect(ctx.message.info).toHaveBeenCalledWith('updates.install_all_summary:' + JSON.stringify({ done: 0, failed: 2, skipped: 0 }));
  });

  it('resets showProgress via the finally safety net on every exit path', async () => {
    const ctx = setup({ sessionWaitImpl: vi.fn(async () => 'cancelled') });
    // 并行模式：确认阶段两项都要点（取消不影响其他项的确认流程）。
    await runBatch(ctx, { itemDialogs: ['positive', 'positive'] });
    const calls = ctx.setShowProgress.mock.calls;
    expect(calls.length).toBeGreaterThan(0);
    expect(calls[calls.length - 1][0]).toBe(false);
  });

  it('does nothing when the batch confirm dialog is declined', async () => {
    const ctx = setup();
    await runBatch(ctx, { batchDialog: 'negative' });
    expect(ctx.installConfirmed).not.toHaveBeenCalled();
    expect(ctx.message.info).not.toHaveBeenCalled();
  });

  it('short-circuits on an empty updates list', async () => {
    const ctx = setup({ updates: [] });
    await ctx.batch.installAllUpdates();
    expect(ctx.dialog.warning).not.toHaveBeenCalled();
    expect(ctx.installConfirmed).not.toHaveBeenCalled();
  });

  // --- Spec 036 N2: install mutex ---

  it('declines to start while another install is active (N2)', async () => {
    const ctx = setup();
    ctx.active.value = true;
    await ctx.batch.installAllUpdates();
    expect(ctx.dialog.warning).not.toHaveBeenCalled();
    expect(ctx.installConfirmed).not.toHaveBeenCalled();
    expect(ctx.setShowProgress).not.toHaveBeenCalled();
    expect(ctx.message.warning).toHaveBeenCalledWith('install.busy');
  });

  it('keeps the progress dialog open when items fail to start (N2)', async () => {
    const ctx = setup({ sessionStartImpl: vi.fn(async () => false) });
    await runBatch(ctx, { itemDialogs: ['positive', 'positive'] });
    expect(ctx.installConfirmed).toHaveBeenCalledTimes(2);
    expect(ctx.message.info).toHaveBeenCalledWith('updates.install_all_summary:' + JSON.stringify({ done: 0, failed: 2, skipped: 0 }));
    const falseCalls = ctx.setShowProgress.mock.calls.filter(([v]) => v === false);
    expect(falseCalls.length).toBe(1);
  });

  // --- Spec 036 N4: no flicker ---

  it('toggles batchActive around the batch run (N4)', async () => {
    let ctx;
    const sessionWaitImpl = vi.fn(async () => {
      // sample during the parallel run
      batchSamples.push(ctx.batchActive.value);
      return 'completed';
    });
    const batchSamples = [];
    ctx = setup({
      sessionWaitImpl,
      previewResult: { unverified: false, hasFormSpecificArchives: false, availableForms: [] },
    });
    expect(ctx.batchActive.value).toBe(false);
    await runBatch(ctx, {});
    expect(batchSamples.length).toBe(2);
    expect(batchSamples.every((v) => v === true)).toBe(true);
    expect(ctx.batchActive.value).toBe(false);
  });

  it('does not flicker the progress dialog (single open + final close)', async () => {
    const ctx = setup({
      previewResult: { unverified: false, hasFormSpecificArchives: false, availableForms: [] },
    });
    await runBatch(ctx, {});
    const seq = ctx.setShowProgress.mock.calls.map(([v]) => v);
    expect(seq).toEqual([true, false]);
  });

  it('does not flicker the progress dialog when items error (N4)', async () => {
    const ctx = setup({
      sessionWaitImpl: vi.fn(async () => 'error'),
      previewResult: { unverified: false, hasFormSpecificArchives: false, availableForms: [] },
    });
    await runBatch(ctx, {});
    const seq = ctx.setShowProgress.mock.calls.map(([v]) => v);
    expect(seq).toEqual([true, false]);
  });

  // --- Spec 037: parallel engine behavior ---

  it('parallel engine: rows mirror per-item state and expose cancel', async () => {
    const ctx = setup({ sessionWaitImpl: vi.fn(async () => 'completed') });
    await runBatch(ctx, { itemDialogs: ['positive', 'positive'] });
    expect(ctx.parallelBatch.items.length).toBe(2);
    for (const row of ctx.parallelBatch.items) {
      expect(row.active).toBe(false); // completed rows deactivate
      expect(row.tool).toBeTruthy();
    }
    expect(ctx.parallelBatch.running.value).toBe(false);
  });

  it('all declined items → summary with skipped only, no sessions', async () => {
    const ctx = setup();
    await runBatch(ctx, { itemDialogs: ['negative', 'negative'] });
    expect(ctx.installConfirmed).not.toHaveBeenCalled();
    expect(ctx.parallelBatch.items.length).toBe(0);
    expect(ctx.message.info).toHaveBeenCalledWith('updates.install_all_summary:' + JSON.stringify({ done: 0, failed: 0, skipped: 2 }));
  });
});
