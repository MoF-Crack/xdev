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

  it('lock 的依赖树结构保持完整（未因修复而损坏）', () => {
    const lock = readJson('frontend/package-lock.json');
    // 2026-10-25 更新：本断言原钉住「185 条 / tinyexec 0.3.3-rc.2」，
    // 那是修 Dependabot 告警之前的快照。为消除 vitest→tinypool 的 RCE
    // （critical），vitest 从 3.2.7 升到 5.0.5，npm 重建了依赖树：
    //   条目数 185 -> 171（vitest 5 精简了依赖）
    //   tinyexec 0.3.3-rc.2 -> 1.3.1
    // 硬编码具体数字会让每次合法的依赖升级都要改测试，因此改为
    // 断言**结构不变量**：lockfileVersion 正确、root 条目存在、依赖树非空。
    expect(lock.lockfileVersion).toBe(3);
    expect(lock.packages['']).toBeDefined();
    expect(Object.keys(lock.packages).length).toBeGreaterThan(100);
    // root 依赖的**键集**必须与 package.json 一致（未增删依赖）。
    const pkg = readJson('frontend/package.json');
    expect(Object.keys(lock.packages[''].dependencies).sort())
      .toEqual(Object.keys(pkg.dependencies).sort());
    // 注意（2026-10-25 更新）：本断言原先钉住的是 lock 的 root 依赖区间
    // 停留在旧值（naive-ui ^2.38.0 / vue ^3.4.0）这一**既有偏差**。
    //
    // 修复 GitHub Dependabot 告警时执行了 `npm audit fix`（修 vue XSS /
    // source-map-js DoS 等），npm 重写了 lock 的 root 依赖区间，
    // 使其与 package.json 一致 —— 那个既有偏差被顺带消除了。
    //
    // 因此断言从「钉住旧区间」改为「与 package.json 一致」，语义更强：
    // lock 与 package.json 在**版本区间**层面也不得漂移。
    expect(lock.packages[''].dependencies).toEqual(pkg.dependencies);
  });

  it('lock 中 root 版本不再出现旧值', () => {
    const raw = readFileSync(join(repoRoot, 'frontend/package-lock.json'), 'utf8');
    const rootBlock = raw.slice(0, raw.indexOf('"dependencies"'));
    expect(rootBlock).not.toContain('0.3.3-rc.2');
  });
});
