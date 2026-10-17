// 第八轮审计 · 红→绿守卫 1（AppContent 生命周期）
//
// 缺陷（R8-01，P1）：`AppContent.vue` 的 `onMounted(async () => {...})` 在
// **多个 await 之后**（GetBootstrapState → loadUpdates → ... → loadMigrationMode）
// 才调用 `window.runtime.EventsOn(...)` 注册 4 个后端事件监听
// （xdev:tofu-warning / xdev:activation-warning / dashboardStatsUpdated /
// self-update）。而 `onUnmounted` 里的 `EventsOff` 是同步执行的。
//
// App.vue 用 `<SetupWizard v-if="!initialized" /> <AppContent v-else />` 切换视图，
// 且 `checkInit()` 在 onMounted 里异步跑（初始 `initialized = true`，一次往返后
// 可能翻成 false）。因此存在真实时序窗口：
//   1. AppContent 挂载，onMounted 走到第一个 await（GetBootstrapState 未决）；
//   2. checkInit() 返回 false → AppContent 被卸载 → EventsOff 全部执行；
//   3. 步骤 1 的 await 恢复 → **卸载之后**才 EventsOn 注册。
// 结果：这 4 个监听者永远不会被注销（Wails 的 EventsOn 按事件名把回调追加进
// 一个 Set，EventsOff(name) 只在注册之后调用才有效）。每次卸载/重挂载泄漏 4 个
// 监听者，且它们会在组件已经不在时继续跑 `message.warning(...)` / `loadDashboard()`
// ——用户看到「幽灵」激活失败 toast，并触发无意义的仪表盘重算。
//
// 修复方向：把 EventsOn 注册移到 onMounted 的**同步**前缀（第一个 await 之前），
// 或用 `onScopeDispose`/返回的 unsubscribe 函数兜底；两者都可让本用例转绿。

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

describe('R8-01 AppContent 事件监听不得在卸载后注册（红→绿）', () => {
  let resolveBoot;
  beforeEach(() => {
    installWailsMocks({
      // 永不立即返回：把 onMounted 的续体挂在 await 上，制造卸载窗口。
      GetBootstrapState: () => new Promise((res) => { resolveBoot = res; }),
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

  it('卸载后不得留下活跃订阅', async () => {
    const wrapper = mount(AppContent, { shallow: true });
    await flushPromises();

    wrapper.unmount();
    expect(window.runtime.__subscriptions().active).toBe(0);

    // 让挂起的 bootstrap 返回：onMounted 的续体继续执行。
    resolveBoot({ initialized: true, categories: [], stats: {}, favorites: [] });
    await flushPromises();
    await flushPromises();

    // 红：修复前这里 active > 0 —— 卸载之后才注册、且永不注销。
    expect(window.runtime.__subscriptions().active).toBe(0);
  });

  it('已卸载的实例不得再弹 toast（幽灵提示）', async () => {
    const wrapper = mount(AppContent, { shallow: true });
    await flushPromises();
    wrapper.unmount();
    resolveBoot({ initialized: true, categories: [], stats: {}, favorites: [] });
    await flushPromises();
    await flushPromises();

    naive.message.warning.mockClear();
    window.__wailsEmit('xdev:activation-warning', { tool: 'node', version: '22.0.0', message: 'access denied' });
    await flushPromises();

    // 红：修复前为 1（组件已不在，toast 仍弹出）。
    expect(naive.message.warning).not.toHaveBeenCalled();
  });
});
