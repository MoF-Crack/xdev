import { flushPromises, mount } from '@vue/test-utils';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

const naive = vi.hoisted(() => ({
  message: { success: vi.fn(), error: vi.fn(), warning: vi.fn(), info: vi.fn() },
  dialog: { warning: vi.fn(), error: vi.fn(), info: vi.fn(), success: vi.fn(), create: vi.fn() },
}));
vi.mock('naive-ui', async importOriginal => {
  const actual = await importOriginal();
  return { ...actual, useMessage: () => naive.message, useDialog: () => naive.dialog };
});
import AppContent from '../src/AppContent.vue';
import SelfUpdateDialog from '../src/components/SelfUpdateDialog.vue';
import { cleanupWailsMocks, installWailsMocks } from './mocks/wails.js';

const assetName = 'xdev-windows-amd64-v1.0.0-beta.2.zip';
const assetInfo = { platform: 'windows/amd64', assetName, downloadUrls: { [assetName]: 'https://example.test/beta.2.zip' } };
const available = { current: '1.0.0-alpha.3', latest: '1.0.0-beta.2', hasUpdate: true, applied: false, ...assetInfo };
const wrappers = [];
const defaults = {
  GetBootstrapState: async () => ({ initialized: true, categories: [], stats: {}, favorites: [] }),
  GetAvailableUpdates: async () => [], GetIgnoredTools: async () => [], GetSniffConfig: async () => ({}),
  SniffServiceStatus: async () => ({}), GetMigrationMode: async () => 'junction', ScanLegacyDir: async () => [],
  GetCategories: async () => [], GetToolsByCategory: async () => [], GetDashboardStats: async () => ({}), GetFavorites: async () => [],
  GetInstalledVersions: async () => [], GetAvailableVersions: async () => [], CheckToolDependencies: async () => [],
  GetMirrorConfig: async () => ({}), GetPathMode: async () => 'shim', GetProxyConfig: async () => ({}),
  GetCacheBreakdown: async () => ({}), GetArchiveRetention: async () => 'immediate',
};
async function mountedApp(overrides = {}) {
  installWailsMocks({ ...defaults, CheckForUpdate: vi.fn().mockResolvedValue(available), ApplyUpdate: vi.fn().mockResolvedValue({ ...available, applied: true }), IgnoreSelfUpdate: vi.fn(), ...overrides });
  const wrapper = mount(AppContent, { shallow: true, global: { stubs: {
    AppShell: { template: '<main><slot /></main>' },
    SettingsPage: { name: 'SettingsPage', props: ['updateInfo', 'updateError', 'updateChecking', 'selfUpdating', 'updateBusy'], emits: ['check-update', 'open-self-update', 'retry-self-update'], template: '<section />' },
  } } });
  wrappers.push(wrapper);
  await flushPromises();
  return wrapper;
}
beforeEach(() => { vi.clearAllMocks(); });
afterEach(() => { wrappers.splice(0).forEach(wrapper => wrapper.unmount()); cleanupWailsMocks(); document.body.innerHTML = ''; });

