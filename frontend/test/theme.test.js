import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { describe, expect, it } from 'vitest';
import { darkTheme, lightTheme } from 'naive-ui';
import { getNaiveThemeConfig, getNaiveThemeOverrides } from '../src/theme/naive-theme.js';
import { DEFAULT_THEME, THEMES, THEME_VALUES, MAIN_THEMES, LEGACY_THEMES, getThemeMeta, isKnownTheme } from '../src/theme/themes.js';

const root = dirname(dirname(fileURLToPath(import.meta.url)));
const read = (path) => readFileSync(join(root, path), 'utf8');
const themeCss = read('src/styles/theme.css');

// Borders, shadows and disabled text are part of the complete theme contract,
// but are not body-text contrast targets. See theme.contrast.test.js for the
// explicitly selected foreground/background pairs and their WCAG thresholds.
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
  'content-max-width', 'reading-max-width', 'table-max-width', 'content-padding', 'section-gap', 'page-top-space',
  'radius-sm', 'radius-md', 'radius-lg', 'radius-pill',
  'font-ui', 'font-mono', 'font-size-xs', 'font-size-sm', 'font-size-md', 'font-size-lg',
  'motion-fast', 'motion-normal', 'motion-slow', 'ease-standard', 'shadow-soft',
];

function themeBlock(name) {
  return name === ':root'
    ? themeCss.match(/:root\s*\{([\s\S]*?)\}/)?.[1] ?? ''
    : themeCss.match(new RegExp(`\\[data-theme=["']${name}["']\\]\\s*\\{([\\s\\S]*?)\\}`))?.[1] ?? '';
}
function tokensFor(name) {
  return Object.fromEntries([...themeBlock(name).matchAll(/--xdev-([\w-]+)\s*:\s*([^;]+);/g)].map((match) => [match[1], match[2].trim()]));
}

// Fixed review fixtures from prototype.html :root / light / dark. Never read
// the external design file at test time: this must also run in a clean checkout.
const approved = {
  'eye-care': {
    'bg-app': '#e6ede7', 'bg-panel': '#dce6df', 'bg-card': '#f3f6ef', 'bg-elevated': '#eaf0e8',
    'text-primary': '#223b33', 'text-secondary': '#52675d', 'text-muted': '#5e7167',
    accent: '#326b58', 'accent-hover': '#285b49', 'accent-soft': '#d8e7dc', 'text-on-accent': '#ffffff',
    'border-subtle': '#cad7ce', 'border-strong': '#a8baae', success: '#28624e', 'success-soft': '#dceae0',
    warning: '#82531f', 'warning-soft': '#f0e4d0', error: '#ab444d', info: '#756583', 'shadow-md': '0 3px 12px #263d3010',
  },
  light: {
    'bg-app': '#eef2fa', 'bg-panel': '#f8f9fe', 'bg-card': '#ffffff', 'bg-elevated': '#f4f6fd',
    'text-primary': '#252c44', 'text-secondary': '#576780', 'text-muted': '#64718a',
    accent: '#5655cd', 'accent-hover': '#4444b5', 'accent-soft': '#ecebfc', 'text-on-accent': '#ffffff',
    'border-subtle': '#dde3f0', 'border-strong': '#b4bfd6', success: '#21715a', 'success-soft': '#e5f2ed',
    warning: '#90601e', 'warning-soft': '#faf0dc', error: '#bd3d52', info: '#127d8b', 'shadow-md': '0 3px 12px #34447808',
  },
  dark: {
    'bg-app': '#101522', 'bg-panel': '#151c2b', 'bg-card': '#1b2435', 'bg-elevated': '#222d42',
    'text-primary': '#e6edfb', 'text-secondary': '#acb9d0', 'text-muted': '#96a6c0',
    accent: '#a4adff', 'accent-hover': '#b7beff', 'accent-soft': '#2b3557', 'text-on-accent': '#1b2040',
    'border-subtle': '#303d53', 'border-strong': '#62718d', success: '#84cfb3', 'success-soft': '#223e3b',
    warning: '#e7bd78', 'warning-soft': '#3c332a', error: '#ff96a6', info: '#83d2d1', 'shadow-md': '0 3px 14px #03081628',
  },
};

