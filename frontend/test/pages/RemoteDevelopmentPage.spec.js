import { mount } from '@vue/test-utils';
import { describe, expect, it } from 'vitest';
import RemoteDevelopmentPage from '../../src/pages/RemoteDevelopmentPage.vue';

const t = (key) => key;
const instances = [{ id: 'one', adapter: 'docker', status: 'running', addresses: ['C:\\work'] }, { id: 'two', adapter: 'ssh', status: 'stopped', addresses: [] }];

describe('RemoteDevelopmentPage', () => {
  it('creates, copies address, executes, and confirms destroy with instance adapter', async () => {
    const wrapper = mount(RemoteDevelopmentPage, { props: { t, instances } });
    await wrapper.get('[data-field="remote-image"]').setValue('ubuntu:22.04');
    await wrapper.get('[data-action="create-remote"]').trigger('click');
    await wrapper.get('[data-instance="one"] [data-action="copy-address"]').trigger('click');
    await wrapper.get('[data-instance="one"] [data-field="remote-command"]').setValue('pwd');
    await wrapper.get('[data-instance="one"] [data-action="exec-remote"]').trigger('click');
    await wrapper.get('[data-instance="one"] [data-action="destroy-remote"]').trigger('click');
    expect(wrapper.emitted('destroy')).toBeUndefined();
    await wrapper.get('[role="dialog"] [data-action="confirm"]').trigger('click');
    expect(wrapper.emitted('create')[0][0]).toMatchObject({ adapter: 'docker', image: 'ubuntu:22.04' });
    expect(wrapper.emitted('copy-address')[0]).toEqual(['C:\\work']);
    expect(wrapper.emitted('exec')[0]).toEqual(['docker', 'one', 'pwd']);
    expect(wrapper.emitted('destroy')[0]).toEqual(['docker', 'one']);
  });

  it('labels every remote form field', () => {
    const wrapper = mount(RemoteDevelopmentPage, { props: { t, instances } });
    expect(wrapper.findAll('input,select').every((field) => field.attributes('aria-label') || field.element.labels?.length)).toBe(true);
  });

  it('isolates pending and output by instance and makes paths copyable', () => {
    const wrapper = mount(RemoteDevelopmentPage, { props: { t, instances, operating: { one: { exec: true } }, outputs: { two: 'done' } } });
    expect(wrapper.get('[data-instance="one"] [data-action="exec-remote"]').attributes('disabled')).toBeDefined();
    expect(wrapper.get('[data-instance="two"] [data-action="destroy-remote"]').attributes('disabled')).toBeUndefined();
    expect(wrapper.findAllComponents({ name: 'CopyableValue' }).map((c) => c.props('value'))).toEqual(['C:\\work', 'done']);
  });
});
