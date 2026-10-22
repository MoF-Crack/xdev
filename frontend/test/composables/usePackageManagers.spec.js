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

  // ---- 票 36 (Batch 5 / P1-38 G-06)：PM 更新重入守卫 ----

  // 红（修复前）：守卫只在 updatePm 内、即两次 await 之后才置位，慢 IPC 期间
  // 连点两次会并行跑两条链 → 两个确认框 + 两次 UpdatePackageManager。
  it('G-06: holds the guard from entry so a double click during slow IPC opens one dialog', async () => {
    let resolveVersions;
    const { app, dialog, pm } = setup({
      GetAvailableVersions: vi.fn(() => new Promise((resolve) => { resolveVersions = resolve; })),
      PreviewInstall: vi.fn(async () => ({ unverified: true })),
    });
    const tool = { name: 'uv', toolName: 'uv', displayName: 'UV' };

    const first = pm.updatePackageManager(tool);
    const second = pm.updatePackageManager(tool); // 慢 IPC 期间的第二次点击
    expect(pm.pmOperating.value).toBe('uv'); // catalogName，入口即置位
    expect(app.GetAvailableVersions).toHaveBeenCalledOnce();

    resolveVersions([{ version: '0.12.5' }]);
    await Promise.all([first, second]);

    expect(dialog.warning).toHaveBeenCalledOnce();
    expect(app.UpdatePackageManager).not.toHaveBeenCalled();
  });

  it('G-06: keeps the guard while the unverified dialog is open and hands it to updatePm', async () => {
    const { app, dialog, pm } = setup({ PreviewInstall: vi.fn(async () => ({ unverified: true })) });
    const tool = { name: 'uv', toolName: 'uv', displayName: 'UV' };

    await pm.updatePackageManager(tool);
    expect(dialog.warning).toHaveBeenCalledOnce();
    expect(pm.pmOperating.value).toBe('uv'); // 弹窗期间守卫持续有效

    // 弹窗期间再点 Update：既不弹第二个框，也不发起 IPC。
    await pm.updatePackageManager(tool);
    expect(dialog.warning).toHaveBeenCalledOnce();
    expect(app.UpdatePackageManager).not.toHaveBeenCalled();

    // 不开 closable（连带 ESC/遮罩关闭）：这些路径都不经过 onNegativeClick，
    // 会让 pmOperating 永不复位、Update 按钮永久锁死。
    const options = dialog.warning.mock.calls[0][0];
    expect(options.closable).toBe(false);
    expect(options.closeOnEsc).toBe(false);
    expect(options.maskClosable).toBe(false);

    await options.onPositiveClick(); // 复位权 → updatePm → finally 复位
    expect(app.UpdatePackageManager).toHaveBeenCalledWith('uv');
    expect(pm.pmOperating.value).toBe('');
  });

  it('G-06: cancel releases the guard so the Update button is not locked forever', async () => {
    const { dialog, pm } = setup({ PreviewInstall: vi.fn(async () => ({ unverified: true })) });
    const tool = { name: 'uv', toolName: 'uv', displayName: 'UV' };

    await pm.updatePackageManager(tool);
    dialog.warning.mock.calls[0][0].onNegativeClick();
    expect(pm.pmOperating.value).toBe('');

    // 释放后可重新发起（第二次弹框说明守卫确实解除了）。
    await pm.updatePackageManager(tool);
    expect(dialog.warning).toHaveBeenCalledTimes(2);
  });

  it('G-06: the direct path releases the guard in updatePm finally', async () => {
    const { app, dialog, pm } = setup(); // unverified=false → 直连路径
    await pm.updatePackageManager({ name: 'uv', toolName: 'uv', displayName: 'UV' });
    expect(dialog.warning).not.toHaveBeenCalled();
    expect(app.UpdatePackageManager).toHaveBeenCalledWith('uv');
    expect(pm.pmOperating.value).toBe('');
  });

  it('G-06: releases the guard when the update fails', async () => {
    const { message, pm } = setup({
      UpdatePackageManager: vi.fn(async () => { throw new Error('boom'); }),
    });
    await pm.updatePackageManager({ name: 'uv', toolName: 'uv', displayName: 'UV' });
    expect(message.error).toHaveBeenCalledOnce();
    expect(pm.pmOperating.value).toBe('');
  });
});
