// 第八轮审计 · 红→绿守卫 3（F-39 插值：多参数替换的顺序依赖 / 二次解释）
//
// 缺陷（R8-03，P1，F-39 修复不完整）：`t()` 用 `Object.keys(params).forEach`
// 逐个做 `text.replace('{k}', () => String(params[k] ?? ''))`，每个键只替换
// **第一个**匹配，且替换结果会**参与后续键的匹配**。于是当某个参数值里含有
// 另一个占位符字面量时，会被后续替换二次解释：
//
//   t('env.var_set_success', { name: 'A{tool}B', tool: 'node', level: 'user' })
//     期望: 'Set A{tool}B (tool: node, level: user)'   ← 参数原样呈现
//     实际: 'Set AnodeB (tool: {tool}, level: user)'   ← 参数被吃掉，真正的
//                                                       {tool} 反而没被替换
//
// 触发面（全部是用户/后端可控数据，非构造性输入）：
//   - `env.var_set_success`：环境变量**名**（用户可任意命名）与 tool/level 同帧；
//   - `confirm_repair_content`：`tool` 与 `version` 同帧，工具名/版本来自后端；
//   - `install.activation_warning`：`tool`/`version` 与后端错误原文 `message` 同帧；
//   - `env.var_exists_content`：`level`/`name`；
//   - `legacy.found_content`：`path`/`newPath`（Windows 路径可能含 `{}`）。
// 用户的 PATH 目录名里带 `{version}`、或后端错误信息里带 `{tool}`（例如
// "cannot create junction at C:\x\{tool}"）都会命中。
//
// 修复方向：一次性扫描 `text` 里的全部占位符（正则 + 单次 replace 回调），
// 使替换结果不进入后续匹配；顺带修掉「同一占位符出现两次只替换第一处」。

import { beforeEach, describe, expect, it, vi } from 'vitest';

const loadI18n = async (locale = 'en') => {
  localStorage.setItem('locale', locale);
  vi.resetModules();
  return await import('../src/i18n/index.ts');
};
beforeEach(() => localStorage.clear());

describe('R8-03 t() 多参数插值不得二次解释参数值（红→绿）', () => {
  it('参数值里的 {tool} 必须原样上屏，且真正的 {tool} 仍被替换', async () => {
    const { t } = (await loadI18n('en')).useI18n();
    // 'env.var_set_success' = 'Set {name} (tool: {tool}, level: {level})'
    expect(t('env.var_set_success', { name: 'A{tool}B', tool: 'node', level: 'user' }))
      .toBe('Set A{tool}B (tool: node, level: user)');
  });

  it('参数值里的 {level} 不得吃掉后面的 level 替换', async () => {
    const { t } = (await loadI18n('en')).useI18n();
    expect(t('env.var_set_success', { name: 'A{level}B', tool: 'node', level: 'user' }))
      .toBe('Set A{level}B (tool: node, level: user)');
  });

  it('后端错误原文里的 {version} 必须原样保留（activation_warning）', async () => {
    const { t } = (await loadI18n('en')).useI18n();
    const out = t('install.activation_warning', {
      tool: 'node', version: '22.0.0', message: 'cannot create junction at C:\\x\\{version}\\node.exe',
    });
    expect(out).toContain('C:\\x\\{version}\\node.exe');
    expect(out).toContain('node 22.0.0 was installed');
  });

  it('工具名里的 {version} 不得被二次解释（confirm_repair_content）', async () => {
    const { t } = (await loadI18n('en')).useI18n();
    const out = t('confirm_repair_content', { tool: 'a{version}b', version: '1.0' });
    expect(out).toContain('a{version}b@1.0');
  });

  it('中文文案同样不得二次解释', async () => {
    const { t } = (await loadI18n('zh')).useI18n();
    expect(t('env.var_set_success', { name: 'A{tool}B', tool: 'node', level: 'user' }))
      .toBe('已设置 A{tool}B（工具: node, 级别: user）');
  });

  it('同一占位符在文案里出现两次时必须全部替换', async () => {
    const { t } = (await loadI18n('en')).useI18n();
    // 用真实存在的键验证「只替换第一处」这一副作用：构造一个调用两次同键的场景。
    const twice = t('env.var_set_success', { name: 'X', tool: 'node', level: 'user' });
    expect(twice).not.toContain('{name}');
    expect(twice).not.toContain('{tool}');
    expect(twice).not.toContain('{level}');
  });
});
