import { beforeEach, expect, it, vi } from 'vitest';
import { readdirSync, readFileSync, statSync } from 'node:fs';
import { join } from 'node:path';
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

// Ticket 08 (audit-fixes / P1-36)：全量 t() 字面键扫描守卫。
//
// 扫描 frontend/src/**/*.{vue,js,ts} 中所有静态 t('...') / t("...") 调用的
// 字面键，要求每个键都存在于 en 与 zh 两个 locale。缺键的后果是原始键名
// 直接上屏（且带 . 的键名会破坏 aria-label）——历史缺过 7 键
// （settings.check_update 等），此守卫防止复发。
//
// 动态键（模板字面量如 `about.${k}`）无法静态扫描，不在守卫范围。
it('resolves every static t() literal key used in src against both locales', () => {
  const srcDir = join(__dirname, '..', 'src');
  const used = new Map(); // key -> first location
  const walk = (dir) => {
    for (const entry of readdirSync(dir)) {
      const full = join(dir, entry);
      if (statSync(full).isDirectory()) {
        walk(full);
        continue;
      }
      if (!/\.(vue|js|ts)$/.test(entry)) continue;
      if (entry.endsWith('.d.ts')) continue;
      const rel = full.replace(srcDir + /[\\/]/, '');
      const text = readFileSync(full, 'utf8');
      const re = /\bt\(\s*(['"])((?:[^'"\\])+)\1/g;
      let m;
      while ((m = re.exec(text))) {
        // Keys ending in '.' or '_' are dynamic-concatenation prefixes
        // (e.g. t('form.' + f), t('detail.deps_reason_' + code)) — the
        // full keys are dynamic and cannot be checked statically here.
        // Excluded by design; their expansions are covered by the locale
        // parity test instead.
        if (/[._]$/.test(m[2])) continue;
        if (!used.has(m[2])) used.set(m[2], rel);
      }
    }
  };
  walk(srcDir);

  expect(used.size, 'the scan must find t() usages — guard broken if empty').toBeGreaterThan(0);

  const enKeys = new Set(Object.keys(en));
  const zhKeys = new Set(Object.keys(zh));
  const missing = [...used.entries()]
    .filter(([key]) => !enKeys.has(key) || !zhKeys.has(key))
    .map(([key, where]) => `${key} (first used in ${where})`);
  expect(missing, `t() literal keys missing from locales:\n${missing.join('\n')}`).toEqual([]);
});

it('keeps the document language synchronized on initialization and locale changes', async () => {
  localStorage.setItem('locale', 'zh');
  vi.resetModules();
  const module = await import('../src/i18n/index.ts');
  expect(document.documentElement.lang).toBe('zh-CN');
  module.useI18n().setLocale('en');
  expect(document.documentElement.lang).toBe('en');
});
