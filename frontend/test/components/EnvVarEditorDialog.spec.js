import { flushPromises, mount } from '@vue/test-utils';
import { afterEach, describe, expect, it, vi } from 'vitest';

import EnvVarEditorDialog from '../../src/components/EnvVarEditorDialog.vue';

const t = (key) => key;

afterEach(() => { document.body.innerHTML = ''; });

function nameInput(wrapper) {
  return wrapper.get('input:not(.path-input)');
}
function pathInputs(wrapper) {
  return wrapper.findAll('.path-input');
}
function addPathButton(wrapper) {
  return wrapper.findAll('button').find((button) => button.text().includes('env.add_path'));
}
function browseButton(wrapper) {
  return wrapper.findAll('button').find((button) => button.text().trim() === 'btn.browse');
}
function saveButton(wrapper) {
  return wrapper.get('.xdev-btn--primary');
}

describe('EnvVarEditorDialog', () => {
  it('opens and focuses the name input (TC-C1)', async () => {
    const wrapper = mount(EnvVarEditorDialog, { attachTo: document.body, props: { show: false, t } });
    await wrapper.setProps({ show: true });
    await flushPromises();
    expect(wrapper.get('[role="dialog"]').exists()).toBe(true);
    expect(document.activeElement).toBe(nameInput(wrapper).element);
  });

  it('locks the name and hides the level selector when editing (TC-C2)', () => {
    const wrapper = mount(EnvVarEditorDialog, { attachTo: document.body, props: { show: true, editing: { toolName: 'java', name: 'JAVA_HOME', level: 'user', value: 'C:\\Java' }, t } });
    expect(nameInput(wrapper).attributes('disabled')).toBeDefined();
    expect(wrapper.find('select').exists()).toBe(false);
    expect(wrapper.get('h2').text()).toBe('env.edit_title');
  });

  it('offers an editable name and user/machine level in new mode (TC-C3)', () => {
    const wrapper = mount(EnvVarEditorDialog, { attachTo: document.body, props: { show: true, t } });
    expect(nameInput(wrapper).attributes('disabled')).toBeUndefined();
    const select = wrapper.get('select');
    expect(select.findAll('option').map((option) => option.element.value)).toEqual(['user', 'machine']);
  });

  it('fills the first empty path then appends on browse (TC-C4)', async () => {
    const browseDirectory = vi.fn().mockResolvedValue('D:\\SDK');
    const wrapper = mount(EnvVarEditorDialog, { attachTo: document.body, props: { show: true, t, browseDirectory } });
    await flushPromises();
    await browseButton(wrapper).trigger('click');
    await flushPromises();
    expect(pathInputs(wrapper).map((input) => input.element.value)).toEqual(['D:\\SDK']);
    await browseButton(wrapper).trigger('click');
    await flushPromises();
    expect(pathInputs(wrapper).map((input) => input.element.value)).toEqual(['D:\\SDK', 'D:\\SDK']);
  });

  it('adds and removes paths and auto-restores an empty one (TC-C5)', async () => {
    const wrapper = mount(EnvVarEditorDialog, { attachTo: document.body, props: { show: true, t } });
    await addPathButton(wrapper).trigger('click');
    expect(pathInputs(wrapper)).toHaveLength(2);
    await wrapper.get('.path-remove').trigger('click');
    expect(pathInputs(wrapper)).toHaveLength(1);
    await wrapper.get('.path-remove').trigger('click');
    expect(pathInputs(wrapper)).toHaveLength(1);
    expect(pathInputs(wrapper)[0].element.value).toBe('');
  });

  it('emits a new-env save payload with a ;-joined value (TC-C6)', async () => {
    const wrapper = mount(EnvVarEditorDialog, { attachTo: document.body, props: { show: true, t } });
    await nameInput(wrapper).setValue('PATH');
    await pathInputs(wrapper)[0].setValue('a');
    await addPathButton(wrapper).trigger('click');
    await pathInputs(wrapper)[1].setValue('b');
    await wrapper.get('select').setValue('machine');
    await saveButton(wrapper).trigger('click');
    expect(wrapper.emitted('save')[0][0]).toEqual({ toolName: '', name: 'PATH', level: 'machine', value: 'a;b', original: null });
    expect(wrapper.emitted('update:show')).toEqual([[false]]);
  });

  it('emits an edit save payload with original identity (TC-C7)', async () => {
    const editing = { toolName: 'java', name: 'JAVA_HOME', level: 'user', value: 'C:\\Java' };
    const wrapper = mount(EnvVarEditorDialog, { attachTo: document.body, props: { show: false, editing, t } });
    await wrapper.setProps({ show: true });
    await flushPromises();
    await saveButton(wrapper).trigger('click');
    expect(wrapper.emitted('save')[0][0]).toEqual({ toolName: 'java', name: 'JAVA_HOME', level: 'user', value: 'C:\\Java', original: { toolName: 'java', name: 'JAVA_HOME', level: 'user' } });
    expect(wrapper.emitted('update:show')).toEqual([[false]]);
  });

  it('shows the machine-level hint (TC-C8)', async () => {
    const wrapper = mount(EnvVarEditorDialog, { attachTo: document.body, props: { show: true, t } });
    await wrapper.get('select').setValue('machine');
    expect(wrapper.get('.admin-warning').text()).toBe('env.machine_hint');
  });

  it('closes on Escape without saving (TC-C9)', async () => {
    const wrapper = mount(EnvVarEditorDialog, { attachTo: document.body, props: { show: true, t } });
    await wrapper.get('[role="dialog"]').trigger('keydown', { key: 'Escape' });
    expect(wrapper.emitted('update:show')).toEqual([[false]]);
    expect(wrapper.emitted('save')).toBeUndefined();
  });

  it('marks missing paths as invalid (TC-C10)', async () => {
    const editing = { toolName: 'java', name: 'JAVA_HOME', level: 'user', value: 'C:\\Missing', pathEntries: [{ path: 'C:\\Missing', exists: false }] };
    const wrapper = mount(EnvVarEditorDialog, { attachTo: document.body, props: { show: false, editing, t } });
    await wrapper.setProps({ show: true });
    await flushPromises();
    const entry = wrapper.get('.path-entry');
    expect(entry.classes()).toContain('path-entry--invalid');
    expect(entry.get('.path-error').text()).toBe('env.path_missing');
  });

  it('disables save when the name is empty (TC-C11)', () => {
    const wrapper = mount(EnvVarEditorDialog, { attachTo: document.body, props: { show: true, t } });
    expect(saveButton(wrapper).attributes('disabled')).toBeDefined();
  });
});
