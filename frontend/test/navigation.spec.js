import { describe, expect, it } from 'vitest';

import {
  commandPaletteEntries,
  createNavigationGroups,
  findPageDefinition,
  navigationGroups,
} from '../src/navigation/navigation.js';

const staticPageKeys = [
  'dashboard', 'favorites', 'env-vars', 'project', 'tasks', 'services',
  'isolate', 'remote', 'ai', 'custom-tools', 'plugins', 'settings', 'about',
];

describe('navigation registry', () => {
  it('defines every static page exactly once', () => {
    const keys = navigationGroups.flatMap((group) => group.items.map((item) => item.key));

    expect(keys).toHaveLength(new Set(keys).size);
    expect(keys.sort()).toEqual(staticPageKeys.sort());
  });

  it('finds page metadata used by the toolbar', () => {
    expect(findPageDefinition('dashboard')).toMatchObject({ searchable: false });
    expect(findPageDefinition('settings')).toMatchObject({ labelKey: 'nav.settings', searchable: false });
    expect(findPageDefinition('missing')).toBeUndefined();
  });

  it('derives command palette entries from the merged registry', () => {
    const groups = createNavigationGroups([
      { key: 'language', label: 'Languages', searchable: true },
      { key: 'build', label: 'Build tools', searchable: true },
    ]);
    const pageKeys = groups.flatMap((group) => group.items.map((item) => item.key));
    const commandKeys = commandPaletteEntries(groups, (key) => key).map((item) => item.key);

    expect(commandKeys).toEqual(pageKeys);
    expect(findPageDefinition('language', groups)).toMatchObject({ label: 'Languages', searchable: true });
  });

  it('drops dynamic pages whose keys collide with static or earlier dynamic pages', () => {
    const groups = createNavigationGroups([
      { key: 'dashboard', label: 'Duplicate static', searchable: true },
      { key: 'language', label: 'Languages', searchable: true },
      { key: 'language', label: 'Duplicate dynamic', searchable: true },
    ]);
    const keys = groups.flatMap((group) => group.items.map((item) => item.key));

    expect(keys.filter((key) => key === 'dashboard')).toHaveLength(1);
    expect(keys.filter((key) => key === 'language')).toHaveLength(1);
    expect(keys).toHaveLength(new Set(keys).size);
  });
});