describe('theme system', () => {
  it('defines the complete semantic contract in every explicit theme and :root', () => {
    for (const name of [':root', ...THEME_VALUES]) {
      expect(themeBlock(name), `missing explicit ${name} block`).not.toBe('');
      const tokens = tokensFor(name);
      expect(semanticTokens.filter((token) => !(token in tokens)), `missing ${name} tokens`).toEqual([]);
    }
    expect(foundationTokens.filter((token) => !(token in tokensFor(':root')))).toEqual([]);
  });

  it.each(Object.entries(approved))('matches the approved %s preview exactly', (name, expected) => {
    expect(tokensFor(name)).toMatchObject(expected);
    expect(tokensFor(name)['bg-hover']).toBe(expected['bg-elevated']);
    expect(tokensFor(name)['bg-muted']).toBe(expected['bg-elevated']);
    expect(tokensFor(name)['warning-bg']).toBe(expected['warning-soft']);
    expect(tokensFor(name)['warning-text']).toBe(expected.warning);
  });

  it('mirrors default semantic tokens in :root and the explicit dark block', () => {
    const defaults = tokensFor(':root');
    const dark = tokensFor(DEFAULT_THEME);
    for (const token of semanticTokens) expect(defaults[token], token).toBe(dark[token]);
  });

  it('offers the three main themes first without removing persisted classic names', () => {
    expect(MAIN_THEMES.map(({ value }) => value)).toEqual(['eye-care', 'light', 'dark']);
    expect(LEGACY_THEMES.map(({ value }) => value)).toEqual(['midnight', 'nord', 'ember']);
    expect(THEME_VALUES).toEqual(['eye-care', 'light', 'dark', 'midnight', 'nord', 'ember']);
    expect(new Set(THEME_VALUES).size).toBe(6);
    expect(DEFAULT_THEME).toBe('dark');
    for (const name of THEME_VALUES) {
      expect(isKnownTheme(name)).toBe(true);
      expect(getThemeMeta(name).value).toBe(name);
    }
    for (const name of ['unknown', '__proto__', 'constructor', '', null, undefined]) {
      expect(isKnownTheme(name)).toBe(false);
      expect(getThemeMeta(name).value).toBe(DEFAULT_THEME);
      expect(getNaiveThemeConfig(name)).toBe(getNaiveThemeConfig(DEFAULT_THEME));
    }
  });

  it('retains classic palettes and their original family', () => {
    const expected = {
      midnight: ['#0a0e1a', '#0e1526', '#63c7e6', '#04222e'],
      nord: ['#2e3440', '#333b4a', '#88c0d0', '#1f2530'],
      ember: ['#15110c', '#1a1510', '#e6a552', '#1d1405'],
    };
    for (const { value, family, swatch } of LEGACY_THEMES) {
      expect([swatch.bg, swatch.panel, swatch.accent, swatch.onAccent]).toEqual(expected[value]);
      expect(family).toBe('dark');
    }
  });

  it('keeps every registry swatch in sync with its CSS semantic token', () => {
    const swatchToToken = { bg: 'bg-app', panel: 'bg-panel', card: 'bg-card', raised: 'bg-elevated', text: 'text-primary', secondary: 'text-secondary', muted: 'text-muted', accent: 'accent', soft: 'accent-soft', onAccent: 'text-on-accent' };
    for (const theme of THEMES) {
      const tokens = tokensFor(theme.value);
      for (const [key, token] of Object.entries(swatchToToken)) expect(theme.swatch[key], `${theme.value}.${key}`).toBe(tokens[token]);
      expect(themeBlock(theme.value)).toContain(`color-scheme: ${theme.family};`);
    }
  });

  it('maps Naive common semantics without leaking unrelated base palette colors', () => {
    const mapping = {
      primaryColor: 'accent', primaryColorHover: 'accent-hover', primaryColorPressed: 'accent-pressed', primaryColorSuppl: 'accent',
      textColorBase: 'text-primary', textColor1: 'text-primary', textColor2: 'text-secondary', textColor3: 'text-muted',
      textColorDisabled: 'text-dim', placeholderColor: 'text-muted', placeholderColorDisabled: 'text-dim',
      bodyColor: 'bg-app', cardColor: 'bg-card', modalColor: 'bg-card', popoverColor: 'bg-card',
      inputColor: 'bg-elevated', actionColor: 'bg-elevated', tableHeaderColor: 'bg-elevated',
      tableColor: 'bg-card', hoverColor: 'bg-hover', pressedColor: 'accent-soft', codeColor: 'bg-muted',
      borderColor: 'border-subtle', dividerColor: 'border-subtle', railColor: 'border-strong',
      progressRailColor: 'bg-muted', boxShadow1: 'shadow-md', boxShadow2: 'shadow-md', boxShadow3: 'shadow-md',
    };
    for (const { value, family } of THEMES) {
      const config = getNaiveThemeConfig(value);
      const tokens = tokensFor(value);
      expect(config.theme).toBe(family === 'light' ? lightTheme : darkTheme);
      expect(getNaiveThemeOverrides(value)).toBe(config.overrides);
      for (const [key, token] of Object.entries(mapping)) expect(config.overrides.common[key], `${value}.${key}`).toBe(tokens[token]);
      for (const kind of ['info', 'success', 'warning', 'error']) {
        for (const suffix of ['', 'Hover', 'Pressed', 'Suppl']) expect(config.overrides.common[`${kind}Color${suffix}`]).toBe(tokens[kind]);
      }
    }
  });

  it('uses supported Naive theme keys, including nested Select peer keys', () => {
    function checkThemeKeys(theme, overrides, common, context) {
      const self = theme.self?.(common) ?? {};
      for (const [key, value] of Object.entries(overrides)) {
        if (key === 'peers') {
          for (const [peer, peerOverrides] of Object.entries(value)) {
            expect(theme.peers, `${context}.peers.${peer}`).toHaveProperty(peer);
            checkThemeKeys(theme.peers[peer], peerOverrides, common, `${context}.peers.${peer}`);
          }
        } else {
          expect(self, `${context}.${key} is not a supported override`).toHaveProperty(key);
        }
      }
    }
    for (const { value } of THEMES) {
      const config = getNaiveThemeConfig(value);
      const common = { ...config.theme.common, ...config.overrides.common };
      for (const key of Object.keys(config.overrides.common)) expect(config.theme.common).toHaveProperty(key);
      for (const [component, overrides] of Object.entries(config.overrides)) {
        if (component === 'common') continue;
        expect(config.theme, `unrecognized component ${component}`).toHaveProperty(component);
        checkThemeKeys(config.theme[component], overrides, common, `${value}.${component}`);
      }
    }
  });

  it('maps Naive controls to actual theme keys in every interaction state', () => {
    for (const { value } of THEMES) {
      const t = tokensFor(value);
      const o = getNaiveThemeOverrides(value);
      for (const state of ['', 'Hover', 'Pressed', 'Focus', 'Disabled']) expect(o.Button[`textColor${state}Primary`]).toBe(t['text-on-accent']);
      for (const [state, token] of Object.entries({ '': 'accent', Hover: 'accent-hover', Pressed: 'accent-pressed', Focus: 'accent-hover', Disabled: 'accent' })) {
        expect(o.Button[`color${state}Primary`]).toBe(t[token]);
        expect(o.Button[`border${state}Primary`]).toBe(`1px solid ${t[token]}`);
      }
      expect(o.Input.color).toBe(t['bg-elevated']);
      expect(o.Input.border).toBe(`1px solid ${t['border-subtle']}`);
      expect(o.Input.boxShadowFocus).toBe(t['focus-ring']);
      expect(o.Select.peers.InternalSelection.colorActive).toBe(t['bg-elevated']);
      expect(o.Menu.itemColorActiveHover).toBe(t['accent-soft']);
      expect(o.Menu.itemColorHover).toBe(t['bg-hover']);
      expect(o.Menu.itemTextColor).toBe(t['text-secondary']);
      expect(o.DataTable.thColor).toBe(t['bg-elevated']);
      expect(o.DataTable.thTextColor).toBe(t['text-secondary']);
      expect(o.Switch.textColor).toBe(t['text-on-accent']);
      expect(o.Checkbox.checkMarkColor).toBe(t['text-on-accent']);
      expect(o.Progress.textColorLineInner).toBe(t['text-on-accent']);
      expect(o.Layout.siderColor).toBe(t['bg-panel']);
      expect(o.Alert.colorWarning).toBe(t['warning-soft']);
      expect(o.Tooltip.color).toBe(t['bg-card']);
      expect(o.Tooltip.textColor).toBe(t['text-secondary']);
      expect(o.Tag.colorBorderedWarning).toBe(t['warning-soft']);
      expect(o.Tag.textColorChecked).toBe(t['text-on-accent']);
    }
  });

  it('removes the legacy neon palette from application styles', () => {
    const applicationStyles = ['src/styles/theme.css', 'src/App.vue', 'src/AppContent.vue', 'src/SetupWizard.vue', 'index.html'].map(read).join('\n');
    const legacyNeon = /#(?:00e5ff|00f0ff|00b8d4|0097a7|00796b|00695c|00897b|00a08a|e0f7fa|b2dfdb|80cbc4|0a1628|0f2035|132840|17324f)|rgba\(0\s*,\s*(?:229|240|184|151|137|160|105)\s*,/gi;
    expect(applicationStyles.match(legacyNeon) ?? []).toEqual([]);
  });

  it('does not reference undefined theme variables in application styles', () => {
    const source = ['src/styles/theme.css', 'src/App.vue', 'src/AppContent.vue', 'src/SetupWizard.vue'].map(read).join('\n');
    const defined = new Set([...themeCss.matchAll(/--xdev-([\w-]+)\s*:/g)].map((match) => match[1]));
    const referenced = new Set([...source.matchAll(/var\(--xdev-([\w-]+)\b/g)].map((match) => match[1]));
    expect([...referenced].filter((token) => !defined.has(token))).toEqual([]);
  });

  it('disables non-essential motion when the user requests reduced motion', () => {
    expect(themeCss).toMatch(/@media\s*\(prefers-reduced-motion:\s*reduce\)/);
  });
});
