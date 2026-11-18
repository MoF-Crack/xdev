import { mount } from '@vue/test-utils';
import { describe, expect, it } from 'vitest';
import QuickThemeSwitch from '../../src/components/QuickThemeSwitch.vue';

describe('QuickThemeSwitch', () => {
  it('exposes exactly the approved flagship themes with a pressed state', () => {
    const wrapper = mount(QuickThemeSwitch, { props: { theme: 'eye-care', t: key => key } });
    expect(wrapper.findAll('button').map(button => button.attributes('data-theme-select'))).toEqual(['eye-care', 'light', 'dark']);
    expect(wrapper.get('[data-theme-select="eye-care"]').attributes('aria-pressed')).toBe('true');
    expect(wrapper.get('[role="group"]').attributes('aria-label')).toBe('settings.theme');
  });
  it('emits the existing persisted theme identifier and reflects updates', async () => {
    const wrapper = mount(QuickThemeSwitch, { props: { theme: 'dark', t: key => key } });
    await wrapper.get('[data-theme-select="light"]').trigger('click');
    expect(wrapper.emitted('select')).toEqual([['light']]);
    await wrapper.setProps({ theme: 'light' });
    expect(wrapper.get('[data-theme-select="light"]').attributes('aria-pressed')).toBe('true');
    expect(wrapper.get('[data-theme-select="dark"]').attributes('aria-pressed')).toBe('false');
  });
});
