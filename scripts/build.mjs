#!/usr/bin/env node
// One build contract for local scripts and CI. Tool versions are pinned in
// go.mod/package-lock.json; callers may supply GO/NPM/WAILS3 executable paths.
import { spawnSync } from 'node:child_process';
import { readFileSync, mkdirSync, copyFileSync, existsSync } from 'node:fs';
import { resolve, dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { prepareWindowsSyso, runPinnedResourceGenerator } from './release/native-resources.mjs';

const root = resolve(dirname(fileURLToPath(import.meta.url)), '..');
function main() {
const args = new Set(process.argv.slice(2));
const go = process.env.GO || 'go';
const npm = process.env.NPM || (process.platform === 'win32' ? 'npm.cmd' : 'npm');
const wails = process.env.WAILS3 || (process.platform === 'win32' ? 'wails3.exe' : 'wails3');
const goMod = readFileSync(join(root, 'go.mod'), 'utf8');
const wailsVersion = goMod.match(/github\.com\/wailsapp\/wails\/v3\s+(\S+)/)?.[1];
if (!wailsVersion) throw new Error('Pinned Wails v3 dependency is missing');
const version = readFileSync(join(root, 'internal/version/version.go'), 'utf8').match(/const Version = "([^"]+)"/)?.[1];
if (!version) throw new Error('Version constant is missing');
const env = { ...process.env, GOFLAGS: process.env.GOFLAGS || '-mod=readonly' };
const run = (exe, commandArgs, cwd = root) => {
  const result = spawnSync(exe, commandArgs, { cwd, env, stdio: 'inherit', shell: process.platform === 'win32' && /\.cmd$/i.test(exe) });
  if (result.error) throw result.error;
  if (result.status !== 0) {
    // Throw instead of process.exit so owned native resources are always cleaned.
    process.exitCode = result.status || 1;
    throw new Error(`${exe} ${commandArgs[0]} failed (${result.status ?? result.signal})`);
  }
};
if (!args.has('--skip-audit')) {
  if (process.platform === 'win32') run('powershell.exe', ['-NoProfile', '-File', join(root, 'scripts/audit-links.ps1')]);
  else run('bash', [join(root, 'scripts/audit-links.sh')]);
}
if (args.has('--skip-build')) process.exit(0);
const hostOS = { win32: 'windows', darwin: 'darwin', linux: 'linux' }[process.platform];
const hostArch = { x64: 'amd64', arm64: 'arm64' }[process.arch] || process.arch;
const targetOS = process.env.GOOS || hostOS;
const targetArch = process.env.GOARCH || hostArch;
const tags = process.env.XDEV_BUILD_TAGS || (targetOS === 'linux' ? 'desktop,production,gtk3' : 'desktop,production');
if (!args.has('--skip-bindings')) {
  // Resolve bindings using the same platform imports as the desktop build.
  run(wails, ['generate', 'bindings', '-f', `-tags ${tags},bindings`, '-d', 'frontend/bindings', '-ts', './internal/gui']);
}
if (!args.has('--skip-frontend')) {
  run(npm, ['ci', '--ignore-scripts'], join(root, 'frontend'));
  run(npm, ['run', 'build'], join(root, 'frontend'));
}
const out = process.env.XDEV_OUTPUT || join(root, 'build/bin', targetOS === 'windows' ? 'xdev.exe' : 'xdev');
mkdirSync(dirname(out), { recursive: true });
const flags = ['-s', '-w', '-X', `github.com/xmdemt/xdev/internal/version.BuildDate=${new Date().toISOString()}`];
if (process.env.GITHUB_SHA) flags.push('-X', `github.com/xmdemt/xdev/internal/version.GitCommit=${process.env.GITHUB_SHA}`);
const key = process.env.XDEV_SIGNING_PUBLIC_KEY || (existsSync(join(root, 'keys/pub.key')) ? readFileSync(join(root, 'keys/pub.key'), 'utf8').trim() : '');
if (key) flags.push('-X', `github.com/xmdemt/xdev/internal/signer.rootPublicKeyB64=${key}`);
else if (args.has('--release')) throw new Error('Release build requires an Ed25519 public key');
if (args.has('--release')) {
  env.XDEV_REQUIRE_SIGNATURE_COVERAGE = '1';
  env.XDEV_MANIFEST_VERIFY_PUBKEY = key;
  // Verification runs on the builder host, even when packaging another arch.
  const nativeKeys = ['GOOS', 'GOARCH', 'CGO_ENABLED', 'CGO_CFLAGS', 'CGO_LDFLAGS'];
  const saved = Object.fromEntries(nativeKeys.map(key => [key, env[key]]));
  for (const key of nativeKeys) delete env[key];
  env.CGO_ENABLED = '0';
  run(go, ['test', '.', '-run', '^TestManifestSignatureCoverage$', '-count=1'], root);
  for (const key of nativeKeys) {
    if (saved[key] === undefined) delete env[key]; else env[key] = saved[key];
  }
}
// --skip-bindings/--release never bypass native resources. WAILS3 must still
// resolve to the pinned v3 CLI for every Windows target, including cross-builds.
const native = targetOS === 'windows'
  ? prepareWindowsSyso({ root, version, arch: targetArch, wails, run: (exe, commandArgs) => runPinnedResourceGenerator({ exe, commandArgs, go, wailsVersion, run }) })
  : null;
try {
  run(go, ['build', '-trimpath', '-tags', tags, '-ldflags', flags.join(' '), '-o', out, '.']);
} finally {
  native?.cleanup();
}
if (targetOS === hostOS && targetArch === hostArch) run(out, ['version']);
if (!process.env.XDEV_OUTPUT) copyFileSync(out, join(dirname(out), `xdev-${version}${targetOS === 'windows' ? '.exe' : ''}`));
console.log(`Built ${out} with Wails ${wailsVersion} (${tags})`);
}
try {
  main();
} catch (error) {
  console.error(error.message);
  process.exitCode ||= 1;
}
