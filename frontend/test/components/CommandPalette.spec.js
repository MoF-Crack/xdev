import { mount } from '@vue/test-utils';
import { afterEach, describe, expect, it } from 'vitest';

import CommandPalette from '../../src/components/CommandPalette.vue';

const options = [
  { key: 'dashboard', label: 'Dashboard', group: 'General' },
  { key: 'settings', label: 'Settings', group: 'General' },
  { key: 'about', label: 'About', group: 'Help' },
];

function mountPalette(props = {}) {
  return mount(CommandPalette, {
    attachTo: document.body,
    props: {
      show: true,
      options,
      title: 'Command palette',
      searchLabel: 'Search commands, pages, and tools',
      emptyLabel: 'No matching commands',
      ...props,
    },
  });
}

afterEach(() => { document.body.innerHTML = ''; });

describe('CommandPalette', () => {
  it('focuses its named combobox and exposes the active option when opened', async () => {
    const wrapper = mountPalette({ show: false });
    await wrapper.setProps({ show: true });
    const input = wrapper.get('[role="combobox"]');

    expect(document.activeElement).toBe(input.element);
    expect(wrapper.get('[role="dialog"]').attributes('aria-label')).toBe('Command palette');
    expect(input.attributes('aria-controls')).toBe('command-palette-listbox');
    expect(input.attributes('aria-activedescendant')).toBe('command-option-dashboard');
    expect(wrapper.get('#command-option-dashboard').attributes('aria-selected')).toBe('true');
  });

  it('cycles with arrows, supports Home and End, and selects the active option', async () => {
    const wrapper = mountPalette();
    const input = wrapper.get('[role="combobox"]');

    await input.trigger('keydown', { key: 'ArrowUp' });
    expect(input.attributes('aria-activedescendant')).toBe('command-option-about');
    await input.trigger('keydown', { key: 'ArrowDown' });
    expect(input.attributes('aria-activedescendant')).toBe('command-option-dashboard');
    await input.trigger('keydown', { key: 'End' });
    expect(input.attributes('aria-activedescendant')).toBe('command-option-about');
    await input.trigger('keydown', { key: 'Home' });
    await input.trigger('keydown', { key: 'Enter' });

    expect(wrapper.emitted('select')).toEqual([['dashboard']]);
    expect(wrapper.emitted('update:show')).toEqual([[false]]);
  });

  it('keeps the active option valid after filtering and reports no results', async () => {
    const wrapper = mountPalette();
    const input = wrapper.get('[role="combobox"]');
    await input.trigger('keydown', { key: 'End' });
    await input.setValue('settings');

    expect(input.attributes('aria-activedescendant')).toBe('command-option-settings');
    expect(wrapper.findAll('[role="option"]')).toHaveLength(1);

    await input.setValue('missing');
    expect(input.attributes('aria-activedescendant')).toBeUndefined();
    expect(wrapper.get('[role="status"]').text()).toBe('No matching commands');
  });

  it('synchronizes the active option with pointer movement and closes on Escape', async () => {
    const wrapper = mountPalette();
    const input = wrapper.get('[role="combobox"]');
    await wrapper.get('#command-option-settings').trigger('mousemove');
    expect(input.attributes('aria-activedescendant')).toBe('command-option-settings');

    await input.trigger('keydown', { key: 'Escape' });
    expect(wrapper.emitted('update:show')).toEqual([[false]]);
  });

  it('keeps Tab and programmatic focus inside until unmounted', async () => {
    const outside = document.createElement('button');
    document.body.append(outside);
    const wrapper = mountPalette();
    const input = wrapper.get('[role="combobox"]');

    await input.trigger('keydown', { key: 'Tab' });
    expect(document.activeElement).toBe(input.element);
    await input.trigger('keydown', { key: 'Tab', shiftKey: true });
    expect(document.activeElement).toBe(input.element);
    outside.focus();
    expect(document.activeElement).toBe(input.element);

    wrapper.unmount();
    outside.focus();
    expect(document.activeElement).toBe(outside);
  });

  it('selects the filtered active command with mouse and exposes focus visibility', async () => {
    const wrapper = mountPalette();
    const input = wrapper.get('[role="combobox"]');
    input.element.focus();
    await wrapper.vm.$nextTick();
    await input.setValue('settings');
    const option = wrapper.get('#command-option-settings');
    expect(wrapper.get('.command-palette__search').classes()).toContain('command-palette__search--focused');
    await option.trigger('pointerenter');
    await option.trigger('click');

    expect(wrapper.emitted('select')).toEqual([['settings']]);
  });
});
