// 第八轮审计 · R8-02 回归守卫（F-40 修复不完整：migrateTool 入口无重入守卫）
//
// `migrateSingle` / `migrateAll` 在第七轮 F-40 拿到了
// `if (migratingAll.value || migratingSingle.value) return;`，`migrationBusy` 也只
// 透传给了 `EnvironmentVariablesPage`。但同一时刻可用的另外两条迁移入口都走
// `migrateTool`：DashboardPage 扫描结果行「迁移」→ migrateScanTool → migrateTool；
// FavoritesPage / ToolDetailDrawer 的「迁移到 xdev」→ migrateTool。Go 侧
// MigrateLocalTool / migrateLocalToolByJunction / migrateLocalToolByMove 都不取工具锁，
// 所以没有后端兜底。本文件同时守卫：
//   ① `migrateTool` 的双击只触发一次后端调用；
//   ② `migrateTool` 与 `migrateSingle` 并发只触发一次；
//   ③ 迁移进行中，Dashboard/Favorites/Drawer 三个界面的迁移按钮 disabled。

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
import DashboardPage from '../src/pages/DashboardPage.vue';
import FavoritesPage from '../src/pages/FavoritesPage.vue';
import ToolDetailDrawer from '../src/components/ToolDetailDrawer.vue';
import { cleanupWailsMocks, installWailsMocks } from './mocks/wails.js';

// AppShell 必须**真实**渲染：`{ shallow: true }` 会把 AppShell 桩掉，模板里的
// 页面（DashboardPage/FavoritesPage/ToolDetailDrawer）随之消失，无法断言 props 透传。
// 只桩掉 teleport（抽屉/对话框把内容渲染到 body，stub 后留在 wrapper 内便于查询）。
const mountOptions = { global: { stubs: { teleport: true } } };

describe('R8-02 migrateTool 必须复用迁移重入守卫（红→绿）', () => {
  let pending;
  let migrateLocalTool;
  let migrateByJunction;
  // useWindowChrome 在 onMounted 里起了 500ms 轮询；测试若不卸载，轮询会活过
  // afterEach 的 cleanupWailsMocks（window.runtime 被删）→ 未捕获异常。
  const mounted = [];
  const mountApp = () => {
    const wrapper = mount(AppContent, mountOptions);
    mounted.push(wrapper);
    return wrapper;
  };

  beforeEach(() => {
    pending = [];
    const hang = () => new Promise((res) => pending.push(res));
    migrateLocalTool = vi.fn(hang);
    migrateByJunction = vi.fn(hang);
    installWailsMocks({
      GetBootstrapState: async () => ({ initialized: true, categories: [], stats: {}, favorites: [] }),
      GetAvailableUpdates: async () => [],
      GetIgnoredTools: async () => [],
      GetSniffConfig: async () => ({}),
      SniffServiceStatus: async () => ({}),
      GetMigrationMode: async () => 'junction',
      ScanLegacyDir: async () => [],
      CheckToolDependencies: async () => [],
      GetDashboardStats: async () => ({}),
      GetInstalledVersions: async () => [],
      GetAvailableVersions: async () => [],
      MigrateLocalTool: migrateLocalTool,
      MigrateLocalToolByJunction: migrateByJunction,
      MigrateLocalToolByMove: vi.fn(hang),
    });
  });
  afterEach(() => {
    while (mounted.length) mounted.pop().unmount();
    cleanupWailsMocks();
  });

  async function drain() {
    pending.forEach((r) => r());
    await flushPromises();
  }

  it('双击 migrateTool（收藏页/抽屉「迁移到 xdev」）只触发一次后端调用', async () => {
    const wrapper = mountApp();
    await flushPromises();

    const a = wrapper.vm.migrateTool('java', 'C:\\Java');
    const b = wrapper.vm.migrateTool('java', 'C:\\Java');
    await flushPromises();

    // 红：修复前 = 2（两次并发 MigrateLocalTool，Go 侧无工具锁兜底）。
    expect(migrateLocalTool).toHaveBeenCalledTimes(1);
    await drain();
    await Promise.allSettled([a, b]);
  });

  it('migrateTool 与 migrateSingle 并发只触发一次后端迁移', async () => {
    const wrapper = mountApp();
    await flushPromises();

    const single = wrapper.vm.migrateSingle({ name: 'java', version: '21', path: 'C:\\Java' });
    const viaDrawer = wrapper.vm.migrateTool('java', 'C:\\Java');
    await flushPromises();

    // 红：修复前 migrateLocalTool 被调用 1 次 + junction 1 次 = 同一目录两个流程。
    expect(migrateByJunction).toHaveBeenCalledTimes(1);
    expect(migrateLocalTool).not.toHaveBeenCalled();
    await drain();
    await Promise.allSettled([single, viaDrawer]);
  });

  it('反向顺序（migrateTool 先、migrateSingle 后）同样只触发一次', async () => {
    const wrapper = mountApp();
    await flushPromises();

    const viaDrawer = wrapper.vm.migrateTool('java', 'C:\\Java');
    const single = wrapper.vm.migrateSingle({ name: 'java', version: '21', path: 'C:\\Java' });
    await flushPromises();

    expect(migrateLocalTool).toHaveBeenCalledTimes(1);
    expect(migrateByJunction).not.toHaveBeenCalled();
    await drain();
    await Promise.allSettled([single, viaDrawer]);
  });

  it('迁移结束后标志复位，下一次迁移仍可执行', async () => {
    const wrapper = mountApp();
    await flushPromises();
    const busy = wrapper.getComponent({ name: 'DashboardPage' });
    expect(busy.props('migrationBusy')).toBe(false);

    const first = wrapper.vm.migrateTool('java', 'C:\\Java');
    await flushPromises();
    await wrapper.vm.$nextTick();
    expect(busy.props('migrationBusy')).toBe(true);
    pending.splice(0).forEach((r) => r());
    await first;
    await flushPromises();
    expect(busy.props('migrationBusy')).toBe(false);

    const second = wrapper.vm.migrateTool('go', 'C:\\Go');
    await flushPromises();
    expect(migrateLocalTool).toHaveBeenCalledTimes(2);
    await drain();
    await Promise.allSettled([second]);
  });

  it('AppContent 把 migrationBusy 透传给 DashboardPage 与 ToolDetailDrawer', async () => {
    const wrapper = mountApp();
    await flushPromises();

    const dashboard = wrapper.getComponent({ name: 'DashboardPage' });
    const drawer = wrapper.getComponent({ name: 'ToolDetailDrawer' });
    expect(dashboard.props('migrationBusy')).toBe(false);
    expect(drawer.props('migrationBusy')).toBe(false);

    const busy = wrapper.vm.migrateTool('java', 'C:\\Java');
    await flushPromises();
    await wrapper.vm.$nextTick();

    expect(dashboard.props('migrationBusy')).toBe(true);
    expect(drawer.props('migrationBusy')).toBe(true);

    await drain();
    await Promise.allSettled([busy]);
  });

  it('AppContent 把 migrationBusy 透传给 FavoritesPage', async () => {
    const wrapper = mountApp();
    await flushPromises();
    // FavoritesPage 是 defineAsyncComponent，且模板用 v-else-if 按视图切换。
    wrapper.vm.selectedView = 'favorites';
    await flushPromises();
    await new Promise((r) => setTimeout(r, 0));
    await flushPromises();

    const favorites = wrapper.getComponent({ name: 'FavoritesPage' });
    expect(favorites.props('migrationBusy')).toBe(false);

    const busy = wrapper.vm.migrateTool('java', 'C:\\Java');
    await flushPromises();
    await wrapper.vm.$nextTick();

    expect(favorites.props('migrationBusy')).toBe(true);

    await drain();
    await Promise.allSettled([busy]);
  });
});

