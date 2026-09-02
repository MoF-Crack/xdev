// Spec 036 冒烟反馈 #2 复现与契约锁定：批量更新首个失败后继续其余项。
// 用真实 useInstall + 真实 useBatchUpdates，replica 精确镜像 AppContent 的
// startInstallConfirmed（含 N2 守卫与 return started 契约）。
// 冒烟实测根因：startInstallConfirmed 原为 void（返回 undefined）——批量把
// undefined 当「启动失败」计 failed，而安装实际在跑（active=true），下一项
// 预检直接 break——批量在首个项后即中止。契约测试锁定该回归。
import { ref } from 'vue';
import { afterEach, describe, expect, it, vi } from 'vitest';

import { cleanupWailsMocks, installWailsMocks } from '../mocks/wails.js';
import { useInstall } from '../../src/composables/useInstall.js';
import { useBatchUpdates } from '../../src/composables/useBatchUpdates.js';

afterEach(cleanupWailsMocks);

function setup({ updates, backend, voidReturn }) {
  const listeners = {};
  const app = {
    // backend(tool, version): 'fail'（事件失败）| 'ok'（完成）| 'reject'（IPC 拒绝）
    InstallToolConfirmed: vi.fn(async (tool, version) => {
      const behavior = backend(tool, version);
      if (behavior === 'reject') throw new Error('backend rejected');
      const emit = (name, payload) => listeners[name]?.forEach((fn) => fn(payload));
      if (behavior === 'fail') {
        // 后端真实时序：IPC 先返回 nil，随后 poll goroutine 发 installError
        queueMicrotask(() => {
          emit('installError', { taskId: `${tool}@${version}`, tool, version, error: 'download failed' });
        });
      } else if (behavior === 'slow-ok') {
        // 真实下载时序：IPC 立即返回 nil，Completed 事件在下载后才到
        // （active=true 的窗口真实存在——批量第 2 项预检就撞它）。
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
  const state = useInstall({ onComplete: vi.fn(), onError: vi.fn() });

  // replica 镜像 AppContent.startInstallConfirmed；voidReturn=true 模拟修复前
  async function startInstallConfirmed(toolName, version, confirmed, formFilter = '') {
    if (state.active.value) {
      message.warning(t('install.busy'));
      return;
    }
    const started = confirmed
      ? await state.installConfirmed(toolName, version, formFilter)
      : await state.install(toolName, version, formFilter);
    if (!started && state.error.value) message.error(t('install.failed', { error: state.error.value.message }));
    if (voidReturn) return; // 修复前的 void 返回（undefined）
    return started;
  }

  const batch = useBatchUpdates({
    previewInstall: (tool, version) => window.go.gui.App.PreviewInstall(tool, version),
    installConfirmed: (tool, version, formFilter) => startInstallConfirmed(tool, version, true, formFilter),
    waitForCompletion: state.waitForCompletion,
    dialog, message, t, loadUpdates, availableUpdates, setShowProgress,
    active: state.active, batchActive,
  });
  return { batch, message, dialog, state, app, loadUpdates, batchActive, setShowProgress };
}

const FOUR_UPDATES = [
  { tool: 'node', current: '20.0.0', latestVersion: '22.0.0' },
  { tool: 'go', current: '1.22.0', latestVersion: '1.24.0' },
  { tool: 'java', current: '20.0.0', latestVersion: '21.0.0' },
  { tool: 'python', current: '3.11.0', latestVersion: '3.12.0' },
];

async function drive(batch, dialog, itemCount) {
  const p = batch.installAllUpdates();
  for (let i = 0; i <= itemCount; i++) {
    await vi.waitFor(() => expect(dialog.warning.mock.calls.length).toBeGreaterThanOrEqual(i + 1), { timeout: 2000 });
    dialog.warning.mock.calls[i][0].onPositiveClick?.();
  }
  await p;
}

describe('batch updates integration (real useInstall + useBatchUpdates)', () => {
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

  it('CONTRACT: a void-returning installConfirmed breaks the batch after the first item (regression lock)', async () => {
    // 修复前症状复现：第 1 项被误计 failed（undefined 当失败），active 仍 true
    // （slow-ok 事件 50ms 后才到）→ 第 2 项预检 break →
    // 「批量更新已取消：成功0 失败1 跳过0」。与冒烟实测症状一致。
    const ctx = setup({ updates: FOUR_UPDATES, backend: () => 'slow-ok', voidReturn: true });
    await drive(ctx.batch, ctx.dialog, 1); // 批量确认 + 第 1 项确认（第 2 项预检即 break，不再弹窗）
    expect(ctx.app.InstallToolConfirmed).toHaveBeenCalledTimes(1);
    expect(ctx.message.warning).toHaveBeenCalledWith('updates.install_all_cancelled:' + JSON.stringify({ done: 0, failed: 1, skipped: 0 }));
  });

  it('FIXED: with return started the same slow timing completes the whole batch', async () => {
    // 同 slow-ok 时序 + 修复后的布尔返回：第 1 项等待真实 Completed 事件，
    // 4 项全部尝试且全部成功。
    const ctx = setup({ updates: FOUR_UPDATES, backend: () => 'slow-ok' });
    await drive(ctx.batch, ctx.dialog, 4);
    expect(ctx.app.InstallToolConfirmed).toHaveBeenCalledTimes(4);
    expect(ctx.message.info).toHaveBeenCalledWith('updates.install_all_summary:' + JSON.stringify({ done: 4, failed: 0, skipped: 0 }));
  });
});
