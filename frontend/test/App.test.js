import { flushPromises, mount } from '@vue/test-utils';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import App from '../src/App.vue';
import { cleanupWailsMocks, installWailsMocks } from './mocks/wails.js';

describe('App', () => {
  beforeEach(() => {
    installWailsMocks({ IsInitialized: vi.fn().mockResolvedValue(true) });
  });

  afterEach(cleanupWailsMocks);

  it('mounts and checks whether Wails is initialized', async () => {
    const wrapper = mount(App, {
      global: {
        stubs: {
          NConfigProvider: { template: '<div><slot /></div>' },
          NDialogProvider: { template: '<div><slot /></div>' },
          NMessageProvider: { template: '<div><slot /></div>' },
          SetupWizard: { template: '<div>Setup wizard</div>' },
          AppContent: { template: '<main>Application content</main>' },
        },
      },
    });

    await flushPromises();

    expect(window.go.gui.App.IsInitialized).toHaveBeenCalledOnce();
    expect(wrapper.text()).toContain('Application content');
  });

  it('does not mount application subscriptions before the initialization check settles', async () => {
    let resolve;
    installWailsMocks({ IsInitialized: () => new Promise(done => { resolve = done; }) });
    const wrapper = mount(App, { global: { stubs: { AppContent: { template: '<main data-app-content>App</main>' }, SetupWizard: { template: '<main data-setup>Setup</main>' } } } });
    expect(wrapper.find('[data-app-content]').exists()).toBe(false);
    expect(wrapper.find('[aria-busy="true"]').exists()).toBe(true);
    resolve(false);
    await flushPromises();
    expect(wrapper.find('[data-setup]').exists()).toBe(true);
    expect(wrapper.find('[data-app-content]').exists()).toBe(false);
    wrapper.unmount();
  });

  it('shows the setup wizard when Wails is not initialized', async () => {
    installWailsMocks({ IsInitialized: vi.fn().mockResolvedValue(false) });
    const wrapper = mount(App, {
      global: {
        stubs: {
          NConfigProvider: { template: '<div><slot /></div>' },
          NDialogProvider: { template: '<div><slot /></div>' },
          NMessageProvider: { template: '<div><slot /></div>' },
          SetupWizard: { template: '<div>Setup wizard</div>' },
          AppContent: { template: '<main>Application content</main>' },
        },
      },
    });

    await flushPromises();

    expect(wrapper.text()).toContain('Setup wizard');
  });
});
