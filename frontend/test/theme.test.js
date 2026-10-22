import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { describe, expect, it } from 'vitest';
import { getNaiveThemeConfig } from '../src/theme/naive-theme.js';
import { DEFAULT_THEME, THEMES, THEME_VALUES } from '../src/theme/themes.js';

const root = dirname(dirname(fileURLToPath(import.meta.url)));
const read = (path) => readFileSync(join(root, path), 'utf8');
const themeCss = read('src/styles/theme.css');
const naiveTheme = read('src/theme/naive-theme.js');

// Semantic tokens: every [data-theme] block (and :root) must define each of
// these. Guarded so a new token cannot silently fall back to another
// theme's value.
const semanticTokens = [
  'bg-app', 'bg-panel', 'bg-card', 'bg-elevated', 'bg-muted', 'bg-hover',
  'accent', 'accent-hover', 'accent-pressed', 'accent-soft',
  'success', 'success-soft', 'warning', 'warning-soft', 'warning-bg', 'warning-border', 'warning-text',
  'error', 'error-soft', 'info', 'info-soft',
  'text-primary', 'text-secondary', 'text-muted', 'text-dim', 'text-on-accent',
  'border-subtle', 'border-strong', 'focus-ring', 'overlay',
  'shadow-sm', 'shadow-md', 'shadow-lg',
];

const foundationTokens = [
  'space-1', 'space-2', 'space-3', 'space-4', 'space-5', 'space-6', 'space-8',
  'radius-sm', 'radius-md', 'radius-lg', 'radius-pill',
  'font-ui', 'font-mono', 'font-size-xs', 'font-size-sm', 'font-size-md', 'font-size-lg',
  'motion-fast', 'motion-normal', 'motion-slow', 'ease-standard',
];

function declarations(block) {
  return new Set([...block.matchAll(/--xdev-([\w-]+)\s*:/g)].map((match) => match[1]));
}

function themeBlock(name) {
  if (name === DEFAULT_THEME) {
    return themeCss.match(/:root\s*\{([\s\S]*?)\}/)?.[1] ?? '';
  }
  const block = themeCss.match(new RegExp(`\\[data-theme=["']${name}["']\\]\\s*\\{([\\s\\S]*?)\\}`))?.[1] ?? '';
  // the default theme is mirrored in an explicit [data-theme] block too
  if (!block && name === 'dark') {
    return themeCss.match(/:root\s*\{([\s\S]*?)\}/)?.[1] ?? '';
  }
  return block;
}

function cssVar(name, theme) {
  const match = themeBlock(theme).match(new RegExp(`--xdev-${name}:\\s*([^;]+);`));
  return match ? match[1].trim() : undefined;
}

describe('theme system', () => {
  it('defines the complete semantic contract for every registered theme', () => {
    expect(THEME_VALUES.length).toBeGreaterThanOrEqual(6);
    for (const name of THEME_VALUES) {
      const missing = semanticTokens.filter((token) => !declarations(themeBlock(name)).has(token));
      expect(missing, `theme "${name}" is missing: ${missing.join(', ')}`).toEqual([]);
    }
    const rootDeclarations = declarations(themeCss.match(/:root\s*\{([\s\S]*?)\}/)?.[1] ?? '');
    expect([...semanticTokens, ...foundationTokens].filter((token) => !rootDeclarations.has(token))).toEqual([]);
  });

  it('anchors the flagship palette values', () => {
    expect(cssVar('accent', 'dark')).toBe('#5c9cff');
    expect(cssVar('accent', 'light')).toBe('#2f6fed');
    expect(cssVar('accent', 'midnight')).toBe('#63c7e6');
    expect(cssVar('accent', 'nord')).toBe('#88c0d0');
    expect(cssVar('accent', 'ember')).toBe('#e6a552');
    expect(cssVar('accent', 'eye-care')).toBe('#4e7d4e');
  });

  it('exposes Naive UI overrides through one theme-based interface', () => {
    const dark = getNaiveThemeConfig('dark').overrides;
    const light = getNaiveThemeConfig('light').overrides;

    expect(dark.common.primaryColor).toBe('#5c9cff');
    expect(light.common.primaryColor).toBe('#2f6fed');
    expect(getNaiveThemeConfig('midnight').overrides.common.primaryColor).toBe('#63c7e6');
    expect(getNaiveThemeConfig('nord').overrides.common.primaryColor).toBe('#88c0d0');
    expect(getNaiveThemeConfig('ember').overrides.common.primaryColor).toBe('#e6a552');
    expect(getNaiveThemeConfig('eye-care').overrides.common.primaryColor).toBe('#4e7d4e');
    expect(dark.common.bodyColor).toBe('#0e1116');
    expect(light.common.bodyColor).toBe('#f2f4f8');
    expect(getNaiveThemeConfig('unknown').overrides).toBe(dark);
  });

  it('keeps filled primary controls readable: on-accent ink matches the palette family', () => {
    for (const theme of THEMES) {
      const onAccent = getNaiveThemeConfig(theme.value).overrides.Button.textColorPrimary;
      expect(onAccent, `${theme.value} naive onAccent`).toBe(theme.swatch.onAccent);
      expect(onAccent, `${theme.value} onAccent must be a deliberate ink choice`).toMatch(/^#/);
    }
  });

  it('keeps the registry, the CSS tokens and the Naive palettes in sync', () => {
    for (const theme of THEMES) {
      const block = themeBlock(theme.value);
      expect(block, `theme.css block for "${theme.value}"`).not.toBe('');
      expect(cssVar('bg-app', theme.value)).toBe(theme.swatch.bg);
      expect(cssVar('accent', theme.value)).toBe(theme.swatch.accent);
      expect(cssVar('text-on-accent', theme.value)).toBe(theme.swatch.onAccent);
      // every registered theme has a Naive palette entry (keys are plain
      // JS identifiers where possible, quoted when they contain hyphens)
      expect(naiveTheme).toMatch(new RegExp(`['"]?${theme.value}['"]?\\s*:\\s*\\{`));
    }
  });

  it('removes the legacy neon palette from application styles', () => {
    const applicationStyles = [
      'src/styles/theme.css', 'src/App.vue', 'src/AppContent.vue', 'src/SetupWizard.vue', 'index.html',
    ].map(read).join('\n');
    const legacyNeon = /#(?:00e5ff|00f0ff|00b8d4|0097a7|00796b|00695c|00897b|00a08a|e0f7fa|b2dfdb|80cbc4|0a1628|0f2035|132840|17324f)|rgba\(0\s*,\s*(?:229|240|184|151|137|160|105)\s*,/gi;

    expect(applicationStyles.match(legacyNeon) ?? []).toEqual([]);
  });

  it('does not reference undefined theme variables in application styles', () => {
    const files = ['src/styles/theme.css', 'src/App.vue', 'src/AppContent.vue', 'src/SetupWizard.vue'];
    const source = files.map(read).join('\n');
    const defined = new Set([...themeCss.matchAll(/--xdev-([\w-]+)\s*:/g)].map((match) => match[1]));
    const referenced = new Set([...source.matchAll(/var\(--xdev-([\w-]+)\b/g)].map((match) => match[1]));

    expect([...referenced].filter((token) => !defined.has(token))).toEqual([]);
  });

  it('disables non-essential motion when the user requests reduced motion', () => {
    expect(themeCss).toMatch(/@media\s*\(prefers-reduced-motion:\s*reduce\)/);
  });
});
