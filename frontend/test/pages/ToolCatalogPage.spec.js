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
    await wrapper.get('[data-action="install"]').trigger('click');
    expect(wrapper.emitted('update:searchQuery')).toEqual([['node']]);
    expect(wrapper.emitted('update:hideUnavailable')).toEqual([[false]]);
    expect(wrapper.emitted('open-detail')).toEqual([[tools[0]]]);
    expect(wrapper.emitted('install')).toEqual([[tools[0]]]);
    expect(wrapper.get('[data-testid="tool-search"]').attributes('aria-label')).toBe('search.label');
  });

  it.each([false, true])('preserves package manager and favorite contracts in %s narrow mode', async (narrow) => {
    vi.spyOn(window, 'matchMedia').mockReturnValue({ matches: narrow, addEventListener: vi.fn(), removeEventListener: vi.fn() });
    const rows = [
      { name: 'npm', toolName: 'npm', displayName: 'npm', installed: false, canInstall: true, isFavorite: false },
      { name: 'pnpm', toolName: 'pnpm', displayName: 'pnpm', installed: true, canInstall: true, isFavorite: true },
    ];
    const wrapper = mount(ToolCatalogPage, { props: { t, tools: rows, category: 'package-manager' } });
    await wrapper.vm.$nextTick();
    await wrapper.findAll('[data-action="package-manager"]')[0].trigger('click');
    await wrapper.findAll('[data-action="package-manager"]')[1].trigger('click');
    await wrapper.findAll('[data-action="favorite"]')[0].trigger('click');
    expect(wrapper.emitted('install-package-manager')).toEqual([[rows[0]]]);
    expect(wrapper.emitted('update-package-manager')).toEqual([[rows[1]]]);
    expect(wrapper.emitted('toggle-favorite')).toEqual([[rows[0]]]);
    expect(wrapper.findAll('[data-action="favorite"]')[1].attributes('aria-label')).toBe('favorites.remove');
    expect(wrapper.findAll('[data-action="favorite"]')[1].attributes('title')).toBe('favorites.remove');
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
