// 第八轮审计 · R8-06 回归守卫（lockfile 的 root version 必须与 package.json 同步）
//
// 缺陷：`frontend/package.json` 已升到 0.4.0，但 `frontend/package-lock.json` 的
// 两处 **root** version（顶层 `"version"` 与 `packages[""].version`）仍是
// `0.3.3-rc.2`。实测 `npm ci --dry-run` / 真实 `npm ci --ignore-scripts` 都 exit 0
// （npm 不比较 version 字段），所以它**不会**让 CI 失败——但「lock 与 package.json
// 同步」这一前提不成立，后续任何依赖 version 的比对都会踩到。
//
// 本 spec 只断言 root 两处，且**不**约束 `node_modules/tinyexec` 的 0.3.3-rc.2
// （那是真实依赖版本，与 root 版本号同形属巧合）。

import { describe, expect, it } from 'vitest';
import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const here = dirname(fileURLToPath(import.meta.url));
const repoRoot = join(here, '..', '..');
const readJson = (p) => JSON.parse(readFileSync(join(repoRoot, p), 'utf8'));

describe('R8-06 lockfile root version 必须与 package.json 一致（红→绿）', () => {
  it('顶层 version 与 packages[""].version 都等于 package.json 的 version', () => {
    const pkg = readJson('frontend/package.json');
    const lock = readJson('frontend/package-lock.json');
    expect(pkg.version).toBe('0.4.0');
    // 红：修复前两处均为 '0.3.3-rc.2'。
    expect(lock.version).toBe(pkg.version);
    expect(lock.packages[''].version).toBe(pkg.version);
    expect(lock.packages[''].name).toBe(pkg.name);
  });

  it('lock 的依赖树未被改动（只改了 version 字段）', () => {
    const lock = readJson('frontend/package-lock.json');
    // 依赖树完整性：lockfileVersion 与依赖条目数必须保持原状（实测 185 条：
    // root + 184 个依赖条目）。修复只允许动两处 version 字段。
    expect(lock.lockfileVersion).toBe(3);
    expect(Object.keys(lock.packages).length).toBe(185);
    // tinyexec 的真实依赖版本不受 root 版本同步影响（同形字符串，必须区分开）。
    expect(lock.packages['node_modules/tinyexec'].version).toBe('0.3.3-rc.2');
    // root 依赖的**键集**必须与 package.json 一致（未增删依赖）。
    const pkg = readJson('frontend/package.json');
    expect(Object.keys(lock.packages[''].dependencies).sort())
      .toEqual(Object.keys(pkg.dependencies).sort());
    // 注意：lock 的 root 依赖**区间**仍停留在旧值（naive-ui ^2.38.0 / vue ^3.4.0），
    // 与 package.json 的 ^2.44.0 / ^3.5.0 不一致。这是 R8-06 之前就存在的既有偏差，
    // 修它需要 `npm install`（会重写整棵依赖树），超出本次「只改 version 字段」的范围，
    // 故此处把现状钉住，避免被误认为本次改动引入。
    expect(lock.packages[''].dependencies).toEqual({
      '@vicons/ionicons5': '^0.12.0',
      'naive-ui': '^2.38.0',
      vue: '^3.4.0',
    });
  });

  it('lock 中 root 版本不再出现旧值', () => {
    const raw = readFileSync(join(repoRoot, 'frontend/package-lock.json'), 'utf8');
    const rootBlock = raw.slice(0, raw.indexOf('"dependencies"'));
    expect(rootBlock).not.toContain('0.3.3-rc.2');
  });
});
