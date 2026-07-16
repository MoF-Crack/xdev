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
});
