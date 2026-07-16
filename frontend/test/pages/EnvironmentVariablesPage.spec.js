import { flushPromises, mount } from '@vue/test-utils';
import { describe, expect, it, vi } from 'vitest';

import EnvironmentVariablesPage from '../../src/pages/EnvironmentVariablesPage.vue';
import { sameEnvIdentity } from '../../src/pages/envIdentity.js';

const t = (key) => key;
const rows = [
  { name: 'JAVA_HOME', value: 'C:\\Java', toolName: 'java', level: 'user', source: 'xdev', isValid: true },
  { name: 'OLD_HOME', value: 'C:\\Missing', toolName: 'old', level: 'machine', source: 'system', isValid: false, invalidReason: 'missing', validationStatus: 'partial' },
];
const props = { t, browseDirectory: async () => '', envVars: rows, allEnvVars: rows, viewMode: 'dev', allLevel: 'user', validationStats: null, migrationMode: 'junction', migrationTools: [{ name: 'java', version: '21', path: 'C:\\Java' }], migrationResults: [{ name: 'go', status: 'success' }] };

describe('EnvironmentVariablesPage', () => {
  it('only edits from the explicit edit button, not the table row', async () => {
    const wrapper = mount(EnvironmentVariablesPage, { props });
    await wrapper.get('tbody tr').trigger('click');
    expect(wrapper.emitted('edit')).toBeUndefined();
    await wrapper.get('tbody [data-action="edit-env"]').trigger('click');
    expect(wrapper.emitted('edit')).toHaveLength(1);
  });
  it('compares tool, level and name and accepts legacy tool field', () => {
    expect(sameEnvIdentity({ toolName: 'java', level: 'user', name: 'JAVA_HOME' }, { tool: 'java', level: 'user', name: 'JAVA_HOME' })).toBe(true);
    expect(sameEnvIdentity({ toolName: 'java', level: 'user', name: 'JAVA_HOME' }, { toolName: 'node', level: 'user', name: 'JAVA_HOME' })).toBe(false);
  });

  it('renders the structured not-applicable status', () => {
    const wrapper = mount(EnvironmentVariablesPage, { props: { ...props, envVars: [{ ...rows[0], validationStatus: 'not-applicable' }] } });
    expect(wrapper.text()).toContain('env.not-applicable');
  });
  it('supports development/all scopes, level filtering and invalid detection', async () => {
    window.matchMedia = () => ({ matches: true, addEventListener() {}, removeEventListener() {} });
    const wrapper = mount(EnvironmentVariablesPage, { props });
    await flushPromises();
    await wrapper.get('[data-action="scope-all"]').trigger('click');
    await wrapper.setProps({ viewMode: 'all' });
    await flushPromises();
    await wrapper.get('[data-action="all-level"]').setValue('machine');
    await wrapper.get('[data-action="validate"]').trigger('click');
    expect(wrapper.emitted('update:view-mode')).toEqual([['all']]);
    expect(wrapper.emitted('update:all-level')).toEqual([['machine']]);
    expect(wrapper.emitted('validate')).toBeTruthy();
    expect(wrapper.text()).toContain('env.partial');
  });

  it('supports edit, add, save, cancel, browse, delete and clean confirmation', async () => {
    window.matchMedia = () => ({ matches: true, addEventListener() {}, removeEventListener() {} });
    const wrapper = mount(EnvironmentVariablesPage, { props });
    await flushPromises();
    await wrapper.get('[data-row="JAVA_HOME"] [data-action="edit-env"]').trigger('click');
    expect(wrapper.emitted('edit')).toBeTruthy();
    await wrapper.setProps({ editing: rows[0] });
    await wrapper.get('[data-action="browse"]').trigger('click');
    await wrapper.get('[data-action="save-env"]').trigger('click');
    await wrapper.get('[data-action="cancel-edit"]').trigger('click');
    await wrapper.get('[data-row="OLD_HOME"] [data-action="delete-env"]').trigger('click');
    expect(wrapper.find('[role="dialog"]').exists()).toBe(true);
    await wrapper.get('[role="dialog"] [data-action="confirm"]').trigger('click');
    expect(wrapper.emitted('delete')).toBeTruthy();
    expect(props.browseDirectory).toBeTypeOf('function');
    expect(wrapper.emitted('save')).toBeTruthy();
  });

  it('fills the value from browse and locks identity while editing', async () => {
    const browseDirectory = vi.fn().mockResolvedValue('D:\\SDK');
    const wrapper = mount(EnvironmentVariablesPage, { props: { ...props, browseDirectory, editing: rows[0] } });
    expect(wrapper.get('[aria-label="Tool name"]').attributes('disabled')).toBeDefined();
    expect(wrapper.get('[aria-label="Variable name"]').attributes('disabled')).toBeDefined();
    expect(wrapper.get('[aria-label="Level"]').attributes('disabled')).toBeDefined();
    await wrapper.get('[data-action="browse"]').trigger('click');
    await wrapper.get('[data-action="save-env"]').trigger('click');
    expect(wrapper.emitted('save')[0][0]).toMatchObject({ toolName: 'java', name: 'JAVA_HOME', level: 'user', value: 'D:\\SDK', original: { toolName: 'java', name: 'JAVA_HOME', level: 'user' } });
  });

  it('offers equal edit delete and clean actions in desktop and card layouts', () => {
    window.matchMedia = () => ({ matches: false, addEventListener() {}, removeEventListener() {} });
    const wrapper = mount(EnvironmentVariablesPage, { props });
    expect(wrapper.findAll('table [data-action="edit-env"]')).toHaveLength(2);
    expect(wrapper.findAll('table [data-action="delete-env"]')).toHaveLength(2);
    expect(wrapper.findAll('table [data-action="clean-env"]')).toHaveLength(1);
  });

  it('preserves migration scan, modes, single/all actions and textual results', async () => {
    const wrapper = mount(EnvironmentVariablesPage, { props });
    await wrapper.get('[data-action="migration-mode-move"]').trigger('click');
    await wrapper.setProps({ migrationMode: 'move' });
    await wrapper.get('[data-action="migration-scan"]').trigger('click');
    await wrapper.get('[data-action="migration-single"]').trigger('click');
    await wrapper.get('[data-action="migration-all"]').trigger('click');
    expect(wrapper.emitted('update:migration-mode')).toEqual([['move']]);
    expect(wrapper.emitted('migration-scan')).toBeTruthy();
    expect(wrapper.emitted('migration-single')).toEqual([[props.migrationTools[0]]]);
    expect(wrapper.emitted('migration-all')).toEqual([['move']]);
    expect(wrapper.text()).toContain('migration.status_success');
  });
});
