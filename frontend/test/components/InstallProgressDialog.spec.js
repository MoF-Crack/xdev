import { mount } from '@vue/test-utils';
import { afterEach, describe, expect, it, vi } from 'vitest';
import InstallProgressDialog from '../../src/components/InstallProgressDialog.vue';

const t = (key) => key;
afterEach(() => { document.body.innerHTML = ''; });
describe('InstallProgressDialog', () => {
  it('shows localized phase, percent, bytes, speed and controls while preventing active close', async () => {
    const wrapper = mount(InstallProgressDialog, { global: { stubs: { teleport: true } }, props: { show: true, active: true, paused: false, phase: 'Downloading', progress: { percent: 42, bytes: 420, totalBytes: 1000, speed: '2 MB/s' }, t } });
    expect(wrapper.text()).toContain('installing_downloading');
    expect(wrapper.text()).toContain('42%');
    expect(wrapper.text()).toContain('420 B / 1000 B');
    expect(wrapper.text()).toContain('2 MB/s');
    await wrapper.get('[data-action="close"]').trigger('click');
    expect(wrapper.emitted('update:show')).toBeUndefined();
    await wrapper.get('[data-action="pause"]').trigger('click');
    await wrapper.get('[data-action="cancel"]').trigger('click');
    expect(wrapper.emitted('pause')).toHaveLength(1);
    expect(wrapper.emitted('cancel')).toHaveLength(1);
  });

  it('renders the error message as text and offers a copy action', async () => {
    const writeText = vi.fn().mockResolvedValue(undefined);
    Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { writeText } });
    const wrapper = mount(InstallProgressDialog, { global: { stubs: { teleport: true } }, props: { show: true, phase: 'error', error: { message: 'Install failed', details: 'HTTP 503' }, t } });
    expect(wrapper.get('.install-error__message').text()).toContain('Install failed');
    await wrapper.get('[data-action="copy-error"]').trigger('click');
    await vi.waitFor(() => expect(writeText).toHaveBeenCalledWith('Install failed'));
  });

  // Ticket 08 (audit-fixes / P1-36): useInstall.js locally sets the
  // 'Cancelled', 'Paused' and 'error' phases after cancel/pause/failure —
  // the phaseKeyMap must map them to i18n keys instead of showing the
  // raw untranslated phase word.
  it.each([
    ['Cancelled', 'installing_cancelled'],
    ['Paused', 'installing_paused'],
    ['error', 'installing_error'],
  ])('localizes the %s phase through phaseKeyMap', (phase, key) => {
    const wrapper = mount(InstallProgressDialog, { global: { stubs: { teleport: true } }, props: { show: true, active: false, paused: phase === 'Paused', phase, t } });
    expect(wrapper.text()).toContain(key);
  });

  it('focuses an active control, traps focus, and cannot close while active', async () => {
    const trigger = document.createElement('button');
    document.body.append(trigger);
    trigger.focus();
    const wrapper = mount(InstallProgressDialog, { attachTo: document.body, props: { show: false, active: true, paused: false, t } });
    await wrapper.setProps({ show: true });
    const pause = wrapper.get('[data-action="pause"]');
    const background = wrapper.get('[data-action="background"]');

    expect(document.activeElement).toBe(pause.element);
    background.element.focus();
    await background.trigger('keydown', { key: 'Tab' });
    expect(document.activeElement).toBe(pause.element);
    trigger.focus();
    expect(document.activeElement).toBe(pause.element);
    await wrapper.get('[role="dialog"]').trigger('keydown', { key: 'Escape' });
    expect(wrapper.emitted('update:show')).toBeUndefined();
  });

  it('restores the pre-open trigger after completion closes the dialog', async () => {
    const trigger = document.createElement('button');
    document.body.append(trigger);
    trigger.focus();
    const wrapper = mount(InstallProgressDialog, { attachTo: document.body, props: { show: false, active: true, t } });
    await wrapper.setProps({ show: true });
    await wrapper.setProps({ active: false, show: false });
    expect(document.activeElement).toBe(trigger);
    wrapper.unmount();
  });
});
