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
describe('package manager trust plan through sessions', () => {
  it.each([
    [{ name: 'local/pipx', installMethods: [{ name: 'test', command: 'cached command' }] }, 'sub-tool', 'test'],
    [{ name: 'third-party/demo', installCommand: 'cached command' }, 'package-manager', ''],
    [{ name: 'local/pnpm', enableCommand: 'cached command' }, 'sub-tool', ''],
  ])('requires positive approval of the current full definition for $0.name', async (tool, kind, method) => {
    const plan = { source: 'local', path: 'package-managers.yaml', digest: 'a'.repeat(64), summary: 'source: local\nfull YAML fixture command' };
    const legacy = { InstallSubToolTrusted: vi.fn(), InstallPackageManagerTrusted: vi.fn(), InstallSubTool: vi.fn(), InstallPackageManager: vi.fn() };
    const h = setup({ PlanPackageManager: vi.fn(async () => plan), ...legacy });
    const pending = h.pm.installPackageManager(tool); await flush();
    expect(h.app.PlanPackageManager).toHaveBeenCalledWith(tool.name);
    expect(h.app.StartInstall).not.toHaveBeenCalled();
    expect(h.coordinator.acquire('single')).toBeNull();
    const confirm = h.dialog.warning.mock.calls[0][0];
    expect(confirm.content().children).toBe(plan.summary);
    confirm.onPositiveClick(); await flush();
    expect(h.app.StartInstall).toHaveBeenCalledWith(expect.objectContaining({ tool: tool.name, kind, method, pmDigest: plan.digest }));
    for (const fn of Object.values(legacy)) expect(fn).not.toHaveBeenCalled();
    expect(h.message.success).not.toHaveBeenCalled();
    const spec = h.app.StartInstall.mock.calls[0][0];
    h.emit(spec.sessionId, { status: 'completed' }); await pending;
    expect(h.refreshTools).toHaveBeenCalledOnce(); expect(h.coordinator.busy.value).toBe(false);
  });
  it.each(['onNegativeClick', 'onClose', 'onEsc', 'onMaskClick'])('dismisses trust confirmation through %s without execution and replans retry', async close => {
    let digest = 'first';
    const h = setup({ PlanPackageManager: vi.fn(async () => ({ digest, summary: digest })) });
    const tool = { name: 'local/demo' };
    const pending = h.pm.installPackageManager(tool); await flush();
    h.dialog.warning.mock.calls[0][0][close](); await pending;
    expect(h.app.StartInstall).not.toHaveBeenCalled(); expect(h.coordinator.busy.value).toBe(false);
    digest = 'changed'; const retry = h.pm.installPackageManager(tool); await flush();
    expect(h.app.PlanPackageManager).toHaveBeenCalledTimes(2);
    expect(h.dialog.warning.mock.calls[1][0].content().children).toBe('changed');
    h.dialog.warning.mock.calls[1][0].onPositiveClick(); await flush();
    const spec = h.app.StartInstall.mock.calls[0][0]; expect(spec.pmDigest).toBe('changed');
    h.emit(spec.sessionId, { status: 'error', error: 'definition changed after approval' }); await retry;
    expect(h.message.success).not.toHaveBeenCalled(); expect(h.message.error).toHaveBeenCalledOnce();
  });
  it('keeps a selected external method through its separate current-definition confirmation', async () => {
    const h = setup({ PlanPackageManager: vi.fn(async () => ({ digest: 'current', summary: 'all methods YAML' })) });
    const pending = h.pm.installPackageManager({ name: 'local/pipx', installMethods: [{ name: 'pip', command: 'pip' }, { name: 'uv', command: 'uv' }] });
    const selector = h.dialog.warning.mock.calls[0][0]; selector.content().props['onUpdate:value']('uv'); selector.onPositiveClick(); await flush();
    expect(h.app.StartInstall).not.toHaveBeenCalled(); expect(h.coordinator.busy.value).toBe(true);
    h.dialog.warning.mock.calls[1][0].onPositiveClick(); await flush();
    const spec = h.app.StartInstall.mock.calls[0][0]; expect(spec).toMatchObject({ method: 'uv', pmDigest: 'current' });
    h.emit(spec.sessionId, { status: 'completed' }); await pending;
  });
  it.each([null, {}, { digest: 'x' }])('fails closed for incomplete trust plan %j', async plan => {
    const h = setup({ PlanPackageManager: vi.fn(async () => plan) });
    await h.pm.installPackageManager({ name: 'local/demo' });
    expect(h.app.StartInstall).not.toHaveBeenCalled(); expect(h.message.error).toHaveBeenCalledOnce();
    expect(h.coordinator.busy.value).toBe(false);
  });
  it('releases ownership on a rejected plan fetch', async () => {
    const h = setup({ PlanPackageManager: vi.fn(async () => { throw Error('broken YAML'); }) });
    await h.pm.installPackageManager({ name: 'third-party/demo' });
    expect(h.app.StartInstall).not.toHaveBeenCalled(); expect(h.message.error).toHaveBeenCalledOnce();
    expect(h.coordinator.busy.value).toBe(false);
  });
});

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
