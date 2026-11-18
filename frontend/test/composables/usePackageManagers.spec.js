import { ref } from 'vue';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanupWailsMocks } from '../mocks/wails.js';
import { usePackageManagers } from '../../src/composables/usePackageManagers.js';
import { createInstallCoordinator } from '../../src/composables/useInstallCoordinator.js';
import { installSessionHarness } from './installSessionHarness.js';
afterEach(cleanupWailsMocks);
const flush = async () => { for (let i = 0; i < 12; i++) await Promise.resolve(); };
function setup(overrides = {}) {
  const h = installSessionHarness({ GetAvailableVersions: vi.fn(async () => [{ version: '1' }]), PreviewInstall: vi.fn(async () => ({ unverified: false })), MigratePackageManager: vi.fn(async () => null), ...overrides });
  const coordinator = createInstallCoordinator();
  const message = { success: vi.fn(), warning: vi.fn(), error: vi.fn() };
  const dialog = { warning: vi.fn() }; const refreshTools = vi.fn(async () => {});
  const pm = usePackageManagers({ t: key => key, message, dialog, refreshTools, selectedTool: ref({}), coordinator });
  return { ...h, coordinator, pm, message, dialog, refreshTools };
}
describe('package manager session ownership', () => {
  it.each([
    [{ name: 'pipx', installMethods: [{ name: 'pip', command: 'pip install pipx' }] }, 'sub-tool', 'pip', false],
    [{ name: 'pnpm', enableCommand: 'corepack enable pnpm' }, 'sub-tool', '', true],
    [{ name: 'scoop', installCommand: 'irm get.scoop.sh | iex' }, 'package-manager', '', true],
  ])('waits for terminal success of $0.name', async (tool, kind, method, confirm) => {
    const h = setup(); const pending = h.pm.installPackageManager(tool);
    if (confirm) h.dialog.warning.mock.calls[0][0].onPositiveClick();
    await flush();
    expect(h.app.StartInstall).toHaveBeenCalledWith(expect.objectContaining({ tool: tool.name, kind, method }));
    expect(h.message.success).not.toHaveBeenCalled(); expect(h.refreshTools).not.toHaveBeenCalled();
    expect(h.coordinator.busy.value).toBe(true);
    h.emit(h.app.StartInstall.mock.calls[0][0].sessionId, { status: 'completed' }); await pending;
    expect(h.message.success).toHaveBeenCalledOnce(); expect(h.refreshTools).toHaveBeenCalledOnce();
    expect(h.coordinator.busy.value).toBe(false); expect(h.pm.pmOperating.value).toBe('');
    expect(h.listeners.get('installSession').size).toBe(0);
  });
  it('method selector preserves selected method and holds reservation', async () => {
    const h = setup(); const pending = h.pm.installPackageManager({ name: 'pipx', installMethods: [{ name: 'pip', command: 'pip' }, { name: 'uv', command: 'uv' }] });
    const options = h.dialog.warning.mock.calls[0][0];
    options.content().props['onUpdate:value']('uv');
    expect(h.coordinator.acquire('single')).toBeNull(); options.onPositiveClick(); await flush();
    const spec = h.app.StartInstall.mock.calls[0][0]; expect(spec.method).toBe('uv');
    h.emit(spec.sessionId, { status: 'completed' }); await pending;
  });
  it('guards the slow update preflight and waits through confirmation and execution', async () => {
    let resolve; const h = setup({ GetAvailableVersions: vi.fn(() => new Promise(r => { resolve = r; })), PreviewInstall: vi.fn(async () => ({ unverified: true })) });
    const tool = { name: 'uv', toolName: 'uv' }; const pending = h.pm.updatePackageManager(tool);
    await h.pm.updatePackageManager(tool);
    expect(h.app.GetAvailableVersions).toHaveBeenCalledOnce(); expect(h.pm.pmOperating.value).toBe('uv');
    expect(h.coordinator.acquire('batch')).toBeNull(); resolve([{ version: '2' }]); await flush();
    expect(h.dialog.warning).toHaveBeenCalledOnce(); expect(h.app.StartInstall).not.toHaveBeenCalled();
    h.dialog.warning.mock.calls[0][0].onPositiveClick(); await flush();
    const spec = h.app.StartInstall.mock.calls[0][0]; expect(spec).toMatchObject({ tool: 'uv', version: '2', confirmed: true });
    expect(h.pm.pmOperating.value).toBe('uv'); h.emit(spec.sessionId, { status: 'completed' }); await pending;
    expect(h.pm.pmOperating.value).toBe('');
  });
  it.each(['onNegativeClick', 'onClose', 'onEsc', 'onMaskClick'])('releases reservation on %s and permits retry', async close => {
    const h = setup(); const tool = { name: 'pnpm', enableCommand: 'corepack enable pnpm' };
    const pending = h.pm.installPackageManager(tool); h.dialog.warning.mock.calls[0][0][close](); await pending;
    expect(h.coordinator.busy.value).toBe(false); expect(h.app.StartInstall).not.toHaveBeenCalled();
    const retry = h.pm.installPackageManager(tool); expect(h.dialog.warning).toHaveBeenCalledTimes(2);
    h.dialog.warning.mock.calls[1][0].onClose(); await retry;
  });
  it.each(['error', 'cancelled', 'needs-repair'])('releases reservation without premature success on %s', async status => {
    const h = setup(); const pending = h.pm.updatePackageManager({ name: 'uv' }); await flush();
    h.emit(h.app.StartInstall.mock.calls[0][0].sessionId, { status, error: 'broken' }); await pending;
    expect(h.message.success).not.toHaveBeenCalled(); expect(h.coordinator.busy.value).toBe(false);
    expect(h.refreshTools).toHaveBeenCalledOnce();
    expect(h.message.error).toHaveBeenCalledTimes(status === 'cancelled' ? 0 : 1);
  });
  it('rejects competing PM operations while a single install owns the coordinator', async () => {
    const h = setup(); const lease = h.coordinator.acquire('single');
    await h.pm.updatePackageManager({ name: 'uv' });
    expect(h.app.GetAvailableVersions).not.toHaveBeenCalled(); expect(h.message.warning).toHaveBeenCalledWith('install.busy'); lease.release();
  });
  it('releases a rejected start and allows a subsequent operation', async () => {
    const h = setup({ StartInstall: vi.fn(async () => { throw Error('boom'); }) });
    await h.pm.updatePackageManager({ name: 'uv' });
    expect(h.message.error).toHaveBeenCalledOnce(); expect(h.coordinator.busy.value).toBe(false);
    expect(h.pm.pmOperating.value).toBe('');
  });
});
