import { ref } from 'vue';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanupWailsMocks, installWailsMocks } from '../mocks/wails.js';
import { usePackageManagers } from '../../src/composables/usePackageManagers.js';

afterEach(cleanupWailsMocks);

function setup(appOverrides = {}) {
  const app = {
    InstallSubTool: vi.fn(async () => null),
    InstallPackageManager: vi.fn(async () => null),
    UpdatePackageManager: vi.fn(async () => null),
    MigratePackageManager: vi.fn(async () => null),
    GetAvailableVersions: vi.fn(async () => [{ version: '0.12.5' }]),
    PreviewInstall: vi.fn(async () => ({ unverified: false })),
    ...appOverrides,
  };
  installWailsMocks(app);
  const message = { success: vi.fn(), error: vi.fn() };
  const dialog = { warning: vi.fn((opts) => opts) };
  const t = (key, params) => (params ? `${key}:${JSON.stringify(params)}` : key);
  const refreshTools = vi.fn(async () => null);
  const selectedTool = ref({ displayName: 'PNPM' });
  const pm = usePackageManagers({ t, message, dialog, refreshTools, selectedTool });
  return { app, message, dialog, refreshTools, pm };
}

describe('usePackageManagers', () => {
  it('opens a method selector when more than one install method exists', () => {
    const { dialog, pm, app } = setup();
    pm.installPackageManager({
      name: 'pipx',
      displayName: 'PIPX',
      installMethods: [
        { name: 'pip', command: 'pip install pipx' },
        { name: 'uv', command: 'uv tool install pipx' },
      ],
    });
    expect(dialog.warning).toHaveBeenCalledOnce();
    expect(app.InstallSubTool).not.toHaveBeenCalled();
  });

  it('installs a single-method sub-tool without a dialog', async () => {
    const { app, pm, refreshTools } = setup();
    await pm.installPackageManager({
      name: 'pipx',
      displayName: 'PIPX',
      installMethods: [{ name: 'pip', command: 'pip install pipx' }],
    });
    expect(app.InstallSubTool).toHaveBeenCalledWith('pipx', 'pip');
    expect(refreshTools).toHaveBeenCalledOnce();
  });

  it('confirms enableCommand for yarn/pnpm', () => {
    const { dialog, app, pm } = setup();
    pm.installPackageManager({
      name: 'pnpm',
      displayName: 'PNPM',
      enableCommand: 'corepack enable pnpm',
    });
    expect(dialog.warning).toHaveBeenCalledOnce();
    expect(app.InstallSubTool).not.toHaveBeenCalled();
    dialog.warning.mock.calls[0][0].onPositiveClick();
    expect(app.InstallSubTool).toHaveBeenCalledWith('pnpm', '');
  });

  it('installs an independent package manager via InstallPackageManager', async () => {
    const { app, dialog, pm } = setup();
    pm.installPackageManager({ name: 'scoop', displayName: 'Scoop', installCommand: 'irm get.scoop.sh | iex' });
    expect(dialog.warning).toHaveBeenCalledOnce();
    await dialog.warning.mock.calls[0][0].onPositiveClick();
    expect(app.InstallPackageManager).toHaveBeenCalledWith('scoop');
  });

  it('confirms sniff-discovered updates before calling UpdatePackageManager', async () => {
    const { app, dialog, pm } = setup({
      PreviewInstall: vi.fn(async () => ({ unverified: true })),
    });
    await pm.updatePackageManager({ name: 'uv', toolName: 'uv', displayName: 'UV' });
    expect(dialog.warning).toHaveBeenCalledOnce();
    expect(app.UpdatePackageManager).not.toHaveBeenCalled();
    await dialog.warning.mock.calls[0][0].onPositiveClick();
    expect(app.UpdatePackageManager).toHaveBeenCalledWith('uv');
  });
});
