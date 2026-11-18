import { config, mount } from '@vue/test-utils';
import { afterEach, describe, expect, it, vi } from 'vitest';

import ToolCatalogPage from '../../src/pages/ToolCatalogPage.vue';

const t = (key) => key;
const tools = [{ name: 'node', displayName: 'Node.js', description: 'Runtime', installed: false, canInstall: true }];
config.global.stubs.NDataTable = true;
afterEach(() => vi.restoreAllMocks());

function mockNarrow(matches = false) {
  vi.spyOn(window, 'matchMedia').mockReturnValue({ matches, addEventListener: vi.fn(), removeEventListener: vi.fn() });
}

function visibleToolNames(wrapper) {
  return wrapper.findAll('[data-action="detail"]').map((button) => button.attributes('data-tool'));
}

const filterTools = [
  { name: 'managed', installed: true, activeVersion: '2.0.0' },
  { name: 'managed-local', installed: true, activeVersion: '3.0.0', localVersion: '1.0.0', localPath: 'C:\\Detected\\managed' },
  { name: 'local-version', installed: false, localVersion: '1.1.0' },
  { name: 'local-path', installed: false, localPath: 'C:\\Detected\\path-only' },
  { name: 'missing', installed: false, canInstall: true },
];

describe('ToolCatalogPage', () => {
  it('preserves search, availability, detail, and install entry points', async () => {
    vi.spyOn(window, 'matchMedia').mockReturnValue({ matches: false, addEventListener: vi.fn(), removeEventListener: vi.fn() });
    const wrapper = mount(ToolCatalogPage, { props: { t, tools, searchQuery: '', hideUnavailable: true } });
    await wrapper.get('[data-testid="tool-search"]').setValue('node');
    await wrapper.get('[data-testid="hide-unavailable"]').setValue(false);
    await wrapper.get('[data-action="detail"]').trigger('click');
    expect(wrapper.emitted('update:searchQuery')).toEqual([['node']]);
    expect(wrapper.emitted('update:hideUnavailable')).toEqual([[false]]);
    expect(wrapper.emitted('open-detail')).toEqual([[tools[0]]]);
    expect(wrapper.find('[data-action="install"]').exists()).toBe(false);
    expect(wrapper.get('[data-testid="tool-search"]').attributes('aria-label')).toBe('search.label');
  });

  it.each([false, true])('keeps package-manager catalog rows to detail and favorite only in %s narrow mode', async (narrow) => {
    vi.spyOn(window, 'matchMedia').mockReturnValue({ matches: narrow, addEventListener: vi.fn(), removeEventListener: vi.fn() });
    const rows = [
      { name: 'npm', toolName: 'npm', displayName: 'npm', installed: false, canInstall: true, isFavorite: false },
      { name: 'pnpm', toolName: 'pnpm', displayName: 'pnpm', installed: true, canInstall: true, isFavorite: true, hasUpdate: true },
    ];
    const wrapper = mount(ToolCatalogPage, { props: { t, tools: rows, category: 'package-manager' } });
    await wrapper.vm.$nextTick();
    expect(wrapper.findAll('[data-action="package-manager"]')).toHaveLength(0);
    expect(wrapper.findAll('[data-action="install"]')).toHaveLength(0);
    await wrapper.findAll('[data-action="favorite"]')[0].trigger('click');
    expect(wrapper.emitted('toggle-favorite')).toEqual([[rows[0]]]);
    expect(wrapper.findAll('[data-action="favorite"]')[1].attributes('aria-label')).toBe('favorites.remove');
    expect(wrapper.findAll('[data-action="favorite"]')[1].attributes('title')).toBe('favorites.remove');
  });

  it.each([false, true])('never shows catalog Install for already-installed language/build/IDE tools in %s narrow mode', async (narrow) => {
    vi.spyOn(window, 'matchMedia').mockReturnValue({ matches: narrow, addEventListener: vi.fn(), removeEventListener: vi.fn() });
    const rows = [
      { name: 'go', displayName: 'Go', category: 'language', installed: true, localVersion: '1.26.5', localPath: 'E:\\XDev\\install\\go' },
      { name: 'gradle', displayName: 'Gradle', category: 'build', installed: true, localVersion: '8.14' },
      { name: 'vscode', displayName: 'VS Code', category: 'development_tools', installed: true, localPath: 'E:\\XDev\\install\\vscode' },
    ];
    const wrapper = mount(ToolCatalogPage, { props: { t, tools: rows, category: 'language' } });
    await wrapper.vm.$nextTick();
    expect(wrapper.findAll('[data-action="install"]')).toHaveLength(0);
    expect(wrapper.findAll('[data-action="package-manager"]')).toHaveLength(0);
  });

  it.each([false, true])('shows install hints without install actions and never installs local-only tools in %s narrow mode', async (narrow) => {
    vi.spyOn(window, 'matchMedia').mockReturnValue({ matches: narrow, addEventListener: vi.fn(), removeEventListener: vi.fn() });
    const rows = [
      { name: 'missing', displayName: 'Missing', canInstall: false, installHint: 'Use vendor installer' },
      { name: 'local', displayName: 'Local', canInstall: false, localVersion: '1.0', localPath: 'C:\\Local' },
    ];
    const wrapper = mount(ToolCatalogPage, { props: { t, tools: rows, category: 'language' } });
    await wrapper.vm.$nextTick();
    expect(wrapper.text()).toContain('Use vendor installer');
    expect(wrapper.findAll('[data-action="install"]')).toHaveLength(0);
    expect(wrapper.text()).toContain('catalog.local_detected');
  });

  it('switches from a dense desktop table to cards on narrow screens', async () => {
    const listeners = new Set();
    const media = { matches: false, addEventListener: vi.fn((_, fn) => listeners.add(fn)), removeEventListener: vi.fn() };
    vi.spyOn(window, 'matchMedia').mockReturnValue(media);
    const wrapper = mount(ToolCatalogPage, { props: { t, tools } });
    expect(wrapper.find('[data-testid="tool-table"]').exists()).toBe(true);
    media.matches = true;
    listeners.forEach((listener) => listener({ matches: true }));
    await wrapper.vm.$nextTick();
    expect(wrapper.find('[data-testid="tool-card"]').exists()).toBe(true);
  });

  it('keeps the heading separate and the search, filters, table, and count in one panel', () => {
    mockNarrow();
    const translate = (key, params) => params ? `${key}:${params.n}` : key;
    const wrapper = mount(ToolCatalogPage, { props: { t: translate, tools: filterTools, category: 'language' } });
    expect(wrapper.get('h1').text()).toBe('category.language');
    expect(wrapper.get('.tool-catalog__heading p').text()).toBe('catalog.description');
    const panel = wrapper.get('.tool-catalog__panel');
    expect(panel.find('[data-testid="tool-search"]').exists()).toBe(true);
    expect(panel.find('.tool-catalog__tabs').exists()).toBe(true);
    expect(panel.find('[data-testid="tool-table"]').exists()).toBe(true);
    expect(panel.get('.tool-catalog__footer').text()).toBe('catalog.tool_count:5');
  });

  it.each([false, true])('filters actual installed and local rows in %s narrow mode', async (narrow) => {
    mockNarrow(narrow);
    const wrapper = mount(ToolCatalogPage, { props: { t, tools: filterTools, category: 'language' } });
    await wrapper.vm.$nextTick();
    const tabs = wrapper.findAll('.tool-catalog__tabs button');
    expect(tabs.map((tab) => tab.text())).toEqual(['catalog.all 5', 'status.installed 2', 'catalog.local_filter 3']);
    expect(tabs.map((tab) => tab.attributes('aria-pressed'))).toEqual(['true', 'false', 'false']);
    expect(visibleToolNames(wrapper)).toEqual(filterTools.map((tool) => tool.name));

    await tabs[1].trigger('click');
    expect(visibleToolNames(wrapper)).toEqual(['managed', 'managed-local']);
    expect(tabs[1].attributes('aria-pressed')).toBe('true');

    await tabs[2].trigger('click');
    expect(visibleToolNames(wrapper)).toEqual(['managed-local', 'local-version', 'local-path']);
    expect(tabs[2].attributes('aria-pressed')).toBe('true');
    await wrapper.get('[data-action="favorite"]').trigger('click');
    expect(wrapper.emitted('toggle-favorite')).toEqual([[filterTools[1]]]);
    expect(wrapper.find('[data-action="install"]').exists()).toBe(false);

    await tabs[0].trigger('click');
    expect(visibleToolNames(wrapper)).toEqual(filterTools.map((tool) => tool.name));
  });

  it('resets the filter when the category changes and recalculates counts from new rows', async () => {
    mockNarrow();
    const wrapper = mount(ToolCatalogPage, { props: { t, tools: filterTools, category: 'language' } });
    await wrapper.findAll('.tool-catalog__tabs button')[1].trigger('click');
    await wrapper.setProps({ category: 'build', tools: [filterTools[4]] });
    expect(visibleToolNames(wrapper)).toEqual(['missing']);
    expect(wrapper.findAll('.tool-catalog__tabs button').map((tab) => tab.text())).toEqual(['catalog.all 1', 'status.installed 0', 'catalog.local_filter 0']);
    expect(wrapper.findAll('.tool-catalog__tabs button')[0].attributes('aria-pressed')).toBe('true');
  });

  it('shows the filtered empty state without losing the controls or footer', async () => {
    mockNarrow();
    const translate = (key, params) => params ? `${key}:${params.n}` : key;
    const wrapper = mount(ToolCatalogPage, { props: { t: translate, tools, category: 'language' } });
    await wrapper.findAll('.tool-catalog__tabs button')[1].trigger('click');
    expect(wrapper.text()).toContain('no_tools_found');
    expect(wrapper.find('[data-testid="tool-table"]').exists()).toBe(false);
    expect(wrapper.find('[data-testid="tool-search"]').exists()).toBe(true);
    expect(wrapper.get('.tool-catalog__footer').text()).toBe('catalog.tool_count:0');
  });

  it('reflects selectedName on the matching desktop row and updates without changing events', async () => {
    mockNarrow();
    const wrapper = mount(ToolCatalogPage, { props: { t, tools: filterTools, selectedName: 'managed-local' } });
    expect(wrapper.get('tr.tool-table__row--selected').attributes('data-tool')).toBe('managed-local');
    expect(wrapper.get('[data-action="detail"][data-tool="managed-local"]').attributes('aria-current')).toBe('true');
    await wrapper.get('[data-action="detail"][data-tool="local-version"]').trigger('click');
    expect(wrapper.emitted('open-detail')).toEqual([[filterTools[2]]]);
    await wrapper.setProps({ selectedName: 'local-version' });
    expect(wrapper.findAll('tr.tool-table__row--selected')).toHaveLength(1);
    expect(wrapper.get('tr.tool-table__row--selected').attributes('data-tool')).toBe('local-version');
    await wrapper.setProps({ selectedName: '' });
    expect(wrapper.findAll('tr.tool-table__row--selected')).toHaveLength(0);
  });

  it.each([
    [{ loading: true }, 'catalog.loading'],
    [{ error: 'offline' }, 'offline'],
    [{ tools: [] }, 'no_tools_found'],
  ])('shows explicit async state %#', (props, text) => {
    const wrapper = mount(ToolCatalogPage, { props: { t, ...props } });
    expect(wrapper.text()).toContain(text);
  });
});
