// 20261021 审计（Item 5 冒烟暴露）：Wails GUI 需要 `desktop,production`
// 构建标签。普通 `go build` 会编译出“Wails applications will not build
// without the correct build tags”的对话框，GUI 完全不可用。
//
// 本守卫扫 CI 里所有真正的 `go build` 命令行（排除注释与步骤名），要求
// 每条都带 `-tags desktop,production`。Release 产物尤其重要：用户下载的
// xdev-*.tar.gz / .deb / .AppImage 都来自这些命令。
import { describe, expect, it } from 'vitest';
import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const here = dirname(fileURLToPath(import.meta.url));
const repoRoot = join(here, '..', '..');
const ci = readFileSync(join(repoRoot, '.github', 'workflows', 'ci.yaml'), 'utf8');

function buildCommands() {
  return ci
    .split('\n')
    // 去掉注释与步骤名：它们不是可执行命令。
    .filter((line) => !/^\s*#/.test(line) && !/^\s*-\s*name:/.test(line))
    .filter((line) => /(^|\s)go build\s/.test(line));
}

describe('Wails 构建标签守卫', () => {
  it('CI 中所有 go build 命令都必须带 desktop,production', () => {
    const cmds = buildCommands();
    expect(cmds.length, 'CI 应至少有一条 go build 命令').toBeGreaterThan(5);
    const offenders = cmds.filter((l) => !l.includes('-tags desktop,production'));
    expect(
      offenders,
      `以下 go build 缺少 Wails 标签，产物 GUI 会直接弹错误框:\n${offenders.join('\n')}`,
    ).toEqual([]);
  });

  it('本地构建脚本使用 wails build（自带标签）', () => {
    const ps = readFileSync(join(repoRoot, 'build.ps1'), 'utf8');
    const sh = readFileSync(join(repoRoot, 'build.sh'), 'utf8');
    expect(ps).toContain('wails build');
    expect(sh).toContain('wails build');
  });
});
