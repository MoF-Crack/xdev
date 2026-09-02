import { ref } from 'vue';
import { describe, expect, it, vi } from 'vitest';
import { useBatchUpdates } from '../../src/composables/useBatchUpdates.js';

// Spec 035 F2b: batch update loop — per-item confirmation (FR-018),
// event-driven waiting, cancel-aborts-batch, truthful summary, and the
// finally safety net for the progress dialog.

function setup({
  updates = [{ tool: 'node', current: '20.0.0', latestVersion: '22.0.0' }, { tool: 'go', current: '1.22.0', latestVersion: '1.24.0' }],
  previewResult = null, // null → PreviewInstall resolves this shape
  previewImpl,
  installImpl,
  waitImpl,
} = {}) {
  const previewInstall = previewImpl || vi.fn(async () => (previewResult ?? { unverified: true, hasFormSpecificArchives: false, availableForms: [] }));
  const installConfirmed = installImpl || vi.fn(async () => true);
  const waitForCompletion = waitImpl || vi.fn(async () => 'completed');
  const dialog = {
    // Record calls and auto-resolve promise dialogs by clicking the
    // positive/negative button per test config.
    warning: vi.fn(),
  };
  const message = { info: vi.fn(), warning: vi.fn(), success: vi.fn(), error: vi.fn() };
  const t = (key, params) => (params ? `${key}:${JSON.stringify(params)}` : key);
  const availableUpdates = ref([...updates]);
  const setShowProgress = vi.fn();
  const loadUpdates = vi.fn(async () => null);
  const active = ref(false);
  const batchActive = ref(false);
  const batch = useBatchUpdates({
    previewInstall, installConfirmed, waitForCompletion,
    dialog, message, t, loadUpdates, availableUpdates, setShowProgress,
    active, batchActive,
  });
  return { batch, previewInstall, installConfirmed, waitForCompletion, dialog, message, availableUpdates, setShowProgress, loadUpdates, active, batchActive };
}

// clickDialog simulates the user clicking a dialog button: finds the
// latest dialog.warning call whose title matches and invokes its
// handler. resolveWith: 'positive' | 'negative' | 'close'.
function clickDialog(dialog, resolveWith, index = 0) {
  const call = dialog.warning.mock.calls[index];
  const opts = call[0];
  if (resolveWith === 'positive') opts.onPositiveClick?.();
  else if (resolveWith === 'negative') opts.onNegativeClick?.();
  else opts.onClose?.();
}

// runBatch drives installAllUpdates while auto-clicking dialog buttons.
// batchDialog: 'positive' for the initial batch confirm.
// itemDialogs: array of 'positive' | 'negative' per unverified dialog.
async function runBatch(ctx, { batchDialog = 'positive', itemDialogs = [] } = {}) {
  const p = ctx.batch.installAllUpdates();
  await vi.waitFor(() => expect(ctx.dialog.warning).toHaveBeenCalled());
  clickDialog(ctx.dialog, batchDialog);
  // Unverified dialogs appear one by one — click each as it shows.
  for (let i = 0; i < itemDialogs.length; i++) {
    await vi.waitFor(() => expect(ctx.dialog.warning.mock.calls.length).toBeGreaterThanOrEqual(i + 2));
    clickDialog(ctx.dialog, itemDialogs[i], i + 1);
  }
  await p;
}

