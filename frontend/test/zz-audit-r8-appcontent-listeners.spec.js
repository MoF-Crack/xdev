// 第八轮审计 · R8-01 回归守卫（AppContent 事件注册的时序与注销）
//
// 缺陷：`AppContent.vue` 的 4 个 `window.runtime.EventsOn(...)`
// （xdev:tofu-warning / xdev:activation-warning / dashboardStatsUpdated / self-update）
// 写在 `onMounted(async () => {...})` 的**多个 await 之后**，而 `onUnmounted` 的
// `EventsOff` 是同步的。真实路径：App.vue 的 `checkInit()` 在 bootstrap 未决时把
// `<AppContent>` 换成 `<SetupWizard>` → 卸载先跑（此时无监听者可注销）→ 挂起的
// await 恢复 → **卸载之后**才注册 → 这些监听者永不注销，且会在组件已不在时继续
// `message.warning(...)`（幽灵 toast）。
//
// 本文件是 `zz-audit-r8-appcontent-lifecycle.spec.js` 的**互补**守卫：那份用
// `flushPromises()` 再卸载（顺序上已让注册跑完），本份严格按审计复现的时序断言
//   - 挂载后**立即**（未 await）4 个事件已注册；
//   - 卸载后订阅集合清空；
//   - 挂起的 bootstrap resolve 之后**不得**新增任何订阅；
//   - 卸载后 emit 不得再触发 toast。
//
// 修复前实测（审计者复现）：before-unmount {active:3} → after-unmount {active:0}
// → after-late-resolve {active:4, total:7}。

import { flushPromises, mount } from '@vue/test-utils';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

const naive = vi.hoisted(() => ({
  message: { success: vi.fn(), error: vi.fn(), warning: vi.fn(), info: vi.fn() },
  dialog: { warning: vi.fn(), error: vi.fn(), info: vi.fn(), success: vi.fn(), create: vi.fn() },
}));
vi.mock('naive-ui', async (importOriginal) => {
  const actual = await importOriginal();
  return { ...actual, useMessage: () => naive.message, useDialog: () => naive.dialog };
});

import AppContent from '../src/AppContent.vue';
import { cleanupWailsMocks, installWailsMocks } from './mocks/wails.js';

// 审计报告里点名的 4 个事件（localCacheUpdated 由 AppContent 自己的同步 onMounted 注册）。
const AUDITED_EVENTS = ['xdev:tofu-warning', 'xdev:activation-warning', 'dashboardStatsUpdated', 'self-update'];

describe('R8-01 AppContent 必须同步注册事件监听（红→绿）', () => {
  let resolveBoot;
  let bootPending;

  beforeEach(() => {
    naive.message.warning.mockClear();
    naive.dialog.warning.mockClear();
    resolveBoot = null;
    installWailsMocks({
      // 永不立即返回：把 onMounted 的续体挂在 await 上，制造「未决即卸载」窗口。
      GetBootstrapState: () => new Promise((res) => { resolveBoot = res; bootPending = true; }),
      GetAvailableUpdates: async () => [],
      GetIgnoredTools: async () => [],
      GetSniffConfig: async () => ({}),
      SniffServiceStatus: async () => ({}),
      GetMigrationMode: async () => 'junction',
      ScanLegacyDir: async () => [],
      CheckToolDependencies: async () => [],
      GetDashboardStats: async () => ({}),
    });
  });
  afterEach(cleanupWailsMocks);

  it('挂载后立即（未 await）4 个审计事件全部已注册', () => {
    mount(AppContent, { shallow: true });
    // 关键：此处**不** flushPromises —— bootstrap 仍未决。
    expect(bootPending).toBe(true);
    const names = window.runtime.__subscriptionNames();
    for (const name of AUDITED_EVENTS) {
      // 红：修复前这 4 个全为 0（注册被压在 await 之后）。
      expect(names[name], `${name} 必须在挂载的同步前缀注册`).toBe(1);
    }
  });

  it('卸载后订阅全部注销，且挂起的 bootstrap resolve 之后不再新增', async () => {
    const wrapper = mount(AppContent, { shallow: true });
    const beforeUnmount = window.runtime.__subscriptions();
    expect(beforeUnmount.active).toBeGreaterThanOrEqual(AUDITED_EVENTS.length);

    wrapper.unmount();
    expect(window.runtime.__subscriptions().active).toBe(0);
    expect(Object.values(window.runtime.__subscriptionNames()).every((n) => n === 0)).toBe(true);

    // 让挂起的 bootstrap 返回：onMounted 的续体继续执行。
    resolveBoot({ initialized: true, categories: [], stats: {}, favorites: [] });
    await flushPromises();
    await flushPromises();

    // 红：修复前这里 active = 4（卸载之后才注册、且永不注销）。
    expect(window.runtime.__subscriptions().active).toBe(0);
    // 注意：mock 的 unsubscribe 只从 Set 里删回调、保留空 Set，因此这里断言的是
    // 「没有任何事件还有活跃回调」，而不是键集为空。
    expect(Object.entries(window.runtime.__subscriptionNames()).filter(([, n]) => n > 0)).toEqual([]);
    for (const name of AUDITED_EVENTS) {
      expect(window.runtime.__subscriptionNames()[name] || 0, `${name} 不得在卸载后复活`).toBe(0);
    }
  });

  it('卸载后不得再弹出幽灵 toast / 对话框', async () => {
    const wrapper = mount(AppContent, { shallow: true });
    wrapper.unmount();
    resolveBoot({ initialized: true, categories: [], stats: {}, favorites: [] });
    await flushPromises();
    await flushPromises();

    naive.message.warning.mockClear();
    naive.message.info.mockClear();
    naive.dialog.warning.mockClear();

    window.__wailsEmit('xdev:activation-warning', { tool: 'node', version: '22.0.0', message: 'access denied' });
    window.__wailsEmit('xdev:tofu-warning', { tool: 'node', version: '22.0.0' });
    window.__wailsEmit('self-update', { action: 'notify', latest: '0.4.1' });
    await flushPromises();

    // 红：修复前 message.warning 被调用 1 次（组件已不在，toast 仍弹出）。
    expect(naive.message.warning).not.toHaveBeenCalled();
    expect(naive.message.info).not.toHaveBeenCalled();
    expect(naive.dialog.warning).not.toHaveBeenCalled();
  });

  it('正常挂载路径下事件仍能被投递到回调（注册未失效）', async () => {
    mount(AppContent, { shallow: true });
    await flushPromises();

    window.__wailsEmit('xdev:activation-warning', { tool: 'node', version: '22.0.0', message: 'access denied' });
    await flushPromises();

    expect(naive.message.warning).toHaveBeenCalledTimes(1);
  });
});
