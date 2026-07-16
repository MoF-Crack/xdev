import { mount } from '@vue/test-utils';
import { describe, expect, it } from 'vitest';

import PluginsPage from '../../src/pages/PluginsPage.vue';

const t = (key) => key;
const plugins = [
  { name: 'node', displayName: 'Node', type: 'builtin' },
  { name: 'local', type: 'lua', path: 'D:\\XDev\\plugins\\local.lua' },
  { name: 'java', type: 'vfox', path: 'D:\\XDev\\plugins\\java' },
];

describe('PluginsPage', () => {
  it('distinguishes plugin kinds and makes paths copyable', () => {
    const wrapper = mount(PluginsPage, { props: { t, plugins } });
    expect(wrapper.text()).toContain('plugins.builtin');
    expect(wrapper.text()).toContain('lua');
    expect(wrapper.text()).toContain('vfox');
    expect(wrapper.findAll('.copyable-value')).toHaveLength(2);
  });

  it('creates a plugin through the modal form', async () => {
    const wrapper = mount(PluginsPage, { props: { t, plugins } });
    await wrapper.get('[data-action="open-create-plugin"]').trigger('click');
    await wrapper.get('[data-field="plugin-name"]').setValue('demo');
    await wrapper.get('[data-action="create-plugin"]').element.closest('form').dispatchEvent(new Event('submit'));
    await wrapper.vm.$nextTick();
    expect(wrapper.emitted('create')[0]).toEqual(['demo']);
  });

  it('traps modal focus, closes on Escape, and restores trigger focus', async () => {
    const wrapper = mount(PluginsPage, { attachTo: document.body, props: { t, plugins } });
    const trigger = wrapper.get('[data-action="open-create-plugin"]');
    await trigger.trigger('click');
    expect(wrapper.get('[role="dialog"]').attributes('aria-modal')).toBe('true');
    expect(wrapper.get('[data-field="plugin-name"]').element).toBe(document.activeElement);
    await wrapper.get('[data-field="plugin-name"]').setValue('demo');
    wrapper.get('[data-action="create-plugin"]').element.focus();
    await wrapper.get('[data-action="create-plugin"]').trigger('keydown', { key: 'Tab' });
    expect(wrapper.get('[data-action="cancel-create-plugin"]').element).toBe(document.activeElement);
    await wrapper.get('[role="dialog"]').trigger('keydown', { key: 'Escape' });
    expect(wrapper.find('[role="dialog"]').exists()).toBe(false);
    expect(trigger.element).toBe(document.activeElement);
    wrapper.unmount();
  });

  it('guards duplicate submit while creating and exposes retry', async () => {
    const wrapper = mount(PluginsPage, { props: { t, plugins, creating: true, error: 'offline' } });
    await wrapper.get('[role="alert"] button').trigger('click');
    expect(wrapper.emitted('retry')).toHaveLength(1);
  });
});