describe('useBatchUpdates', () => {
  it('installs each unverified item after per-item confirmation', async () => {
    const ctx = setup();
    await runBatch(ctx, { itemDialogs: ['positive', 'positive'] });
    // 1 batch dialog + 2 per-item confirmations
    expect(ctx.dialog.warning).toHaveBeenCalledTimes(3);
    expect(ctx.installConfirmed).toHaveBeenCalledTimes(2);
    expect(ctx.installConfirmed).toHaveBeenNthCalledWith(1, 'node', '22.0.0', '');
    expect(ctx.installConfirmed).toHaveBeenNthCalledWith(2, 'go', '1.24.0', '');
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
    // Old build: preview fails → EVERY item still gets a confirmation
    // dialog (never silently skipped). First installs, second is skipped.
    expect(ctx.dialog.warning).toHaveBeenCalledTimes(3);
    expect(ctx.installConfirmed).toHaveBeenCalledTimes(1);
    expect(ctx.installConfirmed).toHaveBeenCalledWith('node', '22.0.0', '');
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

  it('counts a failed start as failed and moves on', async () => {
    const ctx = setup({ installImpl: vi.fn(async () => false) });
    await runBatch(ctx, { itemDialogs: ['positive', 'positive'] });
    expect(ctx.installConfirmed).toHaveBeenCalledTimes(2);
    expect(ctx.message.info).toHaveBeenCalledWith('updates.install_all_summary:' + JSON.stringify({ done: 0, failed: 2, skipped: 0 }));
  });

  it('counts an error result as failed and continues', async () => {
    const ctx = setup({ waitImpl: vi.fn(async () => 'error') });
    await runBatch(ctx, { itemDialogs: ['positive', 'positive'] });
    expect(ctx.message.info).toHaveBeenCalledWith('updates.install_all_summary:' + JSON.stringify({ done: 0, failed: 2, skipped: 0 }));
    expect(ctx.setShowProgress).toHaveBeenCalledWith(false);
  });

  it('aborting the batch on cancel: no further installs, warning summary', async () => {
    const ctx = setup({ waitImpl: vi.fn(async () => 'cancelled') });
    await runBatch(ctx, { itemDialogs: ['positive'] });
    expect(ctx.installConfirmed).toHaveBeenCalledTimes(1); // second item never starts
    expect(ctx.message.warning).toHaveBeenCalledWith('updates.install_all_cancelled:' + JSON.stringify({ done: 0, failed: 0, skipped: 0 }));
    expect(ctx.message.info).not.toHaveBeenCalled();
  });

  it('treats an idle wait (lost events) as failed, never as success', async () => {
    const ctx = setup({ waitImpl: vi.fn(async () => 'idle') });
    await runBatch(ctx, { itemDialogs: ['positive', 'positive'] });
    expect(ctx.message.info).toHaveBeenCalledWith('updates.install_all_summary:' + JSON.stringify({ done: 0, failed: 2, skipped: 0 }));
  });

  it('resets showProgress via the finally safety net on every exit path', async () => {
    const ctx = setup({ waitImpl: vi.fn(async () => 'cancelled') });
    await runBatch(ctx, { itemDialogs: ['positive'] });
    const calls = ctx.setShowProgress.mock.calls;
    expect(calls.length).toBeGreaterThan(0);
    expect(calls[calls.length - 1][0]).toBe(false);
  });

  it('does nothing when the batch confirm dialog is declined', async () => {
    const ctx = setup();
    await runBatch(ctx, { batchDialog: 'negative' });
    expect(ctx.installConfirmed).not.toHaveBeenCalled();
    expect(ctx.message.info).not.toHaveBeenCalled();
    expect(ctx.message.warning).not.toHaveBeenCalled();
  });

  it('resolves disposed as a cancellation (break out of the loop)', async () => {
    const ctx = setup({ waitImpl: vi.fn(async () => 'disposed') });
    await runBatch(ctx, { itemDialogs: ['positive'] });
    expect(ctx.installConfirmed).toHaveBeenCalledTimes(1);
    expect(ctx.message.warning).toHaveBeenCalled();
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
    ctx.active.value = true; // a single install is already running
    await ctx.batch.installAllUpdates();
    expect(ctx.dialog.warning).not.toHaveBeenCalled();
    expect(ctx.installConfirmed).not.toHaveBeenCalled();
    expect(ctx.setShowProgress).not.toHaveBeenCalled();
    expect(ctx.message.warning).toHaveBeenCalledWith('install.busy');
  });

  it('keeps the progress dialog open when an item fails to start (N2)', async () => {
    const ctx = setup({ installImpl: vi.fn(async () => false) });
    await runBatch(ctx, { itemDialogs: ['positive', 'positive'] });
    expect(ctx.installConfirmed).toHaveBeenCalledTimes(2);
    expect(ctx.message.info).toHaveBeenCalledWith('updates.install_all_summary:' + JSON.stringify({ done: 0, failed: 2, skipped: 0 }));
    // Only the finally net may close the dialog — no per-item close.
    const falseCalls = ctx.setShowProgress.mock.calls.filter(([v]) => v === false);
    expect(falseCalls.length).toBe(1);
  });

  it('yields the batch when a manual install starts mid-batch (N2)', async () => {
    let ctx;
    const waitImpl = vi.fn(async () => {
      // The user squeezed in a manual install via Run in Background
      // right after the first item finished.
      ctx.active.value = true;
      return 'completed';
    });
    ctx = setup({
      waitImpl,
      previewResult: { unverified: false, hasFormSpecificArchives: false, availableForms: [] },
    });
    await runBatch(ctx, {}); // non-unverified → no item dialogs
    expect(ctx.installConfirmed).toHaveBeenCalledTimes(1); // second item never starts
    expect(ctx.message.warning).toHaveBeenCalledWith('updates.install_all_cancelled:' + JSON.stringify({ done: 1, failed: 0, skipped: 0 }));
    expect(ctx.message.info).not.toHaveBeenCalled();
  });

  // --- Spec 036 N4: no flicker between batch items ---

  it('toggles batchActive around the batch loop (N4)', async () => {
    const samples = [];
    let ctx;
    const waitImpl = vi.fn(async () => {
      samples.push(ctx.batchActive.value);
      return 'completed';
    });
    ctx = setup({
      waitImpl,
      previewResult: { unverified: false, hasFormSpecificArchives: false, availableForms: [] },
    });
    expect(ctx.batchActive.value).toBe(false);
    await runBatch(ctx, {});
    expect(samples).toEqual([true, true]); // sampled inside the loop
    expect(ctx.batchActive.value).toBe(false); // reset by finally
  });

  it('does not flicker the progress dialog between batch items (N4)', async () => {
    const ctx = setup({
      previewResult: { unverified: false, hasFormSpecificArchives: false, availableForms: [] },
    });
    await runBatch(ctx, {});
    const seq = ctx.setShowProgress.mock.calls.map(([v]) => v);
    for (let i = 1; i < seq.length; i++) {
      if (seq[i - 1] === false && seq[i] === true) {
        throw new Error(`close/reopen flicker at ${i - 1}->${i}: [${seq.join(', ')}]`);
      }
    }
    expect(seq[0]).toBe(true);
    expect(seq[seq.length - 1]).toBe(false); // finally net
  });

  it('does not flicker the progress dialog when items error (N4)', async () => {
    const ctx = setup({
      waitImpl: vi.fn(async () => 'error'),
      previewResult: { unverified: false, hasFormSpecificArchives: false, availableForms: [] },
    });
    await runBatch(ctx, {});
    const seq = ctx.setShowProgress.mock.calls.map(([v]) => v);
    for (let i = 1; i < seq.length; i++) {
      if (seq[i - 1] === false && seq[i] === true) {
        throw new Error(`close/reopen flicker at ${i - 1}->${i}: [${seq.join(', ')}]`);
      }
    }
    expect(seq[seq.length - 1]).toBe(false);
    expect(ctx.message.info).toHaveBeenCalledWith('updates.install_all_summary:' + JSON.stringify({ done: 0, failed: 2, skipped: 0 }));
  });
});
