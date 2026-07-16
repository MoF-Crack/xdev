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
