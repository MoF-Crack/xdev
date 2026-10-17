// 第八轮审计 · 红→绿守卫 4（F-44 修复不完整：CI 前端门禁漏掉 `npm test`）
//
// 缺陷（R8-04，P2）：F-44 在 `.github/workflows/ci.yaml` 的 build job 里加了
// `Frontend unit tests`（`npx vitest run`），但 `frontend/package.json` 的
// `"test": "node test/smoke.mjs"` 是**另一套**守卫：
//   - `vite.config.js` 的 `test.include = ['test/**/*.{test,spec}.js']` 只匹配
//     `*.test.js` / `*.spec.js`，`test/smoke.mjs` 不匹配；
//   - 仓库里**没有任何**地方（工作流/构建脚本/package 脚本）调用 `npm test`。
// 因此 smoke.mjs 的源码级契约断言（AppContent 必须渲染 CommandPalette/AppShell、
// 抽取后的页面顺序、`v-if="false"` 死分支、导航分组等）在 CI 里从不执行，
// 本地也只有手动 `npm test` 才会跑。这与 F-44 想修的「前端守卫退化为本机自律」
// 是同一类问题，只是漏了第二个入口。
//
// 修复方向：在 `Frontend unit tests` 步骤里追加 `npm test`（或 `npm run test`），
// 或把 smoke.mjs 的断言并入 vitest 用例集。

import { describe, expect, it } from 'vitest';
import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const here = dirname(fileURLToPath(import.meta.url));
const repoRoot = join(here, '..', '..');

const read = (p) => readFileSync(join(repoRoot, p), 'utf8');

describe('R8-04 CI 必须同时跑 vitest 与 smoke.mjs（红→绿）', () => {
  it('frontend/package.json 的 test 脚本不是 vitest（证明是第二套守卫）', () => {
    const pkg = JSON.parse(read('frontend/package.json'));
    expect(pkg.scripts.test).toBe('node test/smoke.mjs');
    expect(pkg.scripts.test).not.toContain('vitest');
  });

  it('vitest 的 include 不匹配 smoke.mjs（证明 vitest 不会顺带跑它）', () => {
    const cfg = read('frontend/vite.config.js');
    expect(cfg).toContain("include: ['test/**/*.{test,spec}.js']");
    expect(/smoke/.test(cfg)).toBe(false);
  });

  it('CI 里必须出现 npm test（红：修复前只有 npx vitest run）', () => {
    const ci = read('.github/workflows/ci.yaml');
    expect(ci).toContain('npx vitest run');
    // 红：修复前 ci.yaml 中没有任何 `npm test` / `npm run test`。
    expect(ci).toMatch(/npm (run )?test\b/);
  });
});
