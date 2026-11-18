import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { describe, expect, it } from 'vitest';
import { MAIN_THEMES, THEMES } from '../src/theme/themes.js';
import { getNaiveThemeOverrides } from '../src/theme/naive-theme.js';

const root = dirname(dirname(fileURLToPath(import.meta.url)));
const css = readFileSync(join(root, 'src/styles/theme.css'), 'utf8');
function tokensFor(name) {
  const block = css.match(new RegExp(`\\[data-theme="${name}"\\]\\s*\\{([\\s\\S]*?)\\}`))?.[1] ?? '';
  return Object.fromEntries([...block.matchAll(/--xdev-([\w-]+):\s*([^;]+);/g)].map((m) => [m[1], m[2].trim()]));
}

// WCAG relative luminance, not perceived-brightness or rounded ratios.
// Only the explicit text/background pairs below are tested. Decorative lines,
// focus rings, disabled content and shadows are not ordinary body text.
function luminance(hex) {
  if (!/^#[a-f\d]{6}$/i.test(hex)) throw new Error(`Expected an opaque six-digit text/background color, got ${hex}`);
  const linear = hex.slice(1).match(/../g).map((channel) => {
    const c = parseInt(channel, 16) / 255;
    return c <= 0.04045 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4;
  });
  return linear.reduce((sum, channel, i) => sum + channel * [0.2126, 0.7152, 0.0722][i], 0);
}
function contrast(a, b) {
  const values = [luminance(a), luminance(b)].sort((x, y) => y - x);
  return (values[0] + 0.05) / (values[1] + 0.05);
}
function expectAA(foreground, background, context) {
  const ratio = contrast(foreground, background);
  expect(ratio, `${context}: ${foreground} on ${background} = ${ratio.toFixed(3)}:1`).toBeGreaterThanOrEqual(4.5);
}

describe('theme text contrast', () => {
  it('uses the standard luminance thresholds and does not round a failure up', () => {
    expect(contrast('#000000', '#ffffff')).toBe(21);
    expect(contrast('#ffffff', '#ffffff')).toBe(1);
    expect(contrast('#777777', '#ffffff')).toBeLessThan(4.5);
    expect(contrast('#767676', '#ffffff')).toBeGreaterThan(4.5);
  });

  it.each(THEMES)('$value CSS primary button text stays AA in default, hover and pressed states', ({ value }) => {
    const t = tokensFor(value);
    for (const state of ['accent', 'accent-hover', 'accent-pressed']) expectAA(t['text-on-accent'], t[state], `${value}.${state}`);
  });

  it.each(THEMES)('$value Naive primary button text stays AA through every interaction state', ({ value }) => {
    const { Button } = getNaiveThemeOverrides(value);
    for (const state of ['', 'Hover', 'Pressed', 'Focus']) expectAA(Button[`textColor${state}Primary`], Button[`color${state}Primary`], `${value}.Button.${state || 'default'}`);
  });

  it.each(MAIN_THEMES)('$value primary and secondary body text stay AA on the four main surfaces', ({ value }) => {
    const t = tokensFor(value);
    for (const text of ['text-primary', 'text-secondary']) {
      for (const surface of ['bg-app', 'bg-panel', 'bg-card', 'bg-elevated']) expectAA(t[text], t[surface], `${value}.${text}/${surface}`);
    }
    expectAA(t['text-muted'], t['bg-card'], `${value}.muted card caption`);
    expectAA(t.accent, t['accent-soft'], `${value}.active label`);
    expectAA(t.success, t['success-soft'], `${value}.success badge`);
    expectAA(t.warning, t['warning-soft'], `${value}.warning badge`);
  });
});
