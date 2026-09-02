// Spec 037 阶段 1 集成测试：真实 useInstall 会话 + 真实 useParallelBatch +
// 真实 useBatchUpdates（串行确认 → 并行执行）。
// 继承 Spec 036 冒烟反馈 #2 的契约意图：单项失败不吞掉其余项 + 汇总真实。
// 并行架构下「首个失败中止批量」结构性不可能（无循环可 break）——
// 专项测试锁定该不变量。
import { ref } from 'vue';
import { afterEach, describe, expect, it, vi } from 'vitest';

import { cleanupWailsMocks, installWailsMocks } from '../mocks/wails.js';
import { useInstall } from '../../src/composables/useInstall.js';
import { useParallelBatch } from '../../src/composables/useParallelBatch.js';
import { useBatchUpdates } from '../../src/composables/useBatchUpdates.js';

afterEach(cleanupWailsMocks);

const FOUR_UPDATES = [
  { tool: 'node', current: '20.0.0', latestVersion: '22.0.0' },
  { tool: 'go', current: '1.22.0', latestVersion: '1.24.0' },
  { tool: 'python', current: '3.11.0', latestVersion: '3.13.0' },
  { tool: 'rust', current: '1.75.0', latestVersion: '1.82.0' },
];

function setup({ updates, backend, voidReturn }) {
  const listeners = {};
  const app = {
    // backend(tool, version): 'fail'（事件失败）| 'ok'（完成）| 'reject'（IPC 拒绝）| 'slow-ok'
    InstallToolConfirmed: vi.fn(async (tool, version) => {
      const behavior = backend(tool, version);
      if (behavior === 'reject') throw new Error('backend rejected');
      const emit = (name, payload) => listeners[name]?.forEach((fn) => fn(payload));
      if (behavior === 'fail') {
        queueMicrotask(() => {
          emit('installError', { taskId: `${tool}@${version}`, tool, version, error: 'download failed' });
        });
      } else if (behavior === 'slow-ok') {
        setTimeout(() => {
          emit('installProgress', { taskId: `${tool}@${version}`, tool, version, phase: 'Completed', percent: 100 });
        }, 50);
      } else {
        queueMicrotask(() => {
          emit('installProgress', { taskId: `${tool}@${version}`, tool, version, phase: 'Completed', percent: 100 });
        });
      }
      return null;
    }),
    PreviewInstall: vi.fn(async () => ({ unverified: true, hasFormSpecificArchives: false, availableForms: [] })),
  };
  const runtime = {
    EventsOn: vi.fn((name, callback) => { (listeners[name] ||= new Set()).add(callback); return () => listeners[name].delete(callback); }),
    EventsOff: vi.fn(),
  };
  installWailsMocks(app, runtime);

  const message = { info: vi.fn(), warning: vi.fn(), success: vi.fn(), error: vi.fn() };
  const dialog = { warning: vi.fn((opts) => opts) };
  const t = (key, params) => (params ? `${key}:${JSON.stringify(params)}` : key);
  const availableUpdates = ref([...updates]);
  const setShowProgress = vi.fn();
  const loadUpdates = vi.fn(async () => null);
  const batchActive = ref(false);

  // Spec 037: every row gets a REAL useInstall session (mirrors
  // AppContent's createBatchSession — event routing by taskId).
  function sessionFactory(row) {
    const session = useInstall({
      onComplete: () => { row.active = false; },
      onError: (data) => { row.error = { message: data?.error || 'err' }; row.active = false; },
      onCancel: () => { row.active = false; },
    });
    return {
      // voidReturn=true 模拟旧 startInstallConfirmed 的 void 返回。
      installConfirmed: async (tool, version, formFilter) => {
        const started = await session.installConfirmed(tool, version, formFilter);
        return voidReturn ? undefined : started;
      },
      waitForCompletion: () => session.waitForCompletion(),
      cancel: () => session.cancel(),
      pause: () => session.pause(),
      resume: () => session.resume(),
    };
  }

  const parallelBatch = useParallelBatch({ sessionFactory, message, t });
  const active = ref(false);
  const batch = useBatchUpdates({
    previewInstall: (tool, version) => app.PreviewInstall(tool, version),
    parallelBatch,
    dialog, message, t, loadUpdates, availableUpdates, setShowProgress,
    active, batchActive,
  });
  return { batch, parallelBatch, app, dialog, message, availableUpdates, setShowProgress, batchActive };
}

