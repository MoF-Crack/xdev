import { mount } from '@vue/test-utils';
import { describe, expect, it } from 'vitest';

import SettingsPage from '../../src/pages/SettingsPage.vue';
import MirrorSettings from '../../src/components/settings/MirrorSettings.vue';

const t = (key) => key;
const props = {
  t,
  theme: 'dark',
  locale: 'en',
  pathMode: 'shim',
  mirrorConfig: { node: 'https://nodejs.org/dist', npm: 'https://registry.npmmirror.com' },
  mirrorTestResults: { node: { success: true, latency: 42, url: 'https://nodejs.org/dist', lastChecked: '2026-07-12T10:00:00Z' } },
  proxy: { enabled: false, http: '', https: '', noProxy: '' },
  updateInfo: null,
  diagResults: [{ check: 'PATH', path: 'D:\\XDev', ok: true }],
  cacheBreakdown: { archives: 1024, shaFiles: 10, partFiles: 0, scanCache: 20, otherFiles: 30, totalCount: 2 },
};

describe('SettingsPage', () => {
  it('rejects non-HTTPS mirror add and save with visible localized feedback', async () => {
    const wrapper = mount(SettingsPage, { props });
    await wrapper.get('#new-mirror-tool').setValue('node'); await wrapper.get('#new-mirror-url').setValue('http://example.test');
    await wrapper.get('[data-action="mirror-add"]').trigger('click');
    expect(wrapper.emitted('mirror-add')).toBeUndefined(); expect(wrapper.get('[role="alert"]').text()).toContain('mirror.url_https');
  });
  it('groups settings with anchor navigation and labels every control', () => {
    const wrapper = mount(SettingsPage, { props });
    expect(wrapper.find('[aria-label="settings.sections"]').exists()).toBe(true);
    expect(wrapper.getComponent(MirrorSettings).props('locale')).toBe('en');
    expect(wrapper.findAll('.settings-page__section').length).toBeGreaterThan(3);
    expect(wrapper.findAll('input').every((input) => input.attributes('aria-label') || input.attributes('id') || input.element.closest('label'))).toBe(true);
  });

  it('preserves theme, locale, path, proxy, profile, update and diagnostics actions', async () => {
    const wrapper = mount(SettingsPage, { props });
    await wrapper.get('[data-action="theme-light"]').find('input').setValue(true);
    await wrapper.get('[data-action="locale"]').setValue('zh');
    await wrapper.get('[data-action="path-mode"]').setValue('direct');
    await wrapper.get('[data-action="proxy-enable"]').setValue(true);
    for (const action of ['proxy-test', 'proxy-save', 'profile-export', 'profile-import', 'check-update', 'run-diagnostics']) {
      await wrapper.get(`[data-action="${action}"]`).trigger('click');
    }
    expect(wrapper.emitted('update:theme')).toEqual([['light']]);
    expect(wrapper.emitted('update:locale')).toEqual([['zh']]);
    expect(wrapper.emitted('update:path-mode')).toEqual([['direct']]);
    expect(wrapper.emitted('proxy-save')).toBeTruthy();
    expect(wrapper.emitted('run-diagnostics')).toBeTruthy();
  });

  it('shows mirror role, concrete URL, connectivity result, latency and last checked', () => {
    const wrapper = mount(SettingsPage, { props });
    const text = wrapper.text();
    expect(text).toContain('official');
    expect(text).toContain('mirror');
    expect(wrapper.get('[data-mirror="node"] input').element.value).toBe('https://nodejs.org/dist');
    expect(text).toContain('42 ms');
    expect(text.toLowerCase()).toContain('connectivity');
    expect(text).toContain('2026');
    expect(text.toLowerCase()).not.toContain('archive verified');
  });

  it('emits mirror CRUD/test/save and confirms destructive actions', async () => {
    const wrapper = mount(SettingsPage, { props });
    await wrapper.get('[data-mirror="node"] input').setValue('https://example.test/node');
    await wrapper.setProps({ mirrorConfig: { ...props.mirrorConfig, node: 'https://example.test/node' } });
    await wrapper.get('[data-mirror="node"] [data-action="mirror-test"]').trigger('click');
    await wrapper.get('[data-action="mirror-save"]').trigger('click');
    await wrapper.get('[data-mirror="node"] [data-action="mirror-remove"]').trigger('click');
    expect(wrapper.find('[role="dialog"]').exists()).toBe(true);
    await wrapper.get('[role="dialog"] [data-action="confirm"]').trigger('click');
    expect(wrapper.emitted('mirror-update')).toBeTruthy();
    expect(wrapper.find('[data-mirror="node"] .result .status-badge').exists()).toBe(false);
    expect(wrapper.emitted('mirror-test')).toBeTruthy();
    expect(wrapper.emitted('mirror-save')).toBeTruthy();
    expect(wrapper.emitted('mirror-remove')).toEqual([['node']]);
  });

  it('requires confirmation before cleaning all cache', async () => {
    const wrapper = mount(SettingsPage, { props });
    await wrapper.get('[data-action="cache-clean-all"]').trigger('click');
    expect(wrapper.find('[role="dialog"]').exists()).toBe(true);
    expect(wrapper.emitted('cache-clean')).toBeFalsy();
    await wrapper.get('[role="dialog"] [data-action="confirm"]').trigger('click');
    expect(wrapper.emitted('cache-clean')).toEqual([['all']]);
  });
});
