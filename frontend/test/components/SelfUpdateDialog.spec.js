import { flushPromises, mount } from '@vue/test-utils';
import { afterEach, describe, expect, it } from 'vitest';
import SelfUpdateDialog from '../../src/components/SelfUpdateDialog.vue';

const t = (key, params) => (params ? `${key}{${Object.values(params).join('/')}}` : key);
const assetName = 'xdev-windows-amd64-v1.0.0-beta.2.zip';
const assetUrl = 'https://example.test/beta.2.zip';
const platformRelease = {
  platform: 'windows/amd64', assetName,
  downloadUrls: {
    'xdev-linux-amd64-v1.0.0-beta.2.tar.gz': 'https://example.test/linux.tar.gz',
    [assetName]: assetUrl,
    'xdev-darwin-arm64-v1.0.0-beta.2.tar.gz': 'https://example.test/darwin.tar.gz',
  },
};
const wrappers = [];
function mountDialog(props = {}) {
  const wrapper = mount(SelfUpdateDialog, { attachTo: document.body, props: { show: true, current: '1.0.0-alpha.3', latest: '1.0.0-beta.2', t, ...props } });
  wrappers.push(wrapper);
  return wrapper;
}
afterEach(() => { wrappers.splice(0).forEach(wrapper => wrapper.unmount()); document.body.innerHTML = ''; });

