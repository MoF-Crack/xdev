import { beforeEach, expect, it, vi } from 'vitest';
import en from '../src/i18n/locales/en';
import zh from '../src/i18n/locales/zh';

beforeEach(() => localStorage.clear());

// 守卫：en/zh 键集必须完全一致——任何一侧新增/遗漏翻译键都会在此失败，
// 而不是在运行时退回显示原始键名（t() 的 fallback 掩盖了不一致）。
it('keeps the en and zh locale key sets in parity', () => {
  const enKeys = new Set(Object.keys(en));
  const zhKeys = new Set(Object.keys(zh));
  const missingInZh = [...enKeys].filter((k) => !zhKeys.has(k));
  const missingInEn = [...zhKeys].filter((k) => !enKeys.has(k));
  expect(missingInZh, `keys present in en but missing in zh: ${missingInZh.join(', ')}`).toEqual([]);
  expect(missingInEn, `keys present in zh but missing in en: ${missingInEn.join(', ')}`).toEqual([]);
});

// 守卫：占位符参数集必须一致——{tool} 在 en 有而 zh 无会导致中文静默显示未替换占位符。
it('keeps placeholder sets in parity between en and zh', () => {
  const placeholders = (s) => (String(s).match(/\{[a-zA-Z_]+\}/g) || []).sort().join(',');
  const problems = [];
  for (const key of Object.keys(en)) {
    const a = placeholders(en[key]);
    const b = placeholders(zh[key]);
    if (a !== b) problems.push(`${key}: en=[${a}] zh=[${b}]`);
  }
  expect(problems, `placeholder mismatches:\n${problems.join('\n')}`).toEqual([]);
});

it('keeps the document language synchronized on initialization and locale changes', async () => {
  localStorage.setItem('locale', 'zh');
  vi.resetModules();
  const module = await import('../src/i18n/index.ts');
  expect(document.documentElement.lang).toBe('zh-CN');
  module.useI18n().setLocale('en');
  expect(document.documentElement.lang).toBe('en');
});
