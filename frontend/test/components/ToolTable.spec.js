import { mount } from '@vue/test-utils';
import { describe, expect, it } from 'vitest';

import ToolTable from '../../src/components/ToolTable.vue';

const t = (key) => key;
const tools = [
  { name: 'node', displayName: 'Node.js', description: 'JavaScript runtime', formFactor: ['cli'], installed: true, activeVersion: '26.5.0', localVersion: '20.18.0', localPath: 'C:\\Detected\\node', isFavorite: true },
  { name: 'python', displayName: 'Python', description: 'Python runtime', formFactor: ['cli', 'gui'], installed: true, isFavorite: false },
  { name: 'custom', displayName: 'Custom tool', installed: false, localVersion: '1.2.3', localPath: 'D:\\Tools\\custom', isFavorite: false },
  { name: 'missing', displayName: 'Missing tool', installed: false, canInstall: false, installHint: 'Use vendor installer', isFavorite: false },
];

function mountTable(props = {}) {
  return mount(ToolTable, { props: { t, tools, ...props } });
}

function getRow(wrapper, name) {
  return wrapper.get(`tbody tr[data-tool="${name}"]`);
}

describe('ToolTable', () => {
  it('preserves four columns and renders compact identity, form, and description content', () => {
    const wrapper = mountTable();
    expect(wrapper.findAll('thead th').map((heading) => heading.text())).toEqual(['table.tool', 'table.version', 'table.local_path', 'table.action']);
    expect(wrapper.findAll('thead th').every((heading) => heading.attributes('scope') === 'col')).toBe(true);
    expect(wrapper.findAll('tbody tr')).toHaveLength(4);
    expect(wrapper.findAll('tbody tr').every((row) => row.findAll('td').length === 4)).toBe(true);
    const node = getRow(wrapper, 'node');
    expect(node.get('.tool-table__monogram').text()).toBe('JS');
    expect(node.get('.tool-table__monogram').attributes('aria-hidden')).toBe('true');
    expect(node.get('.tool-table__name strong').text()).toBe('Node.js');
    expect(node.get('.tool-table__description').text()).toBe('JavaScript runtime');
    expect(node.get('[data-form="cli"]').text()).toBe('form.cli');
    expect(getRow(wrapper, 'python').findAll('.form-badge').map((badge) => badge.text())).toEqual(['form.cli', 'form.gui']);
    expect(getRow(wrapper, 'custom').get('.tool-table__monogram').text()).toBe('Cu');
  });

  it('uses only real versions, installation state, and detected local paths', () => {
    const wrapper = mountTable();
    const node = getRow(wrapper, 'node');
    expect(node.get('.tool-table__version-value').text()).toBe('26.5.0');
    expect(node.get('.tool-table__status').attributes('data-status')).toBe('active');
    expect(node.get('.tool-table__status').text()).toBe('status.active');
    expect(node.get('.tool-table__path code').text()).toBe('C:\\Detected\\node');
    expect(node.get('.tool-table__path code').attributes('title')).toBe('C:\\Detected\\node');
    expect(node.get('.tool-table__local-note').text()).toBe('catalog.local_detected 20.18.0');
    const installed = getRow(wrapper, 'python');
    expect(installed.get('.tool-table__status').attributes('data-status')).toBe('installed');
    expect(installed.get('.tool-table__status').text()).toBe('status.installed');
    expect(installed.get('.tool-table__version-value').text()).toBe('-');
    expect(installed.get('.tool-table__path code').text()).toBe('-');
    const local = getRow(wrapper, 'custom');
    expect(local.get('.tool-table__version-value').text()).toBe('1.2.3');
    expect(local.get('.tool-table__status').attributes('data-status')).toBe('local');
    expect(local.get('.tool-table__status').text()).toBe('catalog.local_detected');
    const missing = getRow(wrapper, 'missing');
    expect(missing.get('.tool-table__status').attributes('data-status')).toBe('missing');
    expect(missing.get('.tool-table__status').text()).toBe('status.not_installed');
    expect(missing.get('.tool-table__version-value').text()).toBe('-');
    expect(missing.get('.tool-table__path code').text()).toBe('-');
    expect(wrapper.text()).not.toMatch(/E:\\XDev|current\\|win-x64/);
  });

  it('recognizes path-only local tools but does not treat an uninstalled stale active version as installed', () => {
    const wrapper = mountTable({ tools: [
      { name: 'path-only', localPath: 'D:\\Detected\\path-only', installed: false },
      { name: 'stale', activeVersion: '0.9.0', installed: false },
    ] });
    expect(getRow(wrapper, 'path-only').get('.tool-table__status').attributes('data-status')).toBe('local');
    expect(getRow(wrapper, 'path-only').get('.tool-table__version-value').text()).toBe('-');
    expect(getRow(wrapper, 'stale').get('.tool-table__status').attributes('data-status')).toBe('missing');
  });

  it('tracks a single controlled selection and exposes it on the detail button', async () => {
    const wrapper = mountTable({ selectedName: 'node' });
    expect(wrapper.findAll('.tool-table__row--selected')).toHaveLength(1);
    expect(getRow(wrapper, 'node').classes()).toContain('tool-table__row--selected');
    expect(getRow(wrapper, 'node').get('[data-action="detail"]').attributes('aria-current')).toBe('true');
    expect(getRow(wrapper, 'python').get('[data-action="detail"]').attributes('aria-current')).toBeUndefined();
    await wrapper.setProps({ selectedName: 'python' });
    expect(getRow(wrapper, 'node').classes()).not.toContain('tool-table__row--selected');
    expect(getRow(wrapper, 'python').classes()).toContain('tool-table__row--selected');
    await wrapper.setProps({ selectedName: 'unknown' });
    expect(wrapper.findAll('.tool-table__row--selected')).toHaveLength(0);
  });

  it('preserves detail and favorite action/data-tool contracts and payloads', async () => {
    const wrapper = mountTable();
    await wrapper.get('[data-action="detail"][data-tool="node"]').trigger('click');
    await wrapper.get('[data-action="favorite"][data-tool="custom"]').trigger('click');
    expect(wrapper.emitted('open-detail')).toEqual([[tools[0]]]);
    expect(wrapper.emitted('toggle-favorite')).toEqual([[tools[2]]]);
    expect(wrapper.findAll('[data-action="detail"]')).toHaveLength(tools.length);
    expect(wrapper.findAll('[data-action="favorite"]')).toHaveLength(tools.length);
    expect(wrapper.get('[data-action="favorite"][data-tool="node"]').attributes('aria-label')).toBe('favorites.remove');
    expect(wrapper.get('[data-action="favorite"][data-tool="node"]').attributes('aria-pressed')).toBe('true');
    expect(wrapper.get('[data-action="favorite"][data-tool="custom"]').attributes('title')).toBe('favorites.add');
    expect(wrapper.get('[data-action="favorite"][data-tool="custom"]').attributes('aria-pressed')).toBe('false');
    expect(wrapper.findAll('[data-action="favorite"] svg').every((icon) => icon.attributes('aria-hidden') === 'true')).toBe(true);
  });

  it('never adds installation actions and retains the real install hint', () => {
    const wrapper = mountTable();
    expect(getRow(wrapper, 'missing').text()).toContain('Use vendor installer');
    expect(wrapper.find('[data-action="install"]').exists()).toBe(false);
    expect(wrapper.find('[data-action="package-manager"]').exists()).toBe(false);
    expect(wrapper.emitted('install')).toBeUndefined();
  });
});
