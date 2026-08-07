import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { describe, expect, it } from 'vitest';

const root = dirname(dirname(fileURLToPath(import.meta.url)));
const read = (path) => readFileSync(join(root, path), 'utf8');
const themeCss = read('src/styles/theme.css');

const semanticTokens = [
  'bg-app', 'bg-panel', 'bg-card', 'bg-elevated', 'bg-muted',
  'accent', 'accent-hover', 'accent-pressed', 'accent-soft',
  'success', 'success-soft', 'warning', 'warning-soft', 'error', 'error-soft', 'info', 'info-soft',
  'text-primary', 'text-secondary', 'text-muted', 'text-dim', 'text-on-accent',
  'border-subtle', 'border-strong', 'focus-ring',
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

describe('theme system', () => {
  it('defines the complete semantic contract for dark and light themes', () => {
    const rootBlock = themeCss.match(/:root\s*\{([\s\S]*?)\}/)?.[1] ?? '';
    const lightBlock = themeCss.match(/\[data-theme=["']light["']\]\s*\{([\s\S]*?)\}/)?.[1] ?? '';
    const eyeCareBlock = themeCss.match(/\[data-theme=["']eye-care["']\]\s*\{([\s\S]*?)\}/)?.[1] ?? '';

    const darkDeclarations = declarations(rootBlock);
    const lightDeclarations = declarations(lightBlock);
    expect([...semanticTokens, ...foundationTokens].filter((token) => !darkDeclarations.has(token))).toEqual([]);
    expect(semanticTokens.filter((token) => !lightDeclarations.has(token))).toEqual([]);
    expect(semanticTokens.filter((token) => !declarations(eyeCareBlock).has(token))).toEqual([]);
    expect(rootBlock).toContain('--xdev-accent: #58a6ff');
    expect(lightBlock).toContain('--xdev-accent: #2563eb');
  });

  it('exposes Naive UI overrides through one theme-based interface', async () => {
    const { getNaiveThemeConfig } = await import('../src/theme/naive-theme.js');
    const dark = getNaiveThemeConfig('dark').overrides;
    const light = getNaiveThemeConfig('light').overrides;
    const eyeCare = getNaiveThemeConfig('eye-care').overrides;

    expect(dark.common.primaryColor).toBe('#58a6ff');
    expect(light.common.primaryColor).toBe('#2563eb');
    expect(eyeCare.common.primaryColor).toBe('#5b8c5a');
    expect(dark.common.bodyColor).toBe('#0d1117');
    expect(light.common.bodyColor).toBe('#eef1f5');
    expect(getNaiveThemeConfig('unknown').overrides).toBe(dark);
  }, 15000);

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
