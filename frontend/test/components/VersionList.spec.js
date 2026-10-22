import { mount } from '@vue/test-utils';
import { afterEach, describe, expect, it, vi } from 'vitest';

import VersionList from '../../src/components/VersionList.vue';

afterEach(() => vi.restoreAllMocks());

// t is injected by the parent in production (AppContent); tests pass a
// key-echoing stub so assertions stay i18n-coupled.
const t = (key, params) => (params ? `${key}:${JSON.stringify(params)}` : key);

describe('VersionList', () => {
  it('renders one row per version with install action in available mode', async () => {
    const wrapper = mount(VersionList, {
      props: {
        mode: 'available',
        versions: [
          { version: '1.0.0', installed: false, active: false },
          { version: '0.9.0', installed: true, active: true },
        ],
        t,
      },
    });
    const rows = wrapper.findAll('.version-row');
    expect(rows).toHaveLength(2);
    expect(rows[0].get('[data-action="install-version"]').text()).toBe('btn.install');
    // installed rows show the installed tag instead of a second install button
    expect(rows[1].find('[data-action="install-version"]').exists()).toBe(false);
    expect(rows[1].text()).toContain('status.installed');
  });

  it('emits install with the chosen version', async () => {
    const wrapper = mount(VersionList, {
      props: { mode: 'available', versions: [{ version: '1.2.3', installed: false }], t },
    });
    await wrapper.get('[data-action="install-version"]').trigger('click');
    expect(wrapper.emitted('install')).toEqual([['1.2.3']]);
  });

  // Spec 039 (dsh rc/alpha 版本流): pre-release versions carry a badge
  // derived from the version string.
  it('marks pre-release versions (rc/alpha) with the prerelease badge', () => {
    const wrapper = mount(VersionList, {
      props: {
        mode: 'available',
        versions: [
          { version: '0.1.2-rc.1', installed: false },
          { version: '0.1.5-alpha.1', installed: false },
          { version: '1.0.0', installed: false },
        ],
        t,
      },
    });
    const rows = wrapper.findAll('.version-row');
    expect(rows[0].text()).toContain('detail.prerelease');
    expect(rows[1].text()).toContain('detail.prerelease');
    expect(rows[2].text()).not.toContain('detail.prerelease');
  });

  it('keeps prerelease badges on installed rows too', () => {
    const wrapper = mount(VersionList, {
      props: {
        mode: 'installed',
        versions: [{ version: '0.1.2-rc.1', installed: true, active: true, intact: true }],
        t,
      },
    });
    expect(wrapper.text()).toContain('detail.prerelease');
  });

  it('shows incomplete badge and repair action for broken installs', async () => {
    const wrapper = mount(VersionList, {
      props: {
        mode: 'installed',
        versions: [{ version: '1.0.0', installed: true, active: false, intact: false }],
        t,
      },
    });
    expect(wrapper.text()).toContain('detail.incomplete');
    await wrapper.get('[data-action="repair-version"]').trigger('click');
    expect(wrapper.emitted('repair')).toEqual([['1.0.0']]);
  });

  it('limits displayed rows and offers show-more with the remainder count', () => {
    const versions = [1, 2, 3, 4].map((i) => ({ version: `1.0.${i}`, installed: false }));
    const wrapper = mount(VersionList, {
      props: { mode: 'available', versions, limit: 2, t },
    });
    expect(wrapper.findAll('.version-row')).toHaveLength(2);
    expect(wrapper.get('[data-action="show-all"]').text()).toBe('detail.show_more:{"count":2}');
  });
});
