import { flushPromises, mount } from '@vue/test-utils';
import { afterEach, describe, expect, it, vi } from 'vitest';
const ui = vi.hoisted(() => ({ message: { success: vi.fn(), error: vi.fn(), warning: vi.fn(), info: vi.fn() }, dialog: { warning: vi.fn(), info: vi.fn() } }));
vi.mock('naive-ui', async original => ({ ...await original(), useMessage: () => ui.message, useDialog: () => ui.dialog }));
import AppContent from '../src/AppContent.vue';
import { cleanupWailsMocks } from './mocks/wails.js';
import { installSessionHarness } from './composables/installSessionHarness.js';
let wrapper;
afterEach(() => { wrapper?.unmount(); cleanupWailsMocks(); vi.clearAllMocks(); });
async function setup(overrides = {}) {
  const h = installSessionHarness({ GetBootstrapState: async () => ({ categories: [], stats: {}, favorites: [] }), GetAvailableUpdates: async () => [], GetIgnoredTools: async () => [], GetSniffConfig: async () => ({}), SniffServiceStatus: async () => ({}), GetMigrationMode: async () => 'junction', ScanLegacyDir: async () => [], GetInstalledVersions: async () => [], GetAvailableVersions: async () => [], GetFavorites: async () => [], GetDashboardStats: vi.fn(async () => ({})), PreviewInstall: vi.fn(async () => ({ unverified: false })), ...overrides });
  wrapper = mount(AppContent, { shallow: true }); await flushPromises(); return h;
}
describe('AppContent shared installation reservation', () => {
  it('blocks PM, batch and repair during single preflight, then releases needs-repair', async () => {
    let resolve; const h = await setup({ PreviewInstall: vi.fn(() => new Promise(r => { resolve = r; })) });
    const pending = wrapper.vm.doInstall('node', '22');
    await wrapper.vm.repairInstall('go', '1');
    await wrapper.vm.installPackageManager({ name: 'scoop', installCommand: 'install' });
    wrapper.vm.availableUpdates = [{ tool: 'go', latestVersion: '1' }];
    await wrapper.vm.installAllUpdates();
    expect(h.app.PreviewInstall).toHaveBeenCalledOnce(); expect(ui.dialog.warning).not.toHaveBeenCalled();
    expect(ui.message.warning).toHaveBeenCalledTimes(3);
    resolve({ unverified: false }); await flushPromises();
    const spec = h.app.StartInstall.mock.calls[0][0];
    h.emit(spec.sessionId, { status: 'needs-repair', error: 'incomplete' }); await pending;
    expect(wrapper.vm.installing).toBe(false); expect(h.app.GetDashboardStats).toHaveBeenCalled();
    const retry = wrapper.vm.doInstall('node', '22'); expect(h.app.PreviewInstall).toHaveBeenCalledTimes(2);
    resolve({ unverified: false }); await flushPromises();
    const second = h.app.StartInstall.mock.calls[1][0]; expect(second.sessionId).not.toBe(spec.sessionId);
    h.emit(spec.sessionId, { status: 'completed' }); expect(wrapper.vm.installing).toBe(true);
    h.emit(second.sessionId, { status: 'completed' }); await retry;
  });
  it('keeps form selection reserved, forwards repair form and releases dismissal', async () => {
    const h = await setup({ PreviewInstall: vi.fn(async () => ({ hasFormSpecificArchives: true, availableForms: ['cli', 'gui'] })) });
    const cancelled = wrapper.vm.doInstall('editor', '1'); await flushPromises();
    expect(wrapper.vm.showFormSelector).toBe(true); wrapper.vm.showFormSelector = false; await flushPromises(); await cancelled;
    const pending = wrapper.vm.repairInstall('editor', '1'); await flushPromises();
    wrapper.vm.confirmFormInstall('gui'); await flushPromises();
    ui.dialog.warning.mock.calls.at(-1)[0].onPositiveClick(); await flushPromises();
    const spec = h.app.StartInstall.mock.calls[0][0]; expect(spec).toMatchObject({ tool: 'editor', form: 'gui', force: true });
    h.emit(spec.sessionId, { status: 'completed' }); await pending;
  });
});
