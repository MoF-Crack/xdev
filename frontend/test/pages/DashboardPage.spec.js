import { mount } from '@vue/test-utils';
import { describe, expect, it } from 'vitest';

import DashboardPage from '../../src/pages/DashboardPage.vue';

const t = (key) => key;

describe('DashboardPage', () => {
  it('renders metrics, copyable paths, and installed tools from props', () => {
    const wrapper = mount(DashboardPage, { props: { t, stats: {
      totalTools: 24, installedCount: 3, installSize: 1073741824, cacheSize: 1024,
      xmdevDir: 'D:\\XDev', installDir: 'D:\\XDev\\install', shimDir: 'D:\\XDev\\shims', cacheDir: 'D:\\XDev\\cache',
      installedTools: [{ displayName: 'Node.js', version: '20.18.0', category: 'language' }],
    } } });
    expect(wrapper.text()).toContain('24');
    expect(wrapper.text()).toContain('1 GB');
    expect(wrapper.html()).toContain('D:\\XDev\\install');
    expect(wrapper.text()).toContain('Node.js');
  });

  it('exposes loading, error/retry, and quick action events', async () => {
    const loading = mount(DashboardPage, { props: { t, loading: true } });
    expect(loading.get('[role="status"]').text()).toContain('loading');
    const wrapper = mount(DashboardPage, { props: { t, error: 'offline' } });
    expect(wrapper.get('[role="alert"]').text()).toContain('offline');
    expect(wrapper.findAll('[role="alert"] button')).toHaveLength(1);
    await wrapper.get('[role="alert"] button').trigger('click');
    expect(wrapper.emitted('retry')).toHaveLength(1);
    await wrapper.setProps({ error: null });
    await wrapper.get('[data-action="scan-local"]').trigger('click');
    await wrapper.get('[data-action="check-update"]').trigger('click');
    expect(wrapper.emitted('scan-local')).toHaveLength(1);
    expect(wrapper.emitted('check-update')).toHaveLength(1);
  });
});
