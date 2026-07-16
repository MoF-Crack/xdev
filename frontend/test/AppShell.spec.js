import { mount } from '@vue/test-utils';
import { describe, expect, it, vi } from 'vitest';

import AppShell from '../src/layouts/AppShell.vue';

const groups = [{
  key: 'tools-group',
  label: 'Tools',
  items: [{ key: 'dashboard', label: 'Dashboard', searchable: false }],
}];

function mountShell(props = {}) {
  return mount(AppShell, {
    props: {
      selectedView: 'dashboard',
      navigationGroups: groups,
      minimize: vi.fn(),
      toggleMaximize: vi.fn(),
      close: vi.fn(),
      ...props,
    },
    slots: { default: '<main>Page content</main>' },
    global: {
      stubs: {
        NLayout: { template: '<div><slot /></div>' },
        NLayoutContent: { template: '<div><slot /></div>' },
      },
    },
  });
}

describe('AppShell', () => {
  it('forwards accessible window controls and title-bar double click', async () => {
    const minimize = vi.fn();
    const toggleMaximize = vi.fn();
    const close = vi.fn();
    const wrapper = mountShell({ minimize, toggleMaximize, close });

    await wrapper.get('[aria-label="Minimize"]').trigger('click');
    await wrapper.get('[aria-label="Maximize"]').trigger('click');
    await wrapper.get('[aria-label="Close"]').trigger('click');
    await wrapper.get('[data-testid="title-bar"]').trigger('dblclick');

    expect(minimize).toHaveBeenCalledOnce();
    expect(toggleMaximize).toHaveBeenCalledTimes(2);
    expect(close).toHaveBeenCalledOnce();
  });

  it.each(['Minimize', 'Maximize', 'Close'])('does not treat a double click on %s as a title-bar double click', async (label) => {
    const toggleMaximize = vi.fn();
    const wrapper = mountShell({ toggleMaximize });

    await wrapper.get(`[aria-label="${label}"]`).trigger('dblclick');

    expect(toggleMaximize).not.toHaveBeenCalled();
  });

  it('emits sidebar selection and supports compact mode', async () => {
    const wrapper = mountShell();

    await wrapper.get('[data-view="dashboard"]').trigger('click');
    await wrapper.get('[aria-label="Collapse sidebar"]').trigger('click');

    expect(wrapper.emitted('select-view')).toEqual([['dashboard']]);
    expect(wrapper.classes()).toContain('app-shell--compact');
    expect(wrapper.get('[aria-label="Expand sidebar"]').exists()).toBe(true);
    expect(wrapper.get('[data-view="dashboard"]').attributes('aria-current')).toBe('page');
  });

  it('forces compact navigation below 800px and hides the ineffective expand control', async () => {
    const listeners = new Set();
    const media = {
      matches: true,
      addEventListener: vi.fn((_, listener) => listeners.add(listener)),
      removeEventListener: vi.fn((_, listener) => listeners.delete(listener)),
    };
    const matchMedia = vi.spyOn(window, 'matchMedia').mockReturnValue(media);

    const wrapper = mountShell();
    await wrapper.vm.$nextTick();

    expect(wrapper.classes()).toContain('app-shell--compact');
    expect(wrapper.find('[aria-label="Expand sidebar"]').exists()).toBe(false);
    expect(wrapper.get('.app-shell__main').attributes('style')).toBeUndefined();

    media.matches = false;
    listeners.forEach((listener) => listener(media));
    await wrapper.vm.$nextTick();
    expect(wrapper.get('[aria-label="Collapse sidebar"]').exists()).toBe(true);
    wrapper.unmount();
    matchMedia.mockRestore();
  });

  it('opens the command palette with Ctrl/Cmd+K and closes it with Escape', async () => {
    const wrapper = mountShell();

    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'k', ctrlKey: true }));
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'k', metaKey: true }));
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }));
    await wrapper.vm.$nextTick();

    expect(wrapper.emitted('open-command-palette')).toHaveLength(2);
    expect(wrapper.emitted('escape')).toHaveLength(1);
  });

  it('removes global shortcut listeners when unmounted', () => {
    const wrapper = mountShell();
    wrapper.unmount();

    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'k', ctrlKey: true }));

    expect(wrapper.emitted('open-command-palette')).toBeUndefined();
  });
});
