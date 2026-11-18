import { readFileSync } from 'node:fs';
import { mount } from '@vue/test-utils';
import { describe, expect, it } from 'vitest';
import XDevLogo from '../src/components/XDevLogo.vue';
import {
  F2_DEV_PATH,
  F2_SYMBOL_PATHS,
  F2_WORDMARK_DEV_TRANSFORM,
  F2_WORDMARK_SYMBOL_TRANSFORM,
} from '../src/brand/f2.js';

// Independent approved outlines: do not replace these fixtures with imports.
const approvedSymbolPaths = [
  'M17 23h14c18 0 26 15 34 29l15 26c5 8 10 13 22 13h11v14H98c-18 0-26-13-34-28L49 51c-5-8-10-14-22-14H17Z',
  'M17 105V91h10c12 0 17-6 22-14l15-26c8-15 16-28 34-28h15v14h-11c-12 0-17 5-22 13L65 76c-8 14-16 29-34 29Z',
];
const approvedDevPath = 'M111 7v73H91c-13 0-20-9-20-23s7-22 20-22 20 8 20 22M140 57h38V47c0-8-7-13-18-13s-20 8-20 22v5c0 12 7 19 20 19h18M205 35l17 45h8l17-45';

const readBytes = (path) => readFileSync(new URL(path, import.meta.url));
const readAsset = (path) => readBytes(path).toString('utf8');
const parseAsset = (path) => new DOMParser().parseFromString(readAsset(path), 'image/svg+xml').documentElement;

function expectSingleXWordmark(svg) {
  expect(svg.getAttribute('viewBox')).toBe('-3 0 269 91');
  expect(svg.getAttribute('data-single-x')).toBe('true');
  expect(svg.querySelectorAll('[data-letter="x"]')).toHaveLength(1);
  expect(svg.querySelectorAll('[data-letters="dev"]')).toHaveLength(1);
  // One flowing x (two filled paths), followed only by the outlined d/e/v path.
  expect(Array.from(svg.querySelectorAll('path'), (path) => path.getAttribute('d'))).toEqual([
    ...approvedSymbolPaths,
    approvedDevPath,
  ]);
  expect(svg.querySelectorAll('text, use, image')).toHaveLength(0);
  expect(svg.textContent.trim()).toBe('');
  expect(svg.querySelector('[data-letter="x"]').getAttribute('transform')).toBe(
    'translate(-11.195121951219512 15.853658536585366) scale(0.6585365853658537)',
  );
  expect(svg.querySelector('[data-letters="dev"]').getAttribute('transform')).toBe('translate(8 0)');
  const dev = svg.querySelector('[data-letters="dev"] path');
  expect(dev.getAttribute('stroke')).toBe('currentColor');
  expect(dev.getAttribute('stroke-width')).toBe('10');
  expect(dev.getAttribute('stroke-linecap')).toBe('round');
  expect(dev.getAttribute('stroke-linejoin')).toBe('round');
}

