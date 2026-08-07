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
    const t = (key, params) => key === 'detail.deps_install_path' ? `Required for ${params.path} install` : key === 'detail.deps_satisfied' ? 'Satisfied' : key === 'detail.deps_missing' ? 'Missing' : key;
    const wrapper = mount(ToolDetailDrawer, { global: { stubs: { teleport: true } }, props: { show: true, tool: { name: 'vscode' }, deps: [
      { tool: 'node', requiredVersion: '>=18', currentVersion: '20.0.0', satisfied: true, reason: '', installPath: '' },
      { tool: 'git', requiredVersion: '>=2.0', currentVersion: '', satisfied: false, reason: 'PATH 中未找到 git', installPath: 'E:\\XDev' },
    ], t } });
    const rows = wrapper.findAll('.dep-row');
    expect(rows).toHaveLength(2);
    expect(rows[0].attributes('data-dep')).toBe('node');
    expect(rows[0].text()).toContain('Satisfied');
    expect(rows[1].text()).toContain('Missing');
    expect(rows[1].text()).toContain('Required for E:\\XDev install');
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
});
