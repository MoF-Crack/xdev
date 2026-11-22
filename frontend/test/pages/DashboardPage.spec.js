import { mount } from '@vue/test-utils';
import { describe, expect, it, vi } from 'vitest';
import MetricCard from '../../src/components/MetricCard.vue';
import VersionList from '../../src/components/VersionList.vue';

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

  it('keeps four metrics and derives a compact first-screen toolchain from real installed tools', async () => {
    const installedTools = [
      { name: 'rust', displayName: 'Rust', version: '1.82.0', category: 'language' },
      { name: 'go', displayName: 'Go', version: '1.23.1', category: 'language' },
      { name: 'java', displayName: 'OpenJDK', version: '21.0.4', category: 'language' },
      { name: 'cmake', displayName: 'CMake', version: '3.31.0', category: 'build' },
    ];
    const wrapper = mount(DashboardPage, { props: { t, stats: { installedTools } } });
    expect(wrapper.findAllComponents(MetricCard)).toHaveLength(4);
    const main = wrapper.get('.dashboard-page__main');
    expect(main.findAll(':scope > .section-card').map(section => section.get('h2').text())).toEqual([
      'dashboard.current_toolchain', 'dashboard.installed_tools', 'updates.title', 'scan.results_title', 'ignore.list_title',
    ]);
    const toolchain = wrapper.get('.dashboard-toolchain');
    expect(toolchain.findAll('.toolchain-card')).toHaveLength(3);
    expect(toolchain.get('.toolchain-card__version').text()).toBe('1.82.0');
    expect(toolchain.text()).not.toMatch(/Node\.js|Python|26\.5\.0|3\.14\.6/);
    expect(wrapper.get('.dashboard-installed').findAll('tbody > tr')).toHaveLength(4);
    expect(wrapper.get('.dashboard-installed').text()).toContain('CMake');
    expect(wrapper.get('.dashboard-page__sidebar').text()).toContain('dashboard.paths');
    expect(wrapper.get('.dashboard-page__sidebar').text()).toContain('dashboard.quick_actions');

    await wrapper.setProps({ stats: { installedTools: [] } });
    expect(wrapper.findAll('.toolchain-card')).toHaveLength(0);
    expect(wrapper.get('.dashboard-toolchain').text()).toContain('env.no_installed_tools');
    wrapper.unmount();
  });

  it('preserves management and expanded version event payloads in the installed list', async () => {
    const tool = { name: 'rust', displayName: 'Rust', version: '1.82.0', category: 'language' };
    const wrapper = mount(DashboardPage, { props: { t, stats: { installedTools: [tool] }, expandedName: 'rust', installedVersions: [{ version: '1.81.0' }], availableVersions: [{ version: '1.83.0' }] }, global: { stubs: { VersionList: true } } });
    await wrapper.get('.dashboard-installed tbody button').trigger('click');
    expect(wrapper.emitted('manage-versions')).toEqual([[tool]]);
    const [installed, available] = wrapper.findAllComponents(VersionList);
    installed.vm.$emit('use', '1.81.0');
    installed.vm.$emit('uninstall', '1.81.0');
    installed.vm.$emit('repair', '1.81.0');
    available.vm.$emit('install', '1.83.0');
    expect(wrapper.emitted('use-version')).toEqual([['rust', '1.81.0']]);
    expect(wrapper.emitted('uninstall-version')).toEqual([['rust', '1.81.0']]);
    expect(wrapper.emitted('repair')).toEqual([['rust', '1.81.0']]);
    expect(wrapper.emitted('install')).toEqual([['rust', '1.83.0']]);
    wrapper.unmount();
  });

  it('summarizes scan state without dropping full results, ignore actions, or migration guards', async () => {
    const host = document.body.appendChild(document.createElement('div'));
    const wrapper = mount(DashboardPage, { attachTo: host, props: { t } });
    expect(wrapper.get('.dashboard-scan-summary').text()).toContain('dashboard.scan_pending');
    await wrapper.setProps({ scanAttempted: true });
    expect(wrapper.get('.dashboard-scan-summary').text()).toContain('dashboard.local_none');
    expect(wrapper.get('.dashboard-scan-results').text()).toContain('scan.empty');
    const result = { name: 'go', displayName: 'Go', localVersion: '1.23.0', localPath: 'D:\\Local\\Go' };
    const ignored = { name: 'rust', kind: 'local', path: 'D:\\Local\\Rust', ignoredAt: '2026-11-18' };
    await wrapper.setProps({ scanResults: [result], ignoredTools: [ignored], migrationBusy: true, scanOperating: true });
    expect(wrapper.get('.dashboard-scan-summary').text()).toContain('dashboard.scanning');
    expect(wrapper.get('.scan-summary').attributes('aria-busy')).toBe('true');
    expect(wrapper.get('[data-action="scan-migrate"]').attributes('disabled')).toBeDefined();
    expect(wrapper.get('.dashboard-scan-results').text()).toContain(result.localPath);
    await wrapper.setProps({ migrationBusy: false, scanOperating: false });
    expect(wrapper.get('.dashboard-scan-summary').text()).toContain('dashboard.local_found');
    await wrapper.get('[data-action="scan-migrate"]').trigger('click');
    await wrapper.get('.dashboard-scan-results .table-actions button:last-child').trigger('click');
    await wrapper.get('.dashboard-ignored tbody button').trigger('click');
    expect(wrapper.emitted('scan-migrate')).toEqual([[result]]);
    expect(wrapper.emitted('scan-ignore')).toEqual([[result]]);
    expect(wrapper.emitted('unignore')).toEqual([[ignored]]);
    const section = wrapper.get('#dashboard-scan-results');
    const scrollIntoView = vi.fn();
    section.element.scrollIntoView = scrollIntoView;
    await wrapper.get('[data-action="view-scan-results"]').trigger('click');
    expect(scrollIntoView).toHaveBeenCalledWith({ block: 'start', behavior: 'auto' });
    expect(document.activeElement).toBe(section.element);
    wrapper.unmount(); host.remove();
  });

  it('keeps quick-action busy states and update event contracts after moving them', async () => {
    const update = { tool: 'go', currentVersion: '1.22.0', latestVersion: '1.23.0', command: 'xdev install go@1.23.0' };
    const wrapper = mount(DashboardPage, { props: { t, updates: [update], sniffing: true, scanOperating: true, updateOperating: true, updatesLoading: true } });
    for (const selector of ['[data-action="scan-local"]', '[data-action="check-update"]', '.sniff-button', '.dashboard-updates .xdev-btn--secondary']) {
      expect(wrapper.get(selector).attributes('disabled')).toBeDefined();
    }
    await wrapper.setProps({ sniffing: false, scanOperating: false, updateOperating: false, updatesLoading: false });
    await wrapper.get('.sniff-button').trigger('click');
    await wrapper.get('.dashboard-updates .xdev-btn--secondary').trigger('click');
    await wrapper.get('.dashboard-updates tbody button').trigger('click');
    expect(wrapper.emitted('sniff-all')).toEqual([[]]);
    expect(wrapper.emitted('refresh-updates')).toEqual([[]]);
    expect(wrapper.emitted('install-update')).toEqual([[update]]);
    wrapper.unmount();
  });

  it('retains install/cache occupancy drawers and explicit destructive confirmations', async () => {
    // Inline only the drawer shell: content and ConfirmAction remain real components.
    const wrapper = mount(DashboardPage, {
      props: {
        t,
        installSizeEntries: [{ tool: 'go', version: '1.23.0', path: 'D:\\XDev\\go', size: 2048 }],
        cacheSizeEntries: [{ tool: 'go', version: '1.23.0', path: 'D:\\XDev\\cache\\go.zip', category: 'archives', size: 1024, isStale: true }],
      },
      global: { stubs: {
        Drawer: { props: ['show'], template: '<div v-if="show" class="test-drawer"><slot /></div>' },
        DrawerContent: { props: ['title'], template: '<section class="test-drawer-content"><h2>{{ title }}</h2><slot /></section>' },
      } },
    });
    const metrics = wrapper.findAllComponents(MetricCard);
    await metrics[2].trigger('click');
    expect(wrapper.emitted('show-install-size')).toEqual([[]]);
    await wrapper.get('[data-action="install-size-category"]').trigger('click');
    expect(wrapper.get('.test-drawer').text()).toContain('D:\\XDev\\go');
    await wrapper.get('[data-action="install-size-back"]').trigger('click');
    expect(wrapper.find('[data-action="install-size-category"]').exists()).toBe(true);

    await metrics[3].trigger('click');
    expect(wrapper.emitted('show-cache-size')).toEqual([[]]);
    await wrapper.get('[data-action="clean-cache-category"]').trigger('click');
    expect(wrapper.emitted('clean-cache-category')).toBeUndefined();
    await wrapper.get('[data-action="cancel"]').trigger('click');
    await wrapper.get('[data-action="clean-cache-category"]').trigger('click');
    await wrapper.get('[data-action="confirm"]').trigger('click');
    expect(wrapper.emitted('clean-cache-category')).toEqual([['archives']]);
    await wrapper.get('[data-action="cache-size-category"]').trigger('click');
    await wrapper.get('[data-action="delete-cache-file"]').trigger('click');
    expect(wrapper.emitted('delete-cache-file')).toBeUndefined();
    await wrapper.get('[data-action="confirm"]').trigger('click');
    expect(wrapper.emitted('delete-cache-file')?.[0]?.[0]).toEqual(wrapper.props('cacheSizeEntries')[0]);
    await wrapper.get('[data-action="cache-size-back"]').trigger('click');
    expect(wrapper.find('[data-action="cache-size-category"]').exists()).toBe(true);
    wrapper.unmount();
  });

  it('shows the retained upgrade and refresh error together', () => {
    const updates = [{ tool: 'android-ndk', currentVersion: 'r26', latestVersion: 'r29', stale: true, refreshError: 'offline' }];
    const wrapper = mount(DashboardPage, { props: { t: (key, params) => params?.reason || key, updates } });
    const row = wrapper.get('.dashboard-updates tbody tr').text();
    expect(row).toContain('r26 → r29');
    expect(row).toContain('offline');
    wrapper.unmount();
  });

  // Ticket 12 (audit-fixes / G-02) — B-5: the "Update All" button is
  // disabled while a batch is running, so the re-entry path is blocked at
  // the UI level too (the composable guards remain the authoritative gate).
  it('disables the update-all button while a batch install is running', async () => {
    const updates = [{ tool: 'node', currentVersion: '20.0.0', latestVersion: '22.0.0', command: 'xdev install node@22.0.0' }];
    const wrapper = mount(DashboardPage, { props: { t, updates } });
    const button = wrapper.get('[data-action="install-all-updates"]');
    expect(button.attributes('disabled')).toBeUndefined();

    await wrapper.setProps({ batchInstalling: true });
    expect(wrapper.get('[data-action="install-all-updates"]').attributes('disabled')).toBeDefined();

    // …and it re-enables once the batch settles.
    await wrapper.setProps({ batchInstalling: false });
    expect(wrapper.get('[data-action="install-all-updates"]').attributes('disabled')).toBeUndefined();
  });
});
