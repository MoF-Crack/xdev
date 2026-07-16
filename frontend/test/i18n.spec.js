import { beforeEach, expect, it, vi } from 'vitest';

beforeEach(() => localStorage.clear());

it('keeps the document language synchronized on initialization and locale changes', async () => {
  localStorage.setItem('locale', 'zh');
  vi.resetModules();
  const module = await import('../src/i18n/index.ts');
  expect(document.documentElement.lang).toBe('zh-CN');
  module.useI18n().setLocale('en');
  expect(document.documentElement.lang).toBe('en');
});
