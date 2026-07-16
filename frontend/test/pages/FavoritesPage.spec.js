import { mount } from '@vue/test-utils';
import { describe, expect, it } from 'vitest';
import FavoritesPage from '../../src/pages/FavoritesPage.vue';

const t = (key) => key;
const favorite = { name: 'node', displayName: 'Node.js', category: 'language', installed: true, activeVersion: '20', localVersion: '18', localPath: 'C:\\node' };

describe('FavoritesPage', () => {
  it('preserves detail, remove, latest, migration, version, and source actions', async () => {
    const wrapper = mount(FavoritesPage, { props: { t, favorites: [favorite], expandedName: '', installedVersions: [], availableVersions: [] } });
    for (const action of ['detail', 'remove-favorite', 'manage-versions', 'switch-local']) await wrapper.get(`[data-action="${action}"]`).trigger('click');
    expect(wrapper.emitted('open-detail')[0]).toEqual([favorite]);
    expect(wrapper.emitted('remove-favorite')[0]).toEqual(['node']);
    expect(wrapper.emitted('manage-versions')[0]).toEqual([favorite]);
    expect(wrapper.emitted('switch-local')[0]).toEqual(['node']);
    expect(wrapper.get('[data-action="detail"]').attributes('aria-label')).toBe('favorites.details');
    expect(wrapper.get('[data-action="remove-favorite"]').attributes('title')).toBe('favorites.remove');
  });

  it('exposes use, uninstall, version install, custom version, and show all', async () => {
    const wrapper = mount(FavoritesPage, { props: { t, favorites: [favorite], expandedName: 'node', installedVersions: [{ version: '20', active: true }, { version: '18' }], availableVersions: Array.from({ length: 6 }, (_, i) => ({ version: `${30 - i}`, installed: false })) } });
    await wrapper.get('[data-action="use-version"]').trigger('click');
    await wrapper.get('[data-action="uninstall-version"]').trigger('click');
    await wrapper.get('[data-action="install-version"]').trigger('click');
    await wrapper.get('[data-action="show-all"]').trigger('click');
    await wrapper.get('[data-testid="favorite-custom-version"]').setValue('21.2');
    await wrapper.get('[data-action="install-custom"]').trigger('click');
    expect(wrapper.emitted('use-version')[0]).toEqual(['node', '18']);
    expect(wrapper.emitted('install')[1]).toEqual(['node', '21.2']);
  });
});
