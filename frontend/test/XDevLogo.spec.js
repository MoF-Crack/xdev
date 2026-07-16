import { mount } from '@vue/test-utils';
import { describe, expect, it } from 'vitest';
import XDevLogo from '../src/components/XDevLogo.vue';

describe('XDevLogo', () => {
  it('can be decorative beside visible branding text', () => {
    const wrapper = mount(XDevLogo, { props: { decorative: true } });
    expect(wrapper.get('svg').attributes('aria-hidden')).toBe('true');
    expect(wrapper.get('svg').attributes('aria-label')).toBeUndefined();
  });

  it('retains its accessible name when it is the only branding', () => {
    const wrapper = mount(XDevLogo);
    expect(wrapper.get('svg').attributes('role')).toBe('img');
    expect(wrapper.get('svg').attributes('aria-label')).toBe('XDev');
  });
});
