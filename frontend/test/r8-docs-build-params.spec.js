// 第八轮审计 · R8-05 回归守卫（文档不得再教用户传已删除的 build.ps1 参数）
//
// 缺陷：F-45 从 `build.ps1` 删掉了死参数，但
// `docs/architecture.md`（-SkipGUI / -SkipFrontend）与
// `docs/installation-guide.md`（-SkipFrontend / -SkipCLI / -SkipGUI）仍在推荐它们。
// PowerShell 遇到未知参数会**直接终止**（不是忽略）：
//   "A parameter cannot be found that matches parameter name 'SkipFrontend'"
// 因此照文档执行必然失败。
//
// 本 spec 双向守卫：docs/** 不再出现三个已删除参数，且**文档列出的参数集合**与
// `build.ps1` 的 `param()` 声明集合一致（既防文档多写，也防文档漏写新参数）。

import { describe, expect, it } from 'vitest';
import { readdirSync, readFileSync, statSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const here = dirname(fileURLToPath(import.meta.url));
const repoRoot = join(here, '..', '..');
const read = (p) => readFileSync(join(repoRoot, p), 'utf8');

const REMOVED_FLAGS = ['-SkipFrontend', '-SkipCLI', '-SkipGUI'];

function markdownFilesIn(dir) {
  return readdirSync(dir, { withFileTypes: true }).flatMap((entry) => {
    // docs/audits 与 docs/history 是**时间点快照**：它们可以引用当时的
    // 已删除参数作为缺陷证据（例如 Round 7 报告）。活文档（architecture /
    // installation / usage）仍被下面的双向断言覆盖。
    if (entry.isDirectory() && (entry.name === 'audits' || entry.name === 'history')) return [];
    const full = join(dir, entry.name);
    if (entry.isDirectory()) return markdownFilesIn(full);
    return entry.name.endsWith('.md') ? [full] : [];
  });
}

/** `build.ps1` 的 param() 里声明的 `-Xxx` 开关名。 */
function declaredParams() {
  const ps = read('build.ps1');
  const start = ps.indexOf('param(');
  expect(start, 'build.ps1 必须有 param() 块').toBeGreaterThan(-1);
  const end = ps.indexOf('\n)', start);
  const block = ps.slice(start, end);
  return [...block.matchAll(/\[switch\]\s*\$(\w+)/g)].map((m) => `-${m[1]}`).sort();
}

describe('R8-05 文档里的 build.ps1 参数必须与脚本一致（红→绿）', () => {
  it('build.ps1 只声明 -SkipBuild / -SkipAudit', () => {
    expect(declaredParams()).toEqual(['-SkipAudit', '-SkipBuild']);
  });

  it('docs/** 不得再出现任何已删除的参数', () => {
    const offenders = [];
    for (const file of markdownFilesIn(join(repoRoot, 'docs'))) {
      const text = readFileSync(file, 'utf8');
      for (const flag of REMOVED_FLAGS) {
        if (text.includes(flag)) offenders.push(`${file.replace(repoRoot, '').replace(/\\/g, '/')}: ${flag}`);
      }
    }
    // 红：修复前 architecture.md 命中 -SkipGUI/-SkipFrontend，
    // installation-guide.md 命中三个全部。
    expect(offenders, `文档仍在推荐 build.ps1 不存在的参数:\n${offenders.join('\n')}`).toEqual([]);
  });

  it('两处文档列出的参数集合与 param() 完全一致', () => {
    const declared = new Set(declaredParams());
    for (const rel of ['docs/architecture.md', 'docs/installation-guide.md']) {
      const text = read(rel);
      const mentioned = new Set(
        [...text.matchAll(/`?(-Skip\w+)`?/g)].map((m) => m[1]),
      );
      const unknown = [...mentioned].filter((f) => !declared.has(f));
      const missing = [...declared].filter((f) => !mentioned.has(f));
      expect(unknown, `${rel} 列出了 param() 中不存在的参数`).toEqual([]);
      expect(missing, `${rel} 漏掉了 param() 中存在的参数`).toEqual([]);
    }
  });

  it('architecture.md 的构建命令块不再宣称「仅 CLI / 跳过前端」', () => {
    const text = read('docs/architecture.md');
    expect(text).not.toMatch(/仅 CLI/);
    expect(text).not.toMatch(/跳过前端编译/);
  });
});