describe('SelfUpdateDialog', () => {
  it('renders version information and preserves startup actions with an explicit download/install confirmation', () => {
    const wrapper = mountDialog();
    expect(wrapper.get('[data-testid="self-update-dialog"]').exists()).toBe(true);
    expect(wrapper.text()).toContain('1.0.0-alpha.3');
    expect(wrapper.text()).toContain('1.0.0-beta.2');
    expect(wrapper.get('[data-action="self-update-update"]').text()).toBe('self_update.download_install');
    expect(wrapper.get('[data-action="self-update-ignore"]').exists()).toBe(true);
    expect(wrapper.get('[data-action="self-update-quit"]').exists()).toBe(true);
    expect(wrapper.text()).toContain('self_update.confirm');
    const dialog = wrapper.get('[role="dialog"]');
    expect(wrapper.get(`#${dialog.attributes('aria-labelledby')}`).text()).toBe('self_update.title');
    expect(wrapper.get(`#${dialog.attributes('aria-describedby')}`).text()).toContain('1.0.0-beta.2');
    expect(wrapper.findAll('button').every(button => !!button.attributes('aria-label'))).toBe(true);
  });

  it('manual confirmation has close and download/install, but does not silently ignore a release', () => {
    const wrapper = mountDialog({ source: 'manual' });
    expect(wrapper.find('[data-action="self-update-ignore"]').exists()).toBe(false);
    expect(wrapper.find('[data-action="self-update-quit"]').exists()).toBe(false);
    expect(wrapper.get('[data-action="self-update-close"]').exists()).toBe(true);
  });

  it('is hidden when show=false', () => {
    const wrapper = mountDialog({ show: false });
    expect(wrapper.find('[data-testid="self-update-dialog"]').exists()).toBe(false);
  });

  it.each(['update', 'ignore', 'quit'])('emits %s for an available startup update', async action => {
    const wrapper = mountDialog();
    await wrapper.get(`[data-action="self-update-${action}"]`).trigger('click');
    expect(wrapper.emitted(action)).toHaveLength(1);
  });

  it('locks submissions synchronously before parent busy props arrive', () => {
    const wrapper = mountDialog();
    const update = wrapper.get('[data-action="self-update-update"]').element;
    update.click(); update.click();
    wrapper.get('[data-action="self-update-ignore"]').element.click();
    wrapper.get('[data-action="self-update-close"]').element.click();
    expect(wrapper.emitted('update')).toHaveLength(1);
    expect(wrapper.emitted('ignore')).toBeUndefined();
    expect(wrapper.emitted('close')).toBeUndefined();
  });

  it('sets initial focus, wraps Tab/Shift+Tab, rejects outside focus and restores the trigger on close', async () => {
    const trigger = document.createElement('button'); document.body.append(trigger); trigger.focus();
    const wrapper = mountDialog({ source: 'manual', returnFocusTo: trigger });
    await flushPromises();
    const close = wrapper.get('[data-action="self-update-close"]').element;
    const update = wrapper.get('[data-action="self-update-update"]').element;
    expect(document.activeElement).toBe(close);
    const panel = wrapper.get('[role="dialog"]');
    await panel.trigger('keydown', { key: 'Tab', shiftKey: true });
    expect(document.activeElement).toBe(update);
    await panel.trigger('keydown', { key: 'Tab' });
    expect(document.activeElement).toBe(close);
    trigger.focus(); expect(document.activeElement).toBe(close);
    await panel.trigger('keydown', { key: 'Escape' });
    expect(wrapper.emitted('close')).toHaveLength(1);
    expect(wrapper.emitted('ignore')).toBeUndefined();
    await wrapper.setProps({ show: false });
    expect(document.activeElement).toBe(trigger);
  });

  it.each(['updating', 'checking', 'dismissing'])('%s disables every action and prevents Escape/mask dismissal, with a focusable busy fallback', async flag => {
    const wrapper = mountDialog(); await flushPromises();
    await wrapper.setProps({ [flag]: true });
    expect(wrapper.get('[role="dialog"]').attributes('aria-busy')).toBe('true');
    expect(wrapper.findAll('button').every(button => button.element.disabled)).toBe(true);
    for (const button of wrapper.findAll('button')) await button.trigger('click');
    await wrapper.get('[role="dialog"]').trigger('keydown', { key: 'Escape' });
    await wrapper.get('.self-update-mask').trigger('click');
    expect(wrapper.emitted('close')).toBeUndefined();
    expect(wrapper.emitted('update')).toBeUndefined();
    expect(wrapper.emitted('ignore')).toBeUndefined();
    expect(wrapper.emitted('quit')).toBeUndefined();
    await wrapper.get('[role="dialog"]').trigger('keydown', { key: 'Tab' });
    expect(document.activeElement).toBe(wrapper.get('[role="dialog"]').element);
    await wrapper.setProps({ [flag]: false });
    await wrapper.get('[role="dialog"]').trigger('keydown', { key: 'Escape' });
    expect(wrapper.emitted('close')).toHaveLength(1);
  });

  it('shows exact backend download bytes and percentage, plus each named verification/install phase', async () => {
    const wrapper = mountDialog({ updating: true, progress: { phase: 'downloading', downloadedBytes: 320, totalBytes: 1000, percent: 32 } });
    expect(wrapper.get('progress').attributes('value')).toBe('32');
    expect(wrapper.get('progress').attributes('aria-label')).toBe('self_update.download_progress');
    expect(wrapper.get('[data-testid="self-update-bytes"]').text()).toContain('320 B / 1000 B · 32%');
    for (const phase of ['verifying', 'extracting', 'installing', 'completed']) {
      await wrapper.setProps({ progress: { phase, downloadedBytes: 1000, totalBytes: 1000, percent: phase === 'completed' ? 100 : 99 } });
      expect(wrapper.get('[data-testid="self-update-progress"] [role="status"]').text()).toBe(`self_update.phase.${phase}`);
      expect(wrapper.find('[data-testid="self-update-restart"]').exists()).toBe(false);
      expect(wrapper.get('[data-action="self-update-close"]').element.disabled).toBe(true);
    }
  });

  it('uses indeterminate progress when total size is unknown and does not invent progress before a backend event', async () => {
    const wrapper = mountDialog({ updating: true });
    expect(wrapper.find('progress').exists()).toBe(false);
    expect(wrapper.text()).toContain('self_update.preparing');
    await wrapper.setProps({ progress: { phase: 'downloading', downloadedBytes: 320, totalBytes: 0, percent: 0 } });
    expect(wrapper.get('progress').attributes('value')).toBeUndefined();
    expect(wrapper.get('[data-testid="self-update-bytes"]').text()).toBe('320 B');
  });

  it('keeps backend error text in an alert with retry and close, and does not offer a fake green status', async () => {
    const wrapper = mountDialog({ status: 'error', error: { kind: 'apply', message: 'checksum mismatch\ntry again' } });
    expect(wrapper.get('[role="alert"]').text()).toContain('checksum mismatch\ntry again');
    expect(wrapper.text()).not.toContain('up_to_date');
    expect(wrapper.get('[role="dialog"]').attributes('aria-busy')).toBe('false');
    expect(wrapper.find('[data-action="self-update-update"]').exists()).toBe(false);
    await wrapper.get('[data-action="self-update-retry"]').trigger('click');
    expect(wrapper.emitted('retry')).toHaveLength(1);
    await wrapper.setProps({ checking: true });
    expect(wrapper.get('[data-action="self-update-retry"]').element.disabled).toBe(true);
  });

  it('shows a restart hint only on applied, and no installation-success action for an up-to-date refetch', async () => {
    const wrapper = mountDialog({ status: 'up-to-date', source: 'manual' });
    expect(wrapper.text()).toContain('self_update.no_update_applied');
    expect(wrapper.text()).not.toContain('self_update.applied{');
    expect(wrapper.find('[data-action="self-update-update"]').exists()).toBe(false);
    await wrapper.setProps({ status: 'applied' });
    expect(wrapper.get('[data-testid="self-update-restart"]').text()).toBe('self_update.restart_hint');
    expect(wrapper.find('[data-action="self-update-quit"]').exists()).toBe(false);
    expect(wrapper.find('[data-action="self-update-update"]').exists()).toBe(false);
  });

  it('displays the detected platform and package, with only its archive behind the manual disclosure', async () => {
    const wrapper = mountDialog({ ...platformRelease, source: 'manual' });
    expect(wrapper.text()).toContain('windows/amd64');
    expect(wrapper.text()).toContain(assetName);
    expect(wrapper.text()).toContain('self_update.windows_zip');
    expect(wrapper.findAll('a')).toHaveLength(0);
    const disclosure = wrapper.get('[data-action="self-update-manual-download"]');
    expect(disclosure.attributes('aria-expanded')).toBe('false');
    await disclosure.trigger('click');
    expect(disclosure.attributes('aria-expanded')).toBe('true');
    expect(wrapper.get(`#${disclosure.attributes('aria-controls')}`).text()).toContain('self_update.manual_warning');
    expect(wrapper.findAll('a')).toHaveLength(1);
    expect(wrapper.get('a').attributes()).toMatchObject({ href: assetUrl, target: '_blank', rel: 'noopener noreferrer' });
    expect(wrapper.emitted('update')).toBeUndefined();
    await disclosure.trigger('click');
    expect(wrapper.findAll('a')).toHaveLength(0);
  });

  it.each([{}, { platform: 'windows/amd64' }, { assetName }, { ...platformRelease, assetName: 'missing.zip' },
    { ...platformRelease, downloadUrls: { [assetName]: 'javascript:alert(1)' } },
    { ...platformRelease, downloadUrls: { [assetName]: 'not a URL' } },
  ])('keeps automatic update usable but hides unproven or unsafe links: %j', async props => {
    const wrapper = mountDialog({ downloadUrls: platformRelease.downloadUrls, ...props });
    expect(wrapper.find('[data-action="self-update-manual-download"]').exists()).toBe(false);
    expect(wrapper.findAll('a')).toHaveLength(0);
    await wrapper.get('[data-action="self-update-update"]').trigger('click');
    expect(wrapper.emitted('update')).toHaveLength(1);
  });

  it('resets the manual disclosure across apply failures, other packages and reopen', async () => {
    const wrapper = mountDialog(platformRelease);
    const toggle = () => wrapper.get('[data-action="self-update-manual-download"]');
    await toggle().trigger('click');
    await wrapper.setProps({ updating: true });
    expect(wrapper.findAll('a')).toHaveLength(0);
    await wrapper.setProps({ updating: false, error: { kind: 'apply', message: 'Could not download' } });
    expect(toggle().attributes('aria-expanded')).toBe('false');
    expect(wrapper.findAll('a')).toHaveLength(0);
    await toggle().trigger('click');
    expect(wrapper.get('a').attributes('href')).toBe(assetUrl);
    await wrapper.setProps({ latest: '1.0.0-beta.3' });
    expect(toggle().attributes('aria-expanded')).toBe('false');
    await toggle().trigger('click');
    await wrapper.setProps({ show: false });
    await wrapper.setProps({ show: true });
    expect(toggle().attributes('aria-expanded')).toBe('false');
    await wrapper.setProps({ error: { kind: 'check', message: 'No matching asset' } });
    expect(wrapper.find('[data-action="self-update-manual-download"]').exists()).toBe(false);
  });

  it('removes the focus trap on unmount', async () => {
    const trigger = document.createElement('button'); document.body.append(trigger);
    const wrapper = mountDialog({ returnFocusTo: trigger });
    await flushPromises();
    wrapper.unmount(); wrappers.splice(wrappers.indexOf(wrapper), 1);
    trigger.focus(); expect(document.activeElement).toBe(trigger);
  });
});
