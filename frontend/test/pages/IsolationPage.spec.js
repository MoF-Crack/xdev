import { mount } from '@vue/test-utils';
import { describe, expect, it } from 'vitest';
import IsolationPage from '../../src/pages/IsolationPage.vue';

const t = (key) => key;

describe('IsolationPage', () => {
  it('guards operations and confirms teardown', async () => {
    const wrapper = mount(IsolationPage, { props: { t, status: { dockerAvailable: true, activeMode: 'container' } } });
    await wrapper.get('[data-action="setup"]').trigger('click');
    await wrapper.get('[data-field="isolate-command"]').setValue('node --version');
    await wrapper.get('[data-action="exec"]').trigger('click');
    await wrapper.get('[data-action="teardown"]').trigger('click');
    expect(wrapper.emitted('teardown')).toBeUndefined();
    await wrapper.get('[role="dialog"] [data-action="confirm"]').trigger('click');
    expect(wrapper.emitted('setup')[0]).toEqual(['container']);
    expect(wrapper.emitted('exec')[0]).toEqual(['node --version']);
    expect(wrapper.emitted('teardown')).toHaveLength(1);
  });

  it('copies command output and disables controls while operating', () => {
    const wrapper = mount(IsolationPage, { props: { t, output: 'v22', operating: true } });
    expect(wrapper.getComponent({ name: 'CopyableValue' }).props('value')).toBe('v22');
    expect(['setup', 'exec', 'teardown'].every((action) => wrapper.get(`[data-action="${action}"]`).attributes('disabled') !== undefined)).toBe(true);
  });
});
