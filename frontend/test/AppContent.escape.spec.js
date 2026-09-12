// 票 36 (Batch 5 / P1-37 G-05) — AppContent 侧 Escape 守卫。
//
// AppShell 在 window 上监听 keydown，任何 Escape 都会 emit('escape')；修复前
// AppContent 无条件执行 setCommandPaletteVisible(false)，于是「命令面板根本没开」
// 时也会 await nextTick() 后把焦点抢到 Ctrl+K 触发器上——抽屉/对话框自己处理
// Escape 后的焦点返还契约被破坏（AppShell.spec.js 的 emit 断言不变，问题在
// 消费端）。这里直接 mount AppContent（shallow 桩掉全部子组件），从 AppShell
// stub 上 emit('escape')，走的就是模板里那条 @escape 绑定。
import { flushPromises, mount } from '@vue/test-utils';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

const naive = vi.hoisted(() => ({
  message: { success: vi.fn(), error: vi.fn(), warning: vi.fn(), info: vi.fn() },
  dialog: { warning: vi.fn(), error: vi.fn(), info: vi.fn(), success: vi.fn(), create: vi.fn() },
}));

// AppContent 在 setup 里调用 useMessage()/useDialog()；naive-ui 在 provider
// 之外会抛错，所以只替换这两个访问器（同 AppContent.versionRace.spec.js）。
vi.mock('naive-ui', async (importOriginal) => {
  const actual = await importOriginal();
  return { ...actual, useMessage: () => naive.message, useDialog: () => naive.dialog };
});

import AppContent from '../src/AppContent.vue';
import AppShell from '../src/layouts/AppShell.vue';
import { cleanupWailsMocks, installWailsMocks } from './mocks/wails.js';

function mountAppContent() {
  // attachTo：焦点断言必须发生在真实文档树里（jsdom 只对已挂载元素更新 activeElement）。
  return mount(AppContent, { shallow: true, attachTo: document.body });
}

// 触发模板上的 @escape：从 AppShell stub 上 emit，与 AppShell 的 window
// keydown 监听走同一条通路（stub 仍带父组件传入的 onEscape 监听）。
async function pressEscape(wrapper) {
  wrapper.findComponent(AppShell).vm.$emit('escape');
  await flushPromises();
}

describe('AppContent Escape guard (票 36 / G-05)', () => {
  beforeEach(() => {
    installWailsMocks({
      GetBootstrapState: async () => ({ initialized: true, categories: [], stats: {}, favorites: [] }),
      GetAvailableUpdates: async () => [],
      GetIgnoredTools: async () => [],
      GetSniffConfig: async () => ({}),
      SniffServiceStatus: async () => ({}),
      GetMigrationMode: async () => 'junction',
      ScanLegacyDir: async () => [],
      CheckToolDependencies: async () => [],
      GetCategories: async () => [],
      GetToolsByCategory: async () => [],
      GetDashboardStats: async () => ({}),
      GetFavorites: async () => [],
      GetInstalledVersions: async () => [],
      GetAvailableVersions: async () => [],
    });
  });

  afterEach(() => {
    cleanupWailsMocks();
    document.body.innerHTML = '';
  });

  it('renders a command palette and wires the escape handler to the shell', async () => {
    const wrapper = mountAppContent();
    await flushPromises();

    expect(wrapper.findComponent(AppShell).exists()).toBe(true);
    expect(wrapper.vm.showCommandPalette).toBe(false);
  });

  // 红（修复前）：面板未打开时 Escape 也会把焦点抢到 Ctrl+K 触发器，
  // 破坏抽屉/对话框自己的焦点返还。
  it('does not steal focus for a palette that is not open', async () => {
    const wrapper = mountAppContent();
    await flushPromises();

    const paletteTrigger = document.createElement('button');
    const drawerTrigger = document.createElement('button');
    document.body.append(paletteTrigger, drawerTrigger);
    // 真实的 commandPaletteTrigger 是 shallow 桩，无法承载焦点，用可控替身。
    wrapper.vm.commandPaletteTrigger = { $el: paletteTrigger };
    drawerTrigger.focus();
    expect(document.activeElement).toBe(drawerTrigger);

    await pressEscape(wrapper);

    expect(wrapper.vm.showCommandPalette).toBe(false);
    expect(document.activeElement).toBe(drawerTrigger);
  });

  // 正向对照：面板确实打开时 Escape 仍然关面板并把焦点还给触发器（G-05 的守卫不能过度）。
  it('still closes the open palette and returns focus to its trigger', async () => {
    const wrapper = mountAppContent();
    await flushPromises();

    const paletteTrigger = document.createElement('button');
    document.body.append(paletteTrigger);
    wrapper.vm.commandPaletteTrigger = { $el: paletteTrigger };

    wrapper.vm.openCommandPalette();
    await flushPromises();
    expect(wrapper.vm.showCommandPalette).toBe(true);

    await pressEscape(wrapper);

    expect(wrapper.vm.showCommandPalette).toBe(false);
    expect(document.activeElement).toBe(paletteTrigger);
  });
});
