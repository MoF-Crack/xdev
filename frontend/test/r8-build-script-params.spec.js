// 第八轮审计 · 红→绿守卫 5（F-45 修复不完整：删了参数没删文档）
//
// 缺陷（R8-05，P3）：F-45 从 `build.ps1` 删掉了死参数 `-SkipFrontend`，但
// `docs/architecture.md` 与 `docs/installation-guide.md` 仍在教用户传
// `-SkipFrontend` / `-SkipCLI` / `-SkipGUI`——这三个参数在脚本的 `param()`
// 里都已不存在。PowerShell 遇到未知参数会直接以
// "A parameter cannot be found that matches parameter name 'SkipFrontend'"
// 终止（不是忽略），所以照文档执行必然失败。
//
// 修复方向：文档只保留 `-SkipBuild` / `-SkipAudit`，或把三个旧参数作为
// 显式 no-op/别名加回 `param()` 并给出弃用提示。

import { describe, expect, it } from 'vitest';
import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const here = dirname(fileURLToPath(import.meta.url));
const repoRoot = join(here, '..', '..');
const read = (p) => readFileSync(join(repoRoot, p), 'utf8');

describe('R8-05 文档里的 build.ps1 参数必须与脚本一致（红→绿）', () => {
  it('build.ps1 只声明 -SkipBuild / -SkipAudit', () => {
    const ps = read('build.ps1');
    const paramBlock = ps.slice(ps.indexOf('param('), ps.indexOf(')', ps.indexOf('param(')));
    expect(paramBlock).toContain('$SkipBuild');
    expect(paramBlock).toContain('$SkipAudit');
    expect(paramBlock).not.toContain('$SkipFrontend');
    expect(paramBlock).not.toContain('$SkipCLI');
    expect(paramBlock).not.toContain('$SkipGUI');
  });

  it('文档不得再推荐已删除的参数（红）', () => {
    const docs = {
      'docs/architecture.md': read('docs/architecture.md'),
      'docs/installation-guide.md': read('docs/installation-guide.md'),
    };
    const offenders = [];
    for (const [file, text] of Object.entries(docs)) {
      for (const flag of ['-SkipFrontend', '-SkipCLI', '-SkipGUI']) {
        if (text.includes(flag)) offenders.push(`${file}: ${flag}`);
      }
    }
    expect(offenders, `文档仍在推荐 build.ps1 不存在的参数:\n${offenders.join('\n')}`).toEqual([]);
  });
});
