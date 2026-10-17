// 第八轮审计 · R8-04 回归守卫（CI 前端门禁必须同时跑 vitest 与 smoke.mjs）
//
// 缺陷：F-44 在 `.github/workflows/ci.yaml` 的 build job 里加了
// `Frontend unit tests`（`npx vitest run`），但 `frontend/package.json` 的
// `"test": "node test/smoke.mjs"` 是**另一套**守卫：
//   - `vite.config.js` 的 `test.include = ['test/**/*.{test,spec}.js']` 只匹配
//     `*.test.js` / `*.spec.js`，`test/smoke.mjs` 不匹配；
//   - 仓库里**没有任何**地方（工作流/构建脚本/package 脚本）调用 `npm test`。
// 于是 smoke.mjs 的源码级契约断言（AppContent 必须渲染 CommandPalette/AppShell、
// 抽取后的页面顺序、`v-if="false"` 死分支、导航分组、locale 键奇偶）在 CI 里
// 从不执行，本地也只有手动 `npm test` 才跑。
//
// 本 spec 由 vitest 运行，cwd 是 `frontend/`（vitest 的 root 由 vite.config.js 所在
// 目录决定），因此仓库根是 `../`。同时断言两条命令**在同一个 step** 里，
// 避免「加了 npm test 但放在某个从不执行的 job/step」这种假绿。

import { describe, expect, it } from 'vitest';
import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const here = dirname(fileURLToPath(import.meta.url));
const repoRoot = join(here, '..', '..');
const read = (p) => readFileSync(join(repoRoot, p), 'utf8');

/** 取出 `- name: <stepName>` 到下一个 `- name:`/job 边界之间的文本。 */
function stepBlock(ci, stepName) {
  const start = ci.indexOf(`- name: ${stepName}`);
  expect(start, `ci.yaml 必须包含步骤 ${stepName}`).toBeGreaterThan(-1);
  const rest = ci.slice(start + 1);
  const nextStep = rest.indexOf('\n      - name:');
  const nextJob = rest.indexOf('\n  [a-z-]+:\n');
  const ends = [nextStep, nextJob].filter((i) => i > -1);
  const end = ends.length ? Math.min(...ends) : rest.length;
  return rest.slice(0, end);
}

describe('R8-04 CI 前端门禁必须同时执行 vitest 与 npm test（红→绿）', () => {
  it('vitest 的 include 不匹配 smoke.mjs（证明 vitest 不会顺带跑它）', () => {
    const cfg = read('frontend/vite.config.js');
    expect(cfg).toContain("include: ['test/**/*.{test,spec}.js']");
    expect(/smoke/.test(cfg)).toBe(false);
  });

  it('frontend/package.json 的 test 脚本是独立的 smoke.mjs 守卫', () => {
    const pkg = JSON.parse(read('frontend/package.json'));
    expect(pkg.scripts.test).toBe('node test/smoke.mjs');
    expect(pkg.scripts.test).not.toContain('vitest');
  });

  it('ci.yaml 同时包含 npx vitest run 与 npm test', () => {
    const ci = read('.github/workflows/ci.yaml');
    expect(ci).toContain('npx vitest run');
    // 红：修复前 ci.yaml 中没有任何 `npm test` / `npm run test`。
    expect(ci).toMatch(/npm (run )?test\b/);
  });

  it('两条命令在同一个 Frontend unit tests 步骤内、且 working-directory 是 frontend', () => {
    const ci = read('.github/workflows/ci.yaml');
    const block = stepBlock(ci, 'Frontend unit tests');
    expect(block).toContain('npx vitest run');
    expect(block).toMatch(/npm (run )?test\b/);
    expect(block).toContain('working-directory: frontend');
    // 必须是两条独立命令行（而不是被注释掉或写进 echo）。
    const commands = block.split('\n').map((l) => l.trim()).filter((l) => l && !l.startsWith('#'));
    expect(commands).toContain('npx vitest run');
    expect(commands).toContain('npm test');
  });

  it('该步骤位于 build job（PR 触发）内，不是只在 release/tag 才跑的 job', () => {
    const ci = read('.github/workflows/ci.yaml');
    const buildJob = ci.slice(ci.indexOf('\n  build:'), ci.indexOf('\n  integration-tests:'));
    expect(buildJob).toContain('Frontend unit tests');
    expect(buildJob).toMatch(/npm (run )?test\b/);
  });
});
