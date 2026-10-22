// 第八轮审计 · 红→绿守卫 2（F-40 修复不完整：迁移重入守卫只覆盖了两个入口）
//
// 缺陷（R8-02，P1）：第七轮 F-40 给 `migrateSingle` / `migrateAll` 加了
// `if (migratingAll.value || migratingSingle.value) return;`，并通过
// `migrationBusy` 禁用了 `EnvironmentVariablesPage` 的三个按钮。
// 但**同一时刻可用的另外两条迁移入口完全没被守卫**：
//   - `DashboardPage` 扫描结果行「迁移」→ `@scan-migrate` → `migrateScanTool`
//     → `migrateTool(name, path)` → `MigrateLocalTool`
//   - `FavoritesPage` / `ToolDetailDrawer` 的「迁移到 xdev」→ `@migrate`
//     → 同一个 `migrateTool`
// `migrateTool` 既不读 `migratingSingle`/`migratingAll`，也不设置它们，因此：
//   ① 双击「迁移到 xdev」= 两次并发 `MigrateLocalTool`；
//   ② 在 env-vars 页发起单工具迁移后，切到仪表盘/收藏页对**同一工具**再点迁移，
//      两个流程并发处理同一目录；
//   ③ `EnvironmentVariablesPage` 的 busy 态对这两个入口没有任何约束，
//      用户在整个迁移期间都没有「进行中」反馈。
// Go 侧 `MigrateLocalTool`（internal/gui/app_local_detect.go:344）与
// `migrateLocalToolByJunction`/`migrateLocalToolByMove`
// （internal/gui/app_migration.go:489/551）**都不获取** `util.AcquireToolLock`
// （全仓 `AcquireToolLock` 只出现在 installer/npm/fixLocalVersion 三处），
// 所以后端也没有兜底互斥——两个流程会同时走到
// `util.CopyDir` / `os.Rename` / `CreateJunction`，造成重复复制、半迁移目录，
// move 模式下第二个流程还可能对已被移走的源路径做 `os.RemoveAll`。
//
// 修复方向：让 `migrateTool` 复用同一对标志（或引入统一的 `migrationBusy` 判定），
// 并把 busy 态透传给 DashboardPage/FavoritesPage/ToolDetailDrawer。

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

describe('R8-02 迁移重入守卫必须覆盖 migrateTool 入口（红→绿）', () => {
  let pending;
  let migrateLocalTool;
  let migrateByJunction;
  let migrateByMove;

  beforeEach(() => {
    pending = [];
    const hang = () => new Promise((res) => pending.push(res));
    migrateLocalTool = vi.fn(hang);
    migrateByJunction = vi.fn(hang);
    migrateByMove = vi.fn(hang);
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
      MigrateLocalToolByMove: migrateByMove,
    });
  });
  afterEach(cleanupWailsMocks);

  async function drain() {
    pending.forEach((r) => r());
    await flushPromises();
  }

  it('双击「迁移到 xdev」(migrateTool) 只应触发一次后端迁移', async () => {
    const wrapper = mount(AppContent, { shallow: true });
    await flushPromises();

    const a = wrapper.vm.migrateTool('java', 'C:\\Java');
    const b = wrapper.vm.migrateTool('java', 'C:\\Java');
    await flushPromises();

    // 红：修复前 = 2（两次并发 MigrateLocalTool，后端无工具锁兜底）。
    expect(migrateLocalTool).toHaveBeenCalledTimes(1);
    await drain();
    await Promise.allSettled([a, b]);
  });

  it('env-vars 单工具迁移进行中时，migrateTool 处理同一工具必须被挡住', async () => {
    const wrapper = mount(AppContent, { shallow: true });
    await flushPromises();

    const single = wrapper.vm.migrateSingle({ name: 'java', version: '21', path: 'C:\\Java' });
    const viaDrawer = wrapper.vm.migrateTool('java', 'C:\\Java');
    await flushPromises();

    // 红：修复前 = 2（migrateSingle 与 migrateTool 并发处理同一目录）。
    expect(migrateLocalTool).not.toHaveBeenCalled();
    expect(migrateByJunction).toHaveBeenCalledTimes(1);
    await drain();
    await Promise.allSettled([single, viaDrawer]);
  });

  it('对照：migrateSingle 双击已被 F-40 守卫挡住', async () => {
    const wrapper = mount(AppContent, { shallow: true });
    await flushPromises();

    const a = wrapper.vm.migrateSingle({ name: 'java', version: '21', path: 'C:\\Java' });
    const b = wrapper.vm.migrateSingle({ name: 'java', version: '21', path: 'C:\\Java' });
    await flushPromises();

    expect(migrateByJunction).toHaveBeenCalledTimes(1);
    await drain();
    await Promise.allSettled([a, b]);
  });
});
