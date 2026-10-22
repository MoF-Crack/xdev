// 票 25 (Batch 4 / P1-22) — 前端 loadVersions 三入口竞态守卫。
//
// 三个入口（loadVersions / openFavVersionManager / openDashVersionManager）
// 都是「发起异步请求 → 落盘」的两步流程：慢响应返回时若用户已经切走了
// 选中工具 / 展开了别的条目，无条件落盘就会用旧数据覆盖新列表（交错
// 污染）。这里用可控的 deferred Wails stub 精确安排「B 先回、A 后回」的
// 交错顺序，断言 A 的迟到响应被丢弃。
//
// 集成方式：直接 mount AppContent（shallow 桩掉所有子组件）并调用其
// setup 暴露的三个入口函数——它们是模板事件处理器的直接目标，因此这条
// 断言走的是真实组件代码路径，而不是复制出来的替身逻辑。
import { flushPromises, mount } from '@vue/test-utils';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

const naive = vi.hoisted(() => ({
  message: { success: vi.fn(), error: vi.fn(), warning: vi.fn(), info: vi.fn() },
  dialog: { warning: vi.fn(), error: vi.fn(), info: vi.fn(), success: vi.fn(), create: vi.fn() },
}));

// AppContent 在 setup 里调用 useMessage()/useDialog()；naive-ui 在 provider
// 之外会抛错，所以只替换这两个访问器，其余 naive-ui 导出保持真实。
vi.mock('naive-ui', async (importOriginal) => {
  const actual = await importOriginal();
  return { ...actual, useMessage: () => naive.message, useDialog: () => naive.dialog };
});

import AppContent from '../src/AppContent.vue';
import { cleanupWailsMocks, installWailsMocks } from './mocks/wails.js';

// deferredRegistry 让测试自己决定每个请求何时返回（键 = 方法名:工具名）。
function deferredRegistry() {
  const pending = new Map();
  return {
    request(key) {
      let resolve;
      const promise = new Promise((res) => { resolve = res; });
      pending.set(key, resolve);
      return promise;
    },
    settle(key, value) {
      const resolve = pending.get(key);
      if (!resolve) throw new Error(`no pending request for ${key}`);
      resolve(value);
    },
  };
}

// 把版本的读写绑定到 deferredRegistry 上（每次请求都返回一个未决 promise）。
function installDeferredVersionApi(registry, app) {
  app.GetInstalledVersions = vi.fn((name) => registry.request(`installed:${name}`));
  app.GetAvailableVersions = vi.fn((name) => registry.request(`available:${name}`));
}

function mountAppContent() {
  return mount(AppContent, { shallow: true });
}

describe('AppContent version-load race guards (票 25 / P1-22)', () => {
  let registry;

  beforeEach(() => {
    registry = deferredRegistry();
    installWailsMocks({
      GetBootstrapState: async () => ({ initialized: true, categories: [], stats: {}, favorites: [] }),
      GetAvailableUpdates: async () => [],
      GetIgnoredTools: async () => [],
      GetSniffConfig: async () => ({}),
      SniffServiceStatus: async () => ({}),
      GetMigrationMode: async () => 'junction',
      ScanLegacyDir: async () => [],
      CheckToolDependencies: async () => [],
    });
    installDeferredVersionApi(registry, window.go.gui.App);
  });

  afterEach(cleanupWailsMocks);

  // LV1: 打开 A → 切到 B → A 的慢响应返回 → B 的列表未被 A 覆盖。
  it('LV1: drops a late loadVersions response for the previously selected tool', async () => {
    const wrapper = mountAppContent();
    await flushPromises();

    const openAlpha = wrapper.vm.openDetail({ name: 'alpha' });
    const openBeta = wrapper.vm.openDetail({ name: 'beta' });
    await flushPromises();

    // beta（新选中）先回……
    registry.settle('installed:beta', [{ version: '2.0.0', active: true }]);
    registry.settle('available:beta', [{ version: '2.0.0' }]);
    await flushPromises();

    // ……alpha（已切走）后回，必须被丢弃。
    registry.settle('installed:alpha', [{ version: '1.0.0', active: true }]);
    registry.settle('available:alpha', [{ version: '1.0.0' }]);
    await Promise.all([openAlpha, openBeta]);
    await flushPromises();

    expect(wrapper.vm.selectedTool?.name).toBe('beta');
    expect(wrapper.vm.installedVersions).toEqual([{ version: '2.0.0', active: true }]);
    expect(wrapper.vm.availableVersions).toEqual([{ version: '2.0.0' }]);
  });

  // LV2: openFavVersionManager 同型断言。
  it('LV2: drops a late favorites version response for the previously expanded entry', async () => {
    const wrapper = mountAppContent();
    await flushPromises();

    const openAlpha = wrapper.vm.openFavVersionManager({ name: 'alpha' });
    const openBeta = wrapper.vm.openFavVersionManager({ name: 'beta' });
    await flushPromises();

    registry.settle('installed:beta', [{ version: '2.0.0', active: true }]);
    registry.settle('available:beta', [{ version: '2.0.0' }]);
    await flushPromises();

    registry.settle('installed:alpha', [{ version: '1.0.0', active: true }]);
    registry.settle('available:alpha', [{ version: '1.0.0' }]);
    await Promise.all([openAlpha, openBeta]);
    await flushPromises();

    expect(wrapper.vm.favVersionExpanded).toBe('beta');
    expect(wrapper.vm.favInstalledVers).toEqual([{ version: '2.0.0', active: true }]);
    expect(wrapper.vm.favAvailableVers).toEqual([{ version: '2.0.0' }]);
  });

  // LV3: openDashVersionManager 同型断言。
  it('LV3: drops a late dashboard version response for the previously expanded tool', async () => {
    const wrapper = mountAppContent();
    await flushPromises();

    const openAlpha = wrapper.vm.openDashVersionManager({ name: 'alpha' });
    const openBeta = wrapper.vm.openDashVersionManager({ name: 'beta' });
    await flushPromises();

    registry.settle('installed:beta', [{ version: '2.0.0', active: true }]);
    registry.settle('available:beta', [{ version: '2.0.0' }]);
    await flushPromises();

    registry.settle('installed:alpha', [{ version: '1.0.0', active: true }]);
    registry.settle('available:alpha', [{ version: '1.0.0' }]);
    await Promise.all([openAlpha, openBeta]);
    await flushPromises();

    expect(wrapper.vm.dashVersionExpanded).toBe('beta');
    expect(wrapper.vm.dashInstalledVers).toEqual([{ version: '2.0.0', active: true }]);
    expect(wrapper.vm.dashAvailableVers).toEqual([{ version: '2.0.0' }]);
  });

  // 正向对照：守卫不能把「当前条目」的正常响应也丢掉。
  it('still applies the response of the tool that is still selected', async () => {
    const wrapper = mountAppContent();
    await flushPromises();

    const openAlpha = wrapper.vm.openDetail({ name: 'alpha' });
    await flushPromises();
    registry.settle('installed:alpha', [{ version: '1.0.0', active: true }]);
    registry.settle('available:alpha', [{ version: '1.0.0' }]);
    await openAlpha;

    expect(wrapper.vm.selectedTool?.name).toBe('alpha');
    expect(wrapper.vm.availableVersions).toEqual([{ version: '1.0.0' }]);
  });
});
