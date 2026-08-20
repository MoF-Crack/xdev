import { config, mount } from '@vue/test-utils';
import { describe, expect, it, vi } from 'vitest';

import ToolCatalogPage from '../../src/pages/ToolCatalogPage.vue';

const t = (key) => key;
const tools = [{ name: 'node', displayName: 'Node.js', description: 'Runtime', installed: false, canInstall: true }];
config.global.stubs.NDataTable = true;

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

  it.each([
    [{ loading: true }, 'catalog.loading'],
    [{ error: 'offline' }, 'offline'],
    [{ tools: [] }, 'no_tools_found'],
  ])('shows explicit async state %#', (props, text) => {
    const wrapper = mount(ToolCatalogPage, { props: { t, ...props } });
    expect(wrapper.text()).toContain(text);
  });
});
