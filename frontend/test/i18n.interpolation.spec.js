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
//
// R8-03 (P1，第八轮审计)：函数式替换只修掉了替换模式语义的一半。逐键 forEach
// 仍有**顺序依赖**（上一键的替换结果参与下一键的匹配）与**只替换第一处**两个
// 副作用。本文件后半段（`R8-03` describe）是这两条的回归守卫。

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

// R8-03（第八轮审计）：F-39 修复不完整 —— 多参数替换有顺序依赖。
//
// 旧实现 `Object.keys(params).forEach(k => text = text.replace('{'+k+'}', () => ...))`
// 逐个键替换，**替换结果参与后续键的匹配**：
//   t('env.var_set_success', { name: 'A{tool}B', tool: 'node', level: 'user' })
//     期望 'Set A{tool}B (tool: node, level: user)'
//     实际 'Set AnodeB (tool: {tool}, level: user)'
// 触发面全部是用户/后端可控数据：环境变量名（env.var_set_success）、后端错误
// 原文（install.activation_warning 的 message）、Windows 路径
// （legacy.found_content 的 `{version}` 目录名）。
describe('R8-03 多参数插值不得二次解释参数值（单次扫描）', () => {
  it('实测用例 1：参数值里的 {tool} 原样上屏，真正的 {tool} 仍被替换', async () => {
    const { t } = (await loadI18n('en')).useI18n();
    // 'env.var_set_success' = 'Set {name} (tool: {tool}, level: {level})'
    expect(t('env.var_set_success', { name: 'A{tool}B', tool: 'node', level: 'user' }))
      .toBe('Set A{tool}B (tool: node, level: user)');
  });

  it('实测用例 2：参数值里的 {level} 不得吃掉后面的 level 替换', async () => {
    const { t } = (await loadI18n('en')).useI18n();
    expect(t('env.var_set_success', { name: 'A{level}B', tool: 'node', level: 'user' }))
      .toBe('Set A{level}B (tool: node, level: user)');
  });

  it('实测用例 3：后端错误原文里的 {version} 原样保留（install.activation_warning）', async () => {
    const { t } = (await loadI18n('en')).useI18n();
    const out = t('install.activation_warning', {
      tool: 'node', version: '22.0.0', message: 'cannot create junction at C:\\x\\{version}\\node.exe',
    });
    expect(out).toContain('C:\\x\\{version}\\node.exe');
    expect(out).toContain('node 22.0.0 was installed');
  });

  it('实测用例 4：工具名里的 {version} 不得被二次解释（confirm_repair_content）', async () => {
    const { t } = (await loadI18n('en')).useI18n();
    expect(t('confirm_repair_content', { tool: 'a{version}b', version: '1.0' }))
      .toContain('a{version}b@1.0');
  });

  it('中文文案同样不得二次解释', async () => {
    const { t } = (await loadI18n('zh')).useI18n();
    expect(t('env.var_set_success', { name: 'A{tool}B', tool: 'node', level: 'user' }))
      .toBe('已设置 A{tool}B（工具: node, 级别: user）');
  });

  it('参数值里的任意 {other} 占位符都不被解释（即使 other 也在 params 中）', async () => {
    const { interpolate } = await loadI18n('en');
    expect(interpolate('Set {name} (tool: {tool})', { name: 'A{tool}B', tool: 'node' }))
      .toBe('Set A{tool}B (tool: node)');
    // 未在 params 中提供的键在任何位置都保留原样。
    expect(interpolate('Set {name} (tool: {tool})', { name: 'A{missing}B' }))
      .toBe('Set A{missing}B (tool: {tool})');
    // 大小写与下划线同属 `\w+`，一样只按字面量插入。
    expect(interpolate('{a_b} {A1} {tool}', { a_b: '{A1}', A1: 'x', tool: 'node' }))
      .toBe('{A1} x node');
  });

  it('同一占位符在文案里出现两次时两处都被替换', async () => {
    const { interpolate } = await loadI18n('en');
    // locale 文案里没有重复占位符的键，直接对模板断言（interpolate 是 t() 的实现）。
    expect(interpolate('{count} versions, {count} tools', { count: 3 }))
      .toBe('3 versions, 3 tools');
    // 未提供时两处都保留。
    expect(interpolate('{count} versions, {count} tools', {})).toBe('{count} versions, {count} tools');
    // 真实键（单占位符）不回归。
    expect((await loadI18n('en')).useI18n().t('detail.versions_count', { count: 2 })).toBe('2 versions');
  });

  it('回归：F-39 的替换模式字面量与 null/undefined 语义不变', async () => {
    const { t } = (await loadI18n('en')).useI18n();
    for (const raw of ['$&', '$`', "$'", '$$', '$1', '$<name>']) {
      expect(t('status.version', { version: raw })).toBe(`version: ${raw}`);
    }
    expect(t('status.version', { version: null })).toBe('version: ');
    expect(t('status.version', { version: undefined })).toBe('version: ');
    expect(t('detail.versions_count', {})).toBe('{count} versions');
    expect(t('status.version', null)).toBe('version: {version}');
  });
});