const t = (key) => key;

describe('R8-02 迁移进行中三个界面的迁移按钮必须禁用（红→绿）', () => {
  it('DashboardPage 扫描结果行的「迁移」按钮随 migrationBusy 禁用且不再发事件', async () => {
    const scanResults = [{ name: 'java', version: '21', localPath: 'C:\\Java' }];
    const wrapper = mount(DashboardPage, {
      props: { t, stats: {}, scanResults, scanAttempted: true },
    });
    const btn = wrapper.get('[data-action="scan-migrate"]');
    expect(btn.attributes('disabled')).toBeUndefined();

    await wrapper.setProps({ migrationBusy: true });
    expect(wrapper.get('[data-action="scan-migrate"]').attributes('disabled')).toBeDefined();
    await wrapper.get('[data-action="scan-migrate"]').trigger('click');
    expect(wrapper.emitted('scan-migrate')).toBeUndefined();
  });

  it('FavoritesPage 的「迁移到 xdev」按钮随 migrationBusy 禁用且不再发事件', async () => {
    const favorites = [{ name: 'java', displayName: 'Java', category: 'language', installed: false, localVersion: '', localPath: 'C:\\Java' }];
    const wrapper = mount(FavoritesPage, { props: { t, favorites } });
    expect(wrapper.get('[data-action="migrate"]').attributes('disabled')).toBeUndefined();

    await wrapper.setProps({ migrationBusy: true });
    expect(wrapper.get('[data-action="migrate"]').attributes('disabled')).toBeDefined();
    await wrapper.get('[data-action="migrate"]').trigger('click');
    expect(wrapper.emitted('migrate')).toBeUndefined();
  });

  it('ToolDetailDrawer 的「迁移到 xdev」按钮随 migrationBusy 禁用且不再发事件', async () => {
    const tool = { name: 'java', displayName: 'Java', category: 'language', installed: false, localPath: 'C:\\Java' };
    const wrapper = mount(ToolDetailDrawer, {
      global: { stubs: { teleport: true } },
      props: { t, show: true, tool },
    });
    expect(wrapper.get('[data-action="migrate"]').attributes('disabled')).toBeUndefined();

    await wrapper.setProps({ migrationBusy: true });
    expect(wrapper.get('[data-action="migrate"]').attributes('disabled')).toBeDefined();
    await wrapper.get('[data-action="migrate"]').trigger('click');
    expect(wrapper.emitted('migrate')).toBeUndefined();
  });
});
