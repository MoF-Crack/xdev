import { beforeEach, describe, expect, it, vi } from 'vitest';

// F-39 (P1)：t() 的占位符插值必须把参数当作**字面量**。
//
// 旧实现 `text.replace(`{${k}}`, params[k])` 用字符串作为 replace 的第二参数，
// 于是参数里的 `$&` / `$` + backtick / `$'` / `$$` / `$1` 被 JS 解释为替换模式：
//   - `$&`  → 插入匹配到的占位符字面量（如 `{version}`），参数本身丢失
//   - `$'`  → 把匹配点之后的整段文案复制一遍（文案重复上屏）
//   - `$$`  → 折叠成单个 `$`；`$1` → 无捕获组时按字面量以外的规则处理，造成字符丢失
// 参数来源于用户数据（环境变量名、工具名、Windows 路径），必须原样输出。
//
// 这些用例在修复前为**红**，函数式替换后为**绿**。

const loadI18n = async (locale = 'en') => {
  localStorage.setItem('locale', locale);
  vi.resetModules();
  return await import('../src/i18n/index.ts');
};

beforeEach(() => localStorage.clear());

describe('t() 插值把参数当字面量（F-39）', () => {
  // 'status.version' = 'version: {version}'（占位符在串尾）
  const literalCases = [
    ['$&（匹配到的占位符）', '$&', 'version: $&'],
    ['$`（匹配点之前的文案）', '$`', 'version: $`'],
    ["$'（匹配点之后的文案）", "$'", "version: $'"],
    ['$$（美元符号折叠）', '$$', 'version: $$'],
    ['$1（捕获组引用）', '$1', 'version: $1'],
    ['$<name>（命名捕获组引用）', '$<name>', 'version: $<name>'],
  ];

  for (const [label, param, expected] of literalCases) {
    it(`保留字面量：${label}`, async () => {
      const { useI18n } = await loadI18n('en');
      expect(useI18n().t('status.version', { version: param })).toBe(expected);
    });
  }

  it('占位符在文案中部时不会复制后半段文案（$` 与 $\'）', async () => {
    const { useI18n } = await loadI18n('en');
    const { t } = useI18n();
    // 'confirm_repair_content' = 'The installation of {tool}@{version} is incomplete ...'
    const tail = ' is incomplete (missing binaries). Reinstall the same version to repair it?';
    expect(t('confirm_repair_content', { tool: "$'", version: '1.0.0' })).toBe(
      `The installation of $'@1.0.0${tail}`,
    );
    expect(t('confirm_repair_content', { tool: '$`', version: '1.0.0' })).toBe(
      'The installation of $`@1.0.0' + tail,
    );
    expect(t('confirm_repair_content', { tool: '$&', version: '1.0.0' })).toBe(
      'The installation of $&@1.0.0' + tail,
    );
  });

  it('同时替换多个参数时互不干扰', async () => {
    const { useI18n } = await loadI18n('en');
    // 'env.var_set_success' = 'Set {name} (tool: {tool}, level: {level})'
    expect(
      useI18n().t('env.var_set_success', { name: '$&', tool: "$'", level: '$$' }),
    ).toBe("Set $& (tool: $', level: $$)");
  });

  it('中文文案同样按字面量插值', async () => {
    const { useI18n } = await loadI18n('zh');
    // 'detail.switched_to_xdev' = '已切换 {name} 到 xdev 版本 {version}'
    expect(useI18n().t('detail.switched_to_xdev', { name: '$&', version: "$'" })).toBe(
      "已切换 $& 到 xdev 版本 $'",
    );
  });

  it('非字符串参数按字符串插入，未提供/为 null 的参数不残留 "null"/"undefined"', async () => {
    const { useI18n } = await loadI18n('en');
    const { t } = useI18n();
    expect(t('detail.versions_count', { count: 0 })).toBe('0 versions');
    expect(t('detail.versions_count', { count: 42 })).toBe('42 versions');
    expect(t('detail.versions_count', { count: null })).toBe(' versions');
    expect(t('detail.versions_count', { count: undefined })).toBe(' versions');
    // 未在 params 中出现的占位符保持原样——插值只作用于调用方显式传入的键，
    // 这是 F-39 之外既有行为，不在本次修复范围。
    expect(t('detail.versions_count', {})).toBe('{count} versions');
  });

  it('无参数调用与未知键的既有行为不变', async () => {
    const { useI18n } = await loadI18n('en');
    const { t } = useI18n();
    expect(t('status.version')).toBe('version: {version}');
    expect(t('does.not.exist')).toBe('does.not.exist');
  });
});
