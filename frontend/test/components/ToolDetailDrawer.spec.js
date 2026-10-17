import { mount } from '@vue/test-utils';
import { describe, expect, it } from 'vitest';
import ToolDetailDrawer from '../../src/components/ToolDetailDrawer.vue';

describe('ToolDetailDrawer', () => {
  it('uses responsive width, copyable path, versions, and returns focus after close', async () => {
    const trigger = document.createElement('button'); document.body.append(trigger); trigger.focus();
    const wrapper = mount(ToolDetailDrawer, { attachTo: document.body, global: { stubs: { teleport: true } }, props: { show: true, tool: { name: 'node', displayName: 'Node', localPath: 'C:\\node' }, installedVersions: [], availableVersions: [], t: (key) => key, returnFocusTo: trigger } });
    expect(wrapper.get('.tool-detail-drawer').attributes('data-responsive-width')).toBe('min(520px, calc(100vw - 24px))');
    expect(wrapper.findComponent({ name: 'CopyableValue' }).props('value')).toBe('C:\\node');
    await wrapper.setProps({ show: false });
    expect(document.activeElement).toBe(trigger);
    wrapper.unmount(); trigger.remove();
  });

  it('clears a custom version when the tool changes or the drawer closes', async () => {
    const wrapper = mount(ToolDetailDrawer, { global: { stubs: { teleport: true } }, props: { show: true, tool: { name: 'node' }, t: (key) => key } });
    const input = wrapper.get('input');
    await input.setValue('22');
    await wrapper.setProps({ tool: { name: 'go' } });
    expect(input.element.value).toBe('');
    await input.setValue('1.24');
    await wrapper.setProps({ show: false });
    expect(input.element.value).toBe('');
  });

  it('renders dependency rows with satisfied/missing states and install path hint', () => {
    const t = (key, params) => key === 'detail.deps_install_path' ? `Required for ${params.path} install` : key === 'detail.deps_satisfied' ? 'Satisfied' : key === 'detail.deps_missing' ? 'Missing' : key === 'detail.deps_reason_not_found' ? 'Not found in PATH' : key;
    // 第七轮审计 F-42：原因行的判据是 reasonCode（Go 侧只填 ReasonCode/ReasonParams，
    // Reason 恒空），因此 fixture 必须按真实契约提供 reasonCode。
    const wrapper = mount(ToolDetailDrawer, { global: { stubs: { teleport: true } }, props: { show: true, tool: { name: 'vscode' }, deps: [
      { tool: 'node', requiredVersion: '>=18', currentVersion: '20.0.0', satisfied: true, reasonCode: '', reasonParams: {}, installPath: '' },
      { tool: 'git', requiredVersion: '>=2.0', currentVersion: '', satisfied: false, reasonCode: 'not_found', reasonParams: {}, installPath: 'E:\\XDev' },
    ], t } });
    const rows = wrapper.findAll('.dep-row');
    expect(rows).toHaveLength(2);
    expect(rows[0].attributes('data-dep')).toBe('node');
    expect(rows[0].text()).toContain('Satisfied');
    expect(rows[1].text()).toContain('Missing');
    expect(rows[1].text()).toContain('Required for E:\\XDev install');
    // F-42 守卫：原因行必须真的渲染出来（旧判据 dep.reason 恒假 → 永不渲染）。
    expect(rows[1].find('.dep-reason').exists()).toBe(true);
    expect(rows[1].text()).toContain('Not found in PATH');
    // 未提供 reasonCode 的行不得渲染原因行（防空标签）。
    expect(rows[0].find('.dep-reason').exists()).toBe(false);
  });

  it('shows launch and copy-command actions only for installed tools with matching form factor', async () => {
    const t = (key) => key;
    const base = { global: { stubs: { teleport: true } }, props: { show: true, t } };
    // installed GUI+CLI → both actions
    const gui = mount(ToolDetailDrawer, { ...base, props: { ...base.props, tool: { name: 'node', installed: true, formFactor: ['gui', 'cli'] } } });
    expect(gui.get('[data-action="launch"]').exists()).toBe(true);
    expect(gui.get('[data-action="copy-command"]').exists()).toBe(true);
    await gui.get('[data-action="launch"]').trigger('click');
    await gui.get('[data-action="copy-command"]').trigger('click');
    expect(gui.emitted('launch')).toEqual([['node']]);
    expect(gui.emitted('copy-command')).toEqual([['node']]);
    gui.unmount();
    // not installed → no actions
    const notInstalled = mount(ToolDetailDrawer, { ...base, props: { ...base.props, tool: { name: 'node', installed: false, formFactor: ['cli'] } } });
    expect(notInstalled.find('[data-action="launch"]').exists()).toBe(false);
    expect(notInstalled.find('[data-action="copy-command"]').exists()).toBe(false);
    notInstalled.unmount();
    // installed CLI-only → copy only
    const cli = mount(ToolDetailDrawer, { ...base, props: { ...base.props, tool: { name: 'go', installed: true, formFactor: ['cli'] } } });
    expect(cli.find('[data-action="launch"]').exists()).toBe(false);
    expect(cli.get('[data-action="copy-command"]').exists()).toBe(true);
    cli.unmount();
  });

  it('emits migrate-pm with the package-manager name, not the parent toolName', async () => {
    const wrapper = mount(ToolDetailDrawer, {
      global: { stubs: { teleport: true } },
      props: {
        show: true,
        t: (key) => key,
        tool: {
          name: 'pip',
          toolName: 'python',
          displayName: 'PIP',
          instances: [
            { version: '24.0', path: 'C:\\Python\\Scripts\\pip.exe', managed: false, source: 'local' },
          ],
        },
      },
    });
    await wrapper.get('[data-action="migrate-pm"]').trigger('click');
    expect(wrapper.emitted('migrate-pm')).toEqual([['pip']]);
    wrapper.unmount();
  });

  it('offers switch-to-xdev / switch-to-local on non-current package-manager instances', async () => {
    const t = (key) => key;
    const wrapper = mount(ToolDetailDrawer, {
      global: { stubs: { teleport: true } },
      props: {
        show: true, t,
        tool: {
          name: 'uv',
          category: 'package-manager',
          localPath: 'D:\\Env\\uv\\uv.exe',
          instances: [
            { version: '0.7.0', path: 'D:\\Env\\uv\\uv.exe', managed: false, source: 'local' },
            { version: '0.9.0', path: 'E:\\XDev\\install\\uv\\0.9.0', managed: true, source: 'xdev' },
          ],
        },
      },
    });
    expect(wrapper.findAll('[data-action="switch-instance"]')).toHaveLength(1);
    await wrapper.get('[data-action="switch-instance"]').trigger('click');
    expect(wrapper.emitted('switch-xdev')).toEqual([['uv', '0.9.0']]);
    wrapper.unmount();
  });

  it('puts package-manager enable/install/update in the drawer, not the catalog', async () => {
    const t = (key) => key;
    const pnpm = mount(ToolDetailDrawer, {
      global: { stubs: { teleport: true } },
      props: {
        show: true, t,
        tool: {
          name: 'pnpm', category: 'package-manager', installed: true, canInstall: true, offerSubToolInstall: true,
          enableCommand: 'corepack enable pnpm',
          installMethods: [{ name: 'corepack', command: 'corepack enable pnpm' }, { name: 'npm', command: 'npm install -g pnpm' }],
        },
      },
    });
    expect(pnpm.find('[data-action="package-manager"]').exists()).toBe(true);
    expect(pnpm.find('[data-action="package-manager"]').text()).toBe('btn.install');
    expect(pnpm.text()).not.toContain('detail.available_versions');
    expect(pnpm.text()).not.toContain('detail.install_custom');
    await pnpm.get('[data-action="package-manager"]').trigger('click');
    expect(pnpm.emitted('install-package-manager')[0][0].name).toBe('pnpm');
    pnpm.unmount();

    const pipx = mount(ToolDetailDrawer, {
      global: { stubs: { teleport: true } },
      props: { show: true, t, tool: { name: 'pipx', category: 'package-manager', installed: true, canInstall: true, installMethods: [{ name: 'pip', command: 'pip install pipx' }] } },
    });
    expect(pipx.find('[data-action="package-manager"]').exists()).toBe(false);
    pipx.unmount();

    const scoop = mount(ToolDetailDrawer, {
      global: { stubs: { teleport: true } },
      props: { show: true, t, tool: { name: 'scoop', category: 'package-manager', installed: false, canInstall: true } },
    });
    expect(scoop.get('[data-action="package-manager"]').text()).toBe('btn.install');
    scoop.unmount();
  });
});