describe('AppContent self-update integration', () => {
  it('manual check wires confirmation → ApplyUpdate → progress → explicit installed result', async () => {
    let finish;
    const apply = vi.fn().mockReturnValue(new Promise(resolve => { finish = resolve; }));
    const wrapper = await mountedApp({ ApplyUpdate: apply });
    await wrapper.vm.checkUpdate(); await flushPromises();
    const dialog = wrapper.getComponent(SelfUpdateDialog);
    expect(dialog.props()).toMatchObject({ show: true, source: 'manual', status: 'available', current: available.current, latest: available.latest, ...assetInfo });
    expect(apply).not.toHaveBeenCalled();
    dialog.vm.$emit('update'); await flushPromises();
    expect(apply).toHaveBeenCalledTimes(1); expect(dialog.props('updating')).toBe(true);
    window.__wailsEmit('self-update', { action: 'progress', phase: 'downloading', downloadedBytes: 512, totalBytes: 1024, percent: 50, current: available.current, latest: available.latest });
    await flushPromises();
    expect(dialog.props('progress')).toEqual({ phase: 'downloading', downloadedBytes: 512, totalBytes: 1024, percent: 50 });
    dialog.vm.$emit('close'); dialog.vm.$emit('ignore'); dialog.vm.$emit('update'); await flushPromises();
    expect(dialog.props('show')).toBe(true); expect(apply).toHaveBeenCalledTimes(1);
    expect(window.go.gui.App.IgnoreSelfUpdate).not.toHaveBeenCalled();
    finish({ ...available, applied: true }); await flushPromises();
    expect(dialog.props()).toMatchObject({ show: true, updating: false, status: 'applied' });
    expect(naive.message.success).toHaveBeenCalledTimes(1);
    expect(naive.message.success.mock.calls[0][0]).toContain(available.latest);
  });

  it('manual check error is passed to both settings and the retryable dialog', async () => {
    const check = vi.fn().mockResolvedValueOnce({ ...available, hasUpdate: false, error: 'network offline' }).mockResolvedValueOnce(available);
    const wrapper = await mountedApp({ CheckForUpdate: check });
    await wrapper.vm.onMenuSelect('settings'); await flushPromises();
    await wrapper.vm.checkUpdate(); await flushPromises();
    const dialog = wrapper.getComponent(SelfUpdateDialog);
    expect(dialog.props('error')).toEqual({ kind: 'check', message: 'network offline' });
    expect(dialog.props('status')).toBe('error');
    const settings = wrapper.findComponent({ name: 'SettingsPage' });
    expect(settings.props('updateError')).toEqual({ kind: 'check', message: 'network offline' });
    expect(settings.props('updateInfo').error).toBe('network offline');
    expect(naive.message.success).not.toHaveBeenCalled();
    dialog.vm.$emit('retry'); await flushPromises();
    expect(check).toHaveBeenCalledTimes(2); expect(dialog.props('status')).toBe('available');
    expect(settings.props('updateError')).toBeNull();
    dialog.vm.$emit('close'); await flushPromises();
    expect(dialog.props('show')).toBe(false);
    settings.vm.$emit('open-self-update'); await flushPromises();
    expect(dialog.props('show')).toBe(true);
    expect(window.go.gui.App.ApplyUpdate).not.toHaveBeenCalled();
  });

  it('startup notifications use the same dialog and failed automatic updates keep their real error', async () => {
    const wrapper = await mountedApp();
    expect(window.runtime.__subscriptionNames()['self-update']).toBe(1);
    window.__wailsEmit('self-update', { action: 'notify', current: available.current, latest: available.latest, ...assetInfo });
    await flushPromises();
    const dialog = wrapper.getComponent(SelfUpdateDialog);
    expect(dialog.props()).toMatchObject({ show: true, source: 'startup', status: 'available', ...assetInfo });
    expect(naive.message.info).not.toHaveBeenCalled();
    window.__wailsEmit('self-update', { action: 'applied-error', current: available.current, latest: available.latest, error: 'permission denied' });
    await flushPromises();
    expect(dialog.props()).toMatchObject({ show: true, updating: false, status: 'error', error: { kind: 'apply', message: 'permission denied' } });
    wrapper.unmount(); wrappers.splice(wrappers.indexOf(wrapper), 1);
    expect(window.runtime.__subscriptionNames()['self-update']).toBeUndefined();
  });

  it('does not open two focus-trapping dialogs via the global command palette shortcut', async () => {
    const wrapper = await mountedApp();
    wrapper.vm.openCommandPalette();
    expect(wrapper.vm.showCommandPalette).toBe(true);
    window.__wailsEmit('self-update', { action: 'notify', current: available.current, latest: available.latest });
    await flushPromises();
    expect(wrapper.vm.showCommandPalette).toBe(false);
    wrapper.vm.openCommandPalette();
    expect(wrapper.vm.showCommandPalette).toBe(false);
    expect(wrapper.getComponent(SelfUpdateDialog).props('show')).toBe(true);
  });

  it('a no-update ApplyUpdate reply is not mapped to an installed result or success toast', async () => {
    const wrapper = await mountedApp({ ApplyUpdate: vi.fn().mockResolvedValue({ current: available.latest, latest: available.latest, hasUpdate: false, applied: false }) });
    await wrapper.vm.checkUpdate(); await wrapper.vm.applySelfUpdate(); await flushPromises();
    expect(wrapper.getComponent(SelfUpdateDialog).props()).toMatchObject({ status: 'up-to-date', show: true, updating: false });
    expect(naive.message.success).not.toHaveBeenCalled();
  });
});
