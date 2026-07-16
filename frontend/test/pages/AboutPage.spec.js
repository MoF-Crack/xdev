import { mount } from '@vue/test-utils';
import { describe, expect, it } from 'vitest';
import AboutPage from '../../src/pages/AboutPage.vue';

const t = (key) => key;

describe('AboutPage', () => {
  it('shows copyable build values, secure repository link, license, and update entry', async () => {
    const info = { appName: 'XDev', version: '1.2.3', buildDate: 'today', goVersion: '1.24', wailsVersion: '2.9', platform: 'windows', arch: 'amd64', repoURL: 'https://example.test/repo', license: 'MIT' };
    const wrapper = mount(AboutPage, { props: { t, info } });
    expect(wrapper.findAllComponents({ name: 'CopyableValue' }).map((c) => c.props('value'))).toEqual(expect.arrayContaining(['1.2.3', 'today', 'windows/amd64', 'MIT']));
    expect(wrapper.get('a').attributes()).toMatchObject({ target: '_blank', rel: 'noopener noreferrer' });
    await wrapper.get('[data-action="check-update"]').trigger('click');
    expect(wrapper.emitted('check-update')).toHaveLength(1);
  });

  it('does not render a non-http repository URL as a link', () => {
    const wrapper = mount(AboutPage, { props: { t, info: { repoURL: 'javascript:alert(1)' } } });
    expect(wrapper.find('a').exists()).toBe(false);
    expect(wrapper.findAllComponents({ name: 'CopyableValue' }).some((c) => c.props('value') === 'javascript:alert(1)')).toBe(true);
  });
});
