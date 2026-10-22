// 第八轮审计 · F-39 行为基线（记录修复后 t() 的实际语义，非缺陷断言）
//
// 这些用例**全部通过**，用途是把 F-39 修复后的真实行为固定下来，便于后续
// 重构（尤其是修 R8-03 的顺序依赖）时对照：
//   - 字面量替换：`$&` / `` $` `` / `$'` / `$$` / `$1` 原样上屏（F-39 的目标，已达成）；
//   - 非字符串参数走 `String()`：对象 → `[object Object]`、数组 → `1,2`、函数 → 源码；
//   - `null` / `undefined` → 空串（**行为变更**：修复前是字符串 `"null"` / `"undefined"`）；
//   - 未传入的键保留 `{count}`（既有行为，未变）；
//   - `params` 为 `null` / 数字 / 字符串 / 数组时不抛错（`if (params)` 已挡住）。

import { beforeEach, describe, expect, it, vi } from 'vitest';

const loadI18n = async (locale = 'en') => {
  localStorage.setItem('locale', locale);
  vi.resetModules();
  return await import('../src/i18n/index.ts');
};
beforeEach(() => localStorage.clear());

describe('F-39 行为基线（全绿）', () => {
  it('替换模式字符一律按字面量插入', async () => {
    const { t } = (await loadI18n('en')).useI18n();
    // 'status.version' = 'version: {version}'（占位符在串尾）
    for (const raw of ['$&', '$`', "$'", '$$', '$1', '$<name>']) {
      expect(t('status.version', { version: raw })).toBe(`version: ${raw}`);
    }
  });

  it('非字符串参数走 String() 语义', async () => {
    const { t } = (await loadI18n('en')).useI18n();
    expect(t('status.version', { version: { a: 1 } })).toBe('version: [object Object]');
    expect(t('status.version', { version: [1, 2] })).toBe('version: 1,2');
    expect(t('status.version', { version: false })).toBe('version: false');
    expect(t('status.version', { version: NaN })).toBe('version: NaN');
    expect(t('status.version', { version: 0 })).toBe('version: 0');
  });

  it('null / undefined 退化为空串（相对修复前的行为变更）', async () => {
    const { t } = (await loadI18n('en')).useI18n();
    expect(t('status.version', { version: null })).toBe('version: ');
    expect(t('status.version', { version: undefined })).toBe('version: ');
    // 修复前（字符串替换）分别是 'version: null' / 'version: undefined'。
    expect(t('status.version', { version: '' })).toBe('version: ');
  });

  it('未传入的键保留原样占位符（既有行为，未回归）', async () => {
    const { t } = (await loadI18n('en')).useI18n();
    expect(t('detail.versions_count', {})).toBe('{count} versions');
    expect(t('detail.versions_count')).toBe('{count} versions');
    expect(t('does.not.exist')).toBe('does.not.exist');
  });

  it('params 为非对象/null 时不抛错', async () => {
    const { t } = (await loadI18n('en')).useI18n();
    for (const p of [null, 0, 42, 'version', ['version'], undefined]) {
      expect(() => t('status.version', p)).not.toThrow();
    }
    expect(t('status.version', null)).toBe('version: {version}');
  });

  it('占位符在文案中部时不会复制后半段文案（F-39 的核心场景）', async () => {
    const { t } = (await loadI18n('en')).useI18n();
    const tail = ' is incomplete (missing binaries). Reinstall the same version to repair it?';
    expect(t('confirm_repair_content', { tool: "$'", version: '1.0.0' }))
      .toBe(`The installation of $'@1.0.0${tail}`);
  });
});
