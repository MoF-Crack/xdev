import {
  CloudOutline,
  GitBranchOutline, HomeOutline, InformationCircleOutline,
  SettingsOutline, StarOutline, TerminalOutline,
} from '@vicons/ionicons5';

const staticGroups = [
  {
    key: 'tools-group', labelKey: 'nav.tools', items: [
      { key: 'dashboard', labelKey: 'nav.dashboard', icon: HomeOutline, searchable: false },
      { key: 'favorites', labelKey: 'nav.favorites', icon: StarOutline, searchable: false },
    ],
  },
  {
    key: 'environment-group', labelKey: 'nav.environment', items: [
      { key: 'env-vars', labelKey: 'nav.env_vars', icon: TerminalOutline, searchable: false },
    ],
  },
  {
    key: 'development-group', labelKey: 'nav.development', items: [
      { key: 'isolate', labelKey: 'nav.isolate', icon: GitBranchOutline, searchable: false },
      { key: 'remote', labelKey: 'nav.remote', icon: CloudOutline, searchable: false },
    ],
  },
  {
    key: 'settings-group', labelKey: 'nav.settings', items: [
      { key: 'settings', labelKey: 'nav.settings', icon: SettingsOutline, searchable: false },
      { key: 'about', labelKey: 'nav.about', icon: InformationCircleOutline, searchable: false },
    ],
  },
];

export const navigationGroups = staticGroups;

export function createNavigationGroups(dynamicToolPages = []) {
  const knownKeys = new Set(staticGroups.flatMap((group) => group.items.map((item) => item.key)));
  const uniqueDynamicPages = dynamicToolPages.filter((page) => {
    if (!page?.key || knownKeys.has(page.key)) return false;
    knownKeys.add(page.key);
    return true;
  });
  return staticGroups.map((group) => ({
    ...group,
    items: group.key === 'tools-group'
      ? [...group.items, ...uniqueDynamicPages]
      : [...group.items],
  }));
}

export function findPageDefinition(key, groups = navigationGroups) {
  return groups.flatMap((group) => group.items).find((page) => page.key === key);
}

export function commandPaletteEntries(groups, translate) {
  return groups.flatMap((group) => group.items.map((page) => ({
    key: page.key,
    label: page.label ?? translate(page.labelKey),
    group: group.label ?? translate(group.labelKey),
  })));
}