describe('XDevLogo', () => {
  it('defaults to the transparent F2 symbol and exact approved outlines', () => {
    const wrapper = mount(XDevLogo);
    expect(wrapper.classes()).toContain('xdev-logo--symbol');
    expect(wrapper.attributes('viewBox')).toBe('0 0 128 128');
    expect(wrapper.findAll('path').map((path) => path.attributes('d'))).toEqual(approvedSymbolPaths);
    expect(wrapper.find('rect, circle, text, [data-letters="dev"]').exists()).toBe(false);
    expect(F2_SYMBOL_PATHS).toEqual(approvedSymbolPaths);
    expect(F2_DEV_PATH).toBe(approvedDevPath);
    expect(F2_WORDMARK_DEV_TRANSFORM).toBe('translate(8 0)');
    expect(F2_WORDMARK_SYMBOL_TRANSFORM).toBe('translate(-11.195121951219512 15.853658536585366) scale(0.6585365853658537)');
  });

  it('renders only one flowing x plus dev in the complete wordmark', () => {
    const wrapper = mount(XDevLogo, { props: { variant: 'wordmark' } });
    expect(wrapper.classes()).toContain('xdev-logo--wordmark');
    expectSingleXWordmark(wrapper.element);
  });

  it.each(['symbol', 'wordmark'])('can make %s decorative beside visible branding', async (variant) => {
    const wrapper = mount(XDevLogo, { props: { variant, decorative: true } });
    expect(wrapper.attributes('aria-hidden')).toBe('true');
    expect(wrapper.attributes('aria-label')).toBeUndefined();
    expect(wrapper.attributes('role')).toBeUndefined();
    expect(wrapper.attributes('focusable')).toBe('false');
    await wrapper.setProps({ decorative: false });
    expect(wrapper.attributes('aria-hidden')).toBeUndefined();
    expect(wrapper.attributes('role')).toBe('img');
    expect(wrapper.attributes('aria-label')).toBe('XDev');
  });

  it.each(['symbol', 'wordmark'])('retains one accessible name for standalone %s', (variant) => {
    const wrapper = mount(XDevLogo, { props: { variant } });
    expect(wrapper.attributes('role')).toBe('img');
    expect(wrapper.attributes('aria-label')).toBe('XDev');
    expect(wrapper.attributes('aria-hidden')).toBeUndefined();
    expect(wrapper.attributes('focusable')).toBe('false');
    expect(wrapper.findAll('[aria-label]')).toHaveLength(1);
  });

  it('switches variants without retaining duplicate lettering', async () => {
    const wrapper = mount(XDevLogo);
    await wrapper.setProps({ variant: 'wordmark' });
    expectSingleXWordmark(wrapper.element);
    await wrapper.setProps({ variant: 'symbol' });
    expect(wrapper.findAll('path')).toHaveLength(2);
    expect(wrapper.find('[data-letters="dev"]').exists()).toBe(false);
  });

  it('keeps the symbol on accent and dev on text/currentColor, not accent', () => {
    const source = readAsset('../src/components/XDevLogo.vue');
    expect(source).toMatch(/\.xdev-logo__symbol\s*\{\s*color:\s*var\(--xdev-accent,\s*#5655CD\)/);
    expect(source).toMatch(/\.xdev-logo__dev\s*\{\s*color:\s*var\(--xdev-text-primary,\s*currentColor\)/);
  });
});

describe('F2 reusable static assets', () => {
  it.each([
    '../src/assets/logo.svg',
    '../../docs/assets/brand/xdev-wordmark.svg',
  ])('preserves the exact single-x wordmark in %s', (path) => {
    const svg = parseAsset(path);
    expectSingleXWordmark(svg);
    expect(svg.getAttribute('aria-label')).toBe('XDev');
    expect(svg.querySelector('[data-letter="x"]').getAttribute('style')).toContain('var(--xdev-accent,#5655CD)');
    expect(svg.getAttribute('style')).toContain('var(--xdev-text-primary,#252C44)');
  });

  it('provides the same transparent F2 symbol for documentation', () => {
    const svg = parseAsset('../../docs/assets/brand/xdev-symbol.svg');
    expect(svg.getAttribute('viewBox')).toBe('0 0 128 128');
    expect(Array.from(svg.querySelectorAll('path'), (path) => path.getAttribute('d'))).toEqual(approvedSymbolPaths);
    expect(svg.querySelectorAll('rect, text')).toHaveLength(0);
  });

  it.each([
    '../src/assets/logo-small.svg',
    '../public/favicon.svg',
    '../../docs/assets/brand/xdev-app-icon.svg',
  ])('keeps %s fixed indigo/white with the approved app-icon geometry', (path) => {
    const svg = parseAsset(path);
    expect(svg.getAttribute('viewBox')).toBe('0 0 128 128');
    const rect = svg.querySelector('rect');
    expect(['x', 'y', 'width', 'height', 'rx', 'fill'].map((key) => rect.getAttribute(key))).toEqual([
      '4', '4', '120', '120', '28', '#5655CD',
    ]);
    const symbol = svg.querySelector('[data-letter="x"]');
    expect(symbol.getAttribute('transform')).toBe('translate(10 10) scale(.84)');
    expect(symbol.getAttribute('style')).toBe('color:#FFFFFF');
    expect(Array.from(symbol.querySelectorAll('path'), (item) => item.getAttribute('d'))).toEqual(approvedSymbolPaths);
    expect(svg.querySelectorAll('[data-letters="dev"], text')).toHaveLength(0);
    expect(readAsset(path)).not.toContain('var(');
  });

  it('ships the 512px PNG and all seven Windows icon sizes', () => {
    const png = readBytes('../../build/appicon.png');
    expect(png.subarray(0, 8).toString('hex')).toBe('89504e470d0a1a0a');
    expect(png.readUInt32BE(16)).toBe(512);
    expect(png.readUInt32BE(20)).toBe(512);
    const ico = readBytes('../../build/windows/icon.ico');
    expect(ico.readUInt16LE(0)).toBe(0);
    expect(ico.readUInt16LE(2)).toBe(1);
    expect(ico.readUInt16LE(4)).toBe(7);
    const sizes = Array.from({ length: 7 }, (_, index) => {
      const entry = 6 + index * 16;
      const width = ico[entry] || 256;
      const height = ico[entry + 1] || 256;
      expect(height).toBe(width);
      expect(ico.readUInt32LE(entry + 8)).toBeGreaterThan(0);
      expect(ico.readUInt32LE(entry + 12) + ico.readUInt32LE(entry + 8)).toBeLessThanOrEqual(ico.length);
      return width;
    });
    expect(sizes).toEqual([16, 24, 32, 48, 64, 128, 256]);
  });
});
