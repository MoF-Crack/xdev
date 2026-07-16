import { mount } from '@vue/test-utils';
import { describe, expect, it } from 'vitest';
import CustomToolsPage from '../../src/pages/CustomToolsPage.vue';

const t = (key) => key;
const tools = [{ name: 'demo', displayName: 'Demo', category: 'system', version: '1', downloadUrl: 'https://example.test/a.zip', installPath: '' }];

describe('CustomToolsPage', () => {
  it('validates HTTPS locally and submits every field once', async () => {
    const wrapper = mount(CustomToolsPage, { attachTo: document.body, props: { t, tools } });
    const trigger = wrapper.get('[data-action="open-add-tool"]');
    await trigger.trigger('click');
    await wrapper.get('[data-field="tool-name"]').setValue('new');
    await wrapper.get('[data-field="tool-display-name"]').setValue('New Tool');
    await wrapper.get('[data-field="tool-version"]').setValue('2');
    await wrapper.get('[data-field="tool-url"]').setValue('http://example.test/a.zip');
    expect(wrapper.get('[role="alert"]').text()).toContain('custom_tools.url_https');
    await wrapper.get('[data-field="tool-url"]').setValue('https://example.test/a.zip');
    await wrapper.get('[data-field="tool-shims"]').setValue('new.exe');
    await wrapper.get('[data-field="tool-env-vars"]').setValue('HOME={root}');
    await wrapper.get('[data-action="submit-tool"]').trigger('submit');
    await wrapper.get('[data-action="submit-tool"]').trigger('submit');
    expect(wrapper.emitted('add')).toHaveLength(1);
    expect(wrapper.emitted('add')[0][0]).toMatchObject({ name: 'new', displayName: 'New Tool', version: '2', downloadUrl: 'https://example.test/a.zip', shims: 'new.exe', envVars: 'HOME={root}' });
    wrapper.unmount();
  });

  it('supports Escape/focus restore and confirms removal', async () => {
    const wrapper = mount(CustomToolsPage, { attachTo: document.body, props: { t, tools } });
    const trigger = wrapper.get('[data-action="open-add-tool"]');
    await trigger.element.focus(); await trigger.trigger('click');
    expect(wrapper.get('[role="dialog"] input').element).toBe(document.activeElement);
    await wrapper.get('[role="dialog"]').trigger('keydown', { key: 'Escape' });
    expect(trigger.element).toBe(document.activeElement);
    await wrapper.get('[data-tool="demo"] [data-action="install-tool"]').trigger('click');
    await wrapper.get('[data-tool="demo"] [data-action="remove-tool"]').trigger('click');
    expect(wrapper.emitted('remove')).toBeUndefined();
    await wrapper.get('[role="dialog"] [data-action="confirm"]').trigger('click');
    expect(wrapper.emitted('install')[0]).toEqual(['demo']);
    expect(wrapper.emitted('remove')[0]).toEqual(['demo']);
    wrapper.unmount();
  });
});
