import { flushPromises, mount } from '@vue/test-utils';
import { afterEach, describe, expect, it, vi } from 'vitest';

import SetupWizard from '../src/SetupWizard.vue';
import { cleanupWailsMocks, installWailsMocks } from './mocks/wails.js';

const drives = [
  { letter: 'C:', free: 10 * 1024 ** 3, total: 100 * 1024 ** 3 },
  { letter: 'D:', free: 80 * 1024 ** 3, total: 200 * 1024 ** 3 },
];

function mountWizard(overrides = {}) {
  const app = installWailsMocks({
    GetDrives: vi.fn().mockResolvedValue(drives),
    GetLastSelection: vi.fn().mockResolvedValue({}),
    SelectDirectory: vi.fn().mockResolvedValue('E:\\Development\\XDev'),
    InitConfig: vi.fn().mockResolvedValue(undefined),
    ...overrides,
  }).app;
  return { app, wrapper: mount(SetupWizard, { attachTo: document.body, global: { stubs: { NIcon: true } } }) };
}

afterEach(() => {
  document.body.innerHTML = '';
  cleanupWailsMocks();
  vi.restoreAllMocks();
});

describe('SetupWizard', () => {
  it('exposes three progress steps and marks the current step', async () => {
    const { wrapper } = mountWizard();
    await flushPromises();
    expect(wrapper.findAll('.setup-progress__step')).toHaveLength(3);
    expect(wrapper.get('[aria-current="step"]').text()).toContain('Installation location');
    expect(wrapper.find('main').exists()).toBe(true);
  });

  it('uses a labelled native radio group with full-row labels and native keyboard selection', async () => {
    const { wrapper } = mountWizard();
    await flushPromises();
    const fieldset = wrapper.get('fieldset');
    expect(fieldset.get('legend').text()).toBe('Select Installation Drive');
    const radios = wrapper.findAll('input[type="radio"]');
    expect(radios).toHaveLength(2);
    expect(radios[0].element.closest('label')).not.toBeNull();
    radios[1].element.focus();
    await radios[1].trigger('keydown', { key: ' ' });
    await radios[1].setValue(true);
    expect(radios[1].element.checked).toBe(true);
    expect(wrapper.text()).toContain('Avoid the system drive');
    expect(wrapper.text()).toContain('Recommended because it has more than 50 GB free');
  });

  it('restores a custom selection and keeps custom path, back, next, save, and done behavior', async () => {
    const path = 'D:\\deep\\custom\\development\\environment';
    const { app, wrapper } = mountWizard({ GetLastSelection: vi.fn().mockResolvedValue({ drive: 'D:', installDir: path }) });
    await flushPromises();
    expect(wrapper.get('[data-testid="selected-path"]').text()).toBe(path);
    expect(wrapper.find('input[type="radio"]:checked').exists()).toBe(false);
    await wrapper.get('[data-action="next"]').trigger('click');
    expect(wrapper.get('[aria-current="step"]').text()).toContain('Review');
    await wrapper.get('[data-action="back"]').trigger('click');
    await wrapper.get('[data-action="browse"]').trigger('click');
    await flushPromises();
    expect(wrapper.get('[data-testid="selected-path"]').text()).toBe('E:\\Development\\XDev');
    await wrapper.get('[data-action="next"]').trigger('click');
    await wrapper.get('[data-action="save"]').trigger('click');
    await flushPromises();
    expect(app.InitConfig).toHaveBeenCalledWith('E:\\Development\\XDev');
    expect(wrapper.get('[aria-current="step"]').text()).toContain('Complete');
    await wrapper.get('[data-action="done"]').trigger('click');
    expect(wrapper.emitted('done')).toHaveLength(1);
  });

  it.each([
    ['UNC path with no drives', [], { installDir: '\\\\server\\share\\XDev' }],
    ['unlisted drive', drives, { drive: 'Z:', installDir: 'Z:\\Custom\\XDev' }],
    ['custom path on a listed drive', drives, { drive: 'D:', installDir: 'D:\\Custom\\XDev' }],
  ])('restores %s as a custom path without selecting a radio', async (_, available, selection) => {
    const { wrapper } = mountWizard({
      GetDrives: vi.fn().mockResolvedValue(available),
      GetLastSelection: vi.fn().mockResolvedValue(selection),
    });
    await flushPromises();
    expect(wrapper.get('[data-testid="selected-path"]').text()).toBe(selection.installDir);
    expect(wrapper.find('input[type="radio"]:checked').exists()).toBe(false);
  });

  it('only restores an exact default drive path as a radio when that drive exists', async () => {
    const { wrapper } = mountWizard({ GetLastSelection: vi.fn().mockResolvedValue({ drive: 'D:', installDir: 'D:\\XDev' }) });
    await flushPromises();
    expect(wrapper.get('input[value="D:"]').element.checked).toBe(true);
    expect(wrapper.find('[data-testid="selected-path"]').exists()).toBe(false);
  });

  it('guards concurrent saves and calls InitConfig exactly once', async () => {
    let finishSave;
    const initConfig = vi.fn(() => new Promise((resolve) => { finishSave = resolve; }));
    const { wrapper } = mountWizard({ InitConfig: initConfig });
    await flushPromises();
    await wrapper.get('[data-action="next"]').trigger('click');
    const first = wrapper.vm.saveConfig();
    const second = wrapper.vm.saveConfig();
    expect(initConfig).toHaveBeenCalledTimes(1);
    finishSave();
    await Promise.all([first, second]);
  });

  it('moves focus to each new step heading after next, back, and save', async () => {
    const { wrapper } = mountWizard();
    await flushPromises();
    await wrapper.get('[data-action="next"]').trigger('click');
    await flushPromises();
    expect(document.activeElement).toBe(wrapper.get('[data-step-heading="2"]').element);
    await wrapper.get('[data-action="back"]').trigger('click');
    await flushPromises();
    expect(document.activeElement).toBe(wrapper.get('[data-step-heading="1"]').element);
    await wrapper.get('[data-action="next"]').trigger('click');
    await wrapper.get('[data-action="save"]').trigger('click');
    await flushPromises();
    expect(document.activeElement).toBe(wrapper.get('[data-step-heading="3"]').element);
  });

  it('shows localized load, browse, and save errors in live regions', async () => {
    const path = '\\\\server\\share\\tools';
    const { wrapper } = mountWizard({
      GetDrives: vi.fn().mockRejectedValue(new Error('offline')),
      GetLastSelection: vi.fn().mockResolvedValue({ installDir: path }),
    });
    await flushPromises();
    expect(wrapper.get('#setup-load-error').text()).toContain('Unable to load drives');
    expect(wrapper.get('[data-testid="selected-path"]').text()).toBe(path);
    expect(wrapper.get('[data-action="retry-drives"]').exists()).toBe(true);
  });

  it('focuses the selected radio after an explicit successful drive retry', async () => {
    const getDrives = vi.fn()
      .mockRejectedValueOnce(new Error('offline'))
      .mockResolvedValueOnce(drives);
    const { wrapper } = mountWizard({
      GetDrives: getDrives,
      GetLastSelection: vi.fn().mockResolvedValue({ installDir: 'D:\\XDev' }),
    });
    await flushPromises();
    await wrapper.get('[data-action="retry-drives"]').trigger('click');
    await flushPromises();
    expect(wrapper.get('input[value="D:"]').element.checked).toBe(true);
    expect(document.activeElement).toBe(wrapper.get('input[value="D:"]').element);
  });

  it('focuses the load error after an explicit failed drive retry', async () => {
    const getDrives = vi.fn().mockRejectedValue(new Error('offline'));
    const { wrapper } = mountWizard({ GetDrives: getDrives });
    await flushPromises();
    await wrapper.get('[data-action="retry-drives"]').trigger('click');
    await flushPromises();
    const error = wrapper.get('#setup-load-error');
    expect(error.attributes('tabindex')).toBe('-1');
    expect(document.activeElement).toBe(error.element);
  });

  it('does not steal focus when the initial drive load finishes', async () => {
    let resolveDrives;
    const externalButton = document.createElement('button');
    document.body.append(externalButton);
    const { wrapper } = mountWizard({ GetDrives: vi.fn(() => new Promise((resolve) => { resolveDrives = resolve; })) });
    externalButton.focus();
    resolveDrives(drives);
    await flushPromises();
    expect(document.activeElement).toBe(externalButton);
    wrapper.unmount();
  });

  it('shows loading and empty drive states', async () => {
    let resolveDrives;
    const { wrapper } = mountWizard({ GetDrives: vi.fn(() => new Promise((resolve) => { resolveDrives = resolve; })) });
    expect(wrapper.get('[role="status"]').text()).toContain('Loading drives');
    resolveDrives([]);
    await flushPromises();
    expect(wrapper.get('[role="status"]').text()).toContain('No drives available');
  });

  it('keeps save failures visible instead of only logging them', async () => {
    const { wrapper } = mountWizard({ InitConfig: vi.fn().mockRejectedValue(new Error('denied')) });
    await flushPromises();
    await wrapper.get('[data-action="next"]').trigger('click');
    await wrapper.get('[data-action="save"]').trigger('click');
    await flushPromises();
    const error = wrapper.get('#setup-action-error');
    expect(error.text()).toContain('Unable to save configuration');
    expect(document.activeElement).toBe(error.element);
    expect(wrapper.get('[data-action="save"]').attributes('aria-describedby')).toBe('setup-action-error');
  });

  it('focuses and describes browse failures', async () => {
    const { wrapper } = mountWizard({ SelectDirectory: vi.fn().mockRejectedValue(new Error('denied')) });
    await flushPromises();
    await wrapper.get('[data-action="browse"]').trigger('click');
    await flushPromises();
    const error = wrapper.get('#setup-action-error');
    expect(document.activeElement).toBe(error.element);
    expect(wrapper.get('[data-action="browse"]').attributes('aria-describedby')).toBe('setup-action-error');
  });
});