// drive: click the batch confirm + each per-item unverified dialog (all positive).
async function drive(batch, dialog, itemDialogCount) {
  const p = batch.installAllUpdates();
  await vi.waitFor(() => expect(dialog.warning).toHaveBeenCalled());
  dialog.warning.mock.calls[0][0].onPositiveClick?.();
  for (let i = 0; i < itemDialogCount; i++) {
    await vi.waitFor(() => expect(dialog.warning.mock.calls.length).toBeGreaterThanOrEqual(i + 2));
    dialog.warning.mock.calls[i + 1][0].onPositiveClick?.();
  }
  await p;
}

describe('batch updates integration (real useInstall + useParallelBatch + useBatchUpdates)', () => {
  it('continues to the remaining items after the first item fails (event error)', async () => {
    const ctx = setup({ updates: FOUR_UPDATES, backend: (tool) => (tool === 'node' ? 'fail' : 'ok') });
    await drive(ctx.batch, ctx.dialog, 4);
    expect(ctx.app.InstallToolConfirmed).toHaveBeenCalledTimes(4);
    expect(ctx.message.info).toHaveBeenCalledWith('updates.install_all_summary:' + JSON.stringify({ done: 3, failed: 1, skipped: 0 }));
    expect(ctx.message.warning).not.toHaveBeenCalled();
  });

  it('continues after the first item fails (IPC rejection)', async () => {
    const ctx = setup({ updates: FOUR_UPDATES, backend: (tool) => (tool === 'node' ? 'reject' : 'ok') });
    await drive(ctx.batch, ctx.dialog, 4);
    expect(ctx.app.InstallToolConfirmed).toHaveBeenCalledTimes(4);
    expect(ctx.message.info).toHaveBeenCalledWith('updates.install_all_summary:' + JSON.stringify({ done: 3, failed: 1, skipped: 0 }));
  });

  it('INVARIANT (Spec 037): a void-returning session can no longer abort the batch — all items still run', async () => {
    // 旧架构契约（冒烟反馈 #2）：void 返回 → undefined 被当失败 → 下一项
    // 预检 break → 批量首个项后中止。并行架构无「下一项预检」——void 会话
    // 只是该项计 failed，其余项照常执行。锁定该不变量。
    const ctx = setup({ updates: FOUR_UPDATES, backend: () => 'slow-ok', voidReturn: true });
    await drive(ctx.batch, ctx.dialog, 4);
    expect(ctx.app.InstallToolConfirmed).toHaveBeenCalledTimes(4); // 全部 4 项都启动
    expect(ctx.message.info).toHaveBeenCalledWith('updates.install_all_summary:' + JSON.stringify({ done: 0, failed: 4, skipped: 0 }));
  });

  it('FIXED semantics: slow-completing installs all succeed across the whole batch', async () => {
    const ctx = setup({ updates: FOUR_UPDATES, backend: () => 'slow-ok' });
    await drive(ctx.batch, ctx.dialog, 4);
    expect(ctx.app.InstallToolConfirmed).toHaveBeenCalledTimes(4);
    expect(ctx.message.info).toHaveBeenCalledWith('updates.install_all_summary:' + JSON.stringify({ done: 4, failed: 0, skipped: 0 }));
  });

  it('rows mirror per-session progress state for the multi-item dialog', async () => {
    const ctx = setup({ updates: FOUR_UPDATES, backend: () => 'ok' });
    await drive(ctx.batch, ctx.dialog, 4);
    expect(ctx.parallelBatch.items.length).toBe(4);
    expect(ctx.parallelBatch.items.every((row) => !row.active)).toBe(true);
    expect(ctx.parallelBatch.running.value).toBe(false);
  });
});
