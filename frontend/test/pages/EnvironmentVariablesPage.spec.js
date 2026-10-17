import { flushPromises, mount } from '@vue/test-utils';
import { describe, expect, it } from 'vitest';

import EnvironmentVariablesPage from '../../src/pages/EnvironmentVariablesPage.vue';
import { sameEnvIdentity } from '../../src/pages/envIdentity.js';

const t = (key) => key;
const rows = [
  { name: 'JAVA_HOME', value: 'C:\\Java', toolName: 'java', level: 'user', source: 'xdev', isValid: true },
  { name: 'OLD_HOME', value: 'C:\\Missing', toolName: 'old', level: 'machine', source: 'system', isValid: false, invalidReason: 'missing', validationStatus: 'partial' },
];
const props = { t, allEnvVars: rows, allLevel: 'user', validationStats: null, migrationMode: 'junction', migrationTools: [{ name: 'java', version: '21', path: 'C:\\Java' }], migrationResults: [{ name: 'go', status: 'success' }] };

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

  it('renders the structured not-applicable status on the status dot', () => {
    const wrapper = mount(EnvironmentVariablesPage, { props: { ...props, allEnvVars: [{ ...rows[0], validationStatus: 'not-applicable' }] } });
    const dot = wrapper.find('.status-dot');
    expect(dot.exists()).toBe(true);
    expect(dot.attributes('title')).toContain('env.not-applicable');
    expect(dot.attributes('aria-label')).toContain('env.not-applicable');
  });

  it('supports level filtering, validation and partial status', async () => {
    const wrapper = mount(EnvironmentVariablesPage, { props });
    await wrapper.get('[data-action="all-level"]').setValue('machine');
    await wrapper.get('[data-action="validate"]').trigger('click');
    expect(wrapper.emitted('update:all-level')).toEqual([['machine']]);
    expect(wrapper.emitted('validate')).toBeTruthy();
    const dot = wrapper.find('[data-row="OLD_HOME"] .status-dot');
    expect(dot.attributes('title')).toContain('env.partial');
    expect(dot.classes()).toContain('status-dot--warning');
  });

  it('emits edit, new, delete, clean, and validate events from page actions', async () => {
    const wrapper = mount(EnvironmentVariablesPage, { props });
    await wrapper.get('[data-action="new-env"]').trigger('click');
    await wrapper.get('[data-action="validate"]').trigger('click');
    await wrapper.get('tbody [data-action="edit-env"]').trigger('click');
    await wrapper.get('tbody [data-row="OLD_HOME"] [data-action="delete-env"]').trigger('click');
    await flushPromises();
    expect(wrapper.find('[role="dialog"]').exists()).toBe(true);
    await wrapper.get('[role="dialog"] [data-action="confirm"]').trigger('click');
    await wrapper.get('tbody [data-row="OLD_HOME"] [data-action="clean-env"]').trigger('click');
    await flushPromises();
    await wrapper.get('[role="dialog"] [data-action="confirm"]').trigger('click');

    expect(wrapper.emitted('new')).toHaveLength(1);
    expect(wrapper.emitted('validate')).toHaveLength(1);
    expect(wrapper.emitted('edit')).toEqual([[rows[0]]]);
    expect(wrapper.emitted('delete')).toEqual([[rows[1]]]);
    expect(wrapper.emitted('clean')).toEqual([[rows[1]]]);
  });

  it('offers equal edit delete and clean actions in desktop and card layouts', () => {
    window.matchMedia = () => ({ matches: false, addEventListener() {}, removeEventListener() {} });
    const wrapper = mount(EnvironmentVariablesPage, { props });
    expect(wrapper.findAll('table [data-action="edit-env"]')).toHaveLength(2);
    expect(wrapper.findAll('table [data-action="delete-env"]')).toHaveLength(2);
    expect(wrapper.findAll('table [data-action="clean-env"]')).toHaveLength(1);
  });

  it('preserves migration mode display, scan, single/all actions and textual results', async () => {
    const wrapper = mount(EnvironmentVariablesPage, { props });
    // Mode comes from the persisted config (settings page) — shown as text, no local toggle.
    expect(wrapper.get('[data-action="migration-mode"]').text()).toContain('migration.junction');
    await wrapper.setProps({ migrationMode: 'move' });
    expect(wrapper.get('[data-action="migration-mode"]').text()).toContain('migration.move');
    await wrapper.get('[data-action="migration-scan"]').trigger('click');
    await wrapper.get('[data-action="migration-single"]').trigger('click');
    await wrapper.get('[data-action="migration-all"]').trigger('click');
    expect(wrapper.emitted('update:migration-mode')).toBeUndefined();
    expect(wrapper.emitted('migration-scan')).toBeTruthy();
    expect(wrapper.emitted('migration-single')).toEqual([[props.migrationTools[0]]]);
    expect(wrapper.emitted('migration-all')).toHaveLength(1);
    expect(wrapper.text()).toContain('migration.status_success');
  });

  // 第七轮审计 F-40：迁移进行中时三个按钮必须禁用，且点击不再发出事件。
  //
  // 缺陷现场：AppContent 的 migratingAll/migratingSingle 只被写、从不被读
  // （模板零引用），按钮唯一约束是 `:disabled="!migrationTools.length"`，
  // 不随进行中状态禁用。迁移是长耗时复制/移动，快速双击会让同一工具被两个
  // 并发迁移流程处理（Go 侧也无工具锁）。
  it('disables every migration action while a migration is in flight (F-40)', async () => {
    const wrapper = mount(EnvironmentVariablesPage, { props });
    // 空闲：按钮可用。
    expect(wrapper.get('[data-action="migration-scan"]').attributes('disabled')).toBeUndefined();
    expect(wrapper.get('[data-action="migration-single"]').attributes('disabled')).toBeUndefined();
    expect(wrapper.get('[data-action="migration-all"]').attributes('disabled')).toBeUndefined();

    await wrapper.setProps({ migrationBusy: true });
    expect(wrapper.get('[data-action="migration-scan"]').attributes('disabled')).toBeDefined();
    expect(wrapper.get('[data-action="migration-single"]').attributes('disabled')).toBeDefined();
    expect(wrapper.get('[data-action="migration-all"]').attributes('disabled')).toBeDefined();

    // 禁用状态下点击不得再发出任何迁移事件（Vue 的 disabled 阻止原生 click）。
    await wrapper.get('[data-action="migration-single"]').trigger('click');
    await wrapper.get('[data-action="migration-all"]').trigger('click');
    await wrapper.get('[data-action="migration-scan"]').trigger('click');
    expect(wrapper.emitted('migration-single')).toBeUndefined();
    expect(wrapper.emitted('migration-all')).toBeUndefined();
    expect(wrapper.emitted('migration-scan')).toBeUndefined();
  });
});
