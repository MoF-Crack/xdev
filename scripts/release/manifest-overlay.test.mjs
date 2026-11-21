import { test } from 'node:test';
import assert from 'node:assert/strict';
import { execFileSync } from 'node:child_process';
import { mkdtempSync, mkdirSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';
import { tmpdir } from 'node:os';
import { createManifestOverlay, removeManifestOverlay } from './manifest-overlay.mjs';

function fixture(t) {
  const root = mkdtempSync(join(tmpdir(), 'xdev-manifest-overlay-test-'));
  t.after(() => rmSync(root, { recursive: true, force: true }));
  const tracked = join(root, 'repo', 'manifests');
  const signed = join(root, 'signed', 'manifests');
  mkdirSync(tracked, { recursive: true });
  mkdirSync(signed, { recursive: true });
  mkdirSync(join(root, 'signed', 'keys'), { recursive: true });
  writeFileSync(join(tracked, 'demo.yaml'), 'unsigned manifest\n');
  writeFileSync(join(tracked, 'demo.yaml.sig'), 'unsigned signature\n');
  writeFileSync(join(tracked, 'index.yaml'), 'index\n');
  writeFileSync(join(signed, 'demo.yaml'), 'signed manifest\n');
  writeFileSync(join(signed, 'demo.yaml.sig'), 'signed signature\n');
  writeFileSync(join(signed, 'index.yaml'), 'signed index\n');
  writeFileSync(join(root, 'signed', 'keys', 'pub.key'), 'public key\n');
  return { root, tracked, signedRoot: join(root, 'signed') };
}

test('manifest overlay maps every embedded manifest to staged signed bytes without editing checkout', t => {
  const { root, tracked, signedRoot } = fixture(t);
  const overlay = createManifestOverlay(join(root, 'repo'), signedRoot);
  t.after(() => removeManifestOverlay(overlay));
  const parsed = JSON.parse(readFileSync(overlay.path, 'utf8'));
  assert.equal(overlay.fileCount, 3);
  assert.deepEqual(Object.keys(parsed.Replace).sort(), [
    join(tracked, 'demo.yaml'),
    join(tracked, 'demo.yaml.sig'),
    join(tracked, 'index.yaml'),
  ].sort());
  assert.equal(readFileSync(join(tracked, 'demo.yaml'), 'utf8'), 'unsigned manifest\n');
  assert.equal(readFileSync(parsed.Replace[join(tracked, 'demo.yaml')], 'utf8'), 'signed manifest\n');
});

test('manifest overlay refuses an incomplete signed input set', t => {
  const { root, tracked, signedRoot } = fixture(t);
  rmSync(join(signedRoot, 'manifests', 'demo.yaml.sig'));
  assert.throws(() => createManifestOverlay(join(root, 'repo'), signedRoot), /input set differs.*demo\.yaml\.sig/i);
  assert.equal(readFileSync(join(tracked, 'demo.yaml'), 'utf8'), 'unsigned manifest\n');
});

test('manifest overlay admits new signatures only for existing manifests', t => {
  const { root, tracked, signedRoot } = fixture(t);
  rmSync(join(tracked, 'demo.yaml.sig'));
  const overlay = createManifestOverlay(join(root, 'repo'), signedRoot);
  t.after(() => removeManifestOverlay(overlay));
  const parsed = JSON.parse(readFileSync(overlay.path, 'utf8'));
  assert.equal(parsed.Replace[join(tracked, 'demo.yaml.sig')], join(signedRoot, 'manifests', 'demo.yaml.sig'));
  writeFileSync(join(signedRoot, 'manifests', 'orphan.yaml.sig'), 'unexpected');
  assert.throws(() => createManifestOverlay(join(root, 'repo'), signedRoot), /unexpected: orphan\.yaml\.sig/);
});

test('real Go overlay embeds signed bytes while keeping Git VCS metadata clean', t => {
  const base = mkdtempSync(join(tmpdir(), 'xdev-go-overlay-integration-'));
  t.after(() => rmSync(base, { recursive: true, force: true }));
  const repo = join(base, 'repo');
  const trackedManifests = join(repo, 'manifests');
  const signedRoot = join(repo, 'build', 'signed-release-input');
  const signedManifests = join(signedRoot, 'manifests');
  mkdirSync(trackedManifests, { recursive: true });
  mkdirSync(signedManifests, { recursive: true });
  mkdirSync(join(signedRoot, 'keys'), { recursive: true });
  writeFileSync(join(repo, 'go.mod'), 'module example.com/xdev-overlay-probe\n\ngo 1.27.0\n');
  writeFileSync(join(repo, '.gitignore'), 'build/signed-release-input/\n');
  writeFileSync(join(repo, 'build', 'tracked-placeholder.txt'), 'tracked build directory\n');
  writeFileSync(join(trackedManifests, 'demo.yaml'), 'checkout-version\n');
  writeFileSync(join(trackedManifests, 'index.yaml'), 'checkout-index\n');
  writeFileSync(join(signedManifests, 'demo.yaml'), 'signed-release-version\n');
  writeFileSync(join(signedManifests, 'index.yaml'), 'signed-release-index\n');
  writeFileSync(join(signedRoot, 'keys', 'pub.key'), 'fixture-public-key\n');
  writeFileSync(join(repo, 'main.go'), [
    'package main',
    'import (_ "embed"; "fmt")',
    '//go:embed manifests/demo.yaml',
    'var manifest string',
    'func main() { fmt.Print(manifest) }',
  ].join('\n'));
  const git = (...args) => execFileSync('git', args, { cwd: repo, stdio: 'pipe' });
  git('init');
  git('config', 'user.name', 'XDev build test');
  git('config', 'user.email', 'xdev-build-test@example.invalid');
  git('add', '.gitignore', 'go.mod', 'main.go', 'manifests', 'build/tracked-placeholder.txt');
  git('commit', '-m', 'clean release source');
  git('tag', 'v1.2.3');
  assert.equal(git('status', '--porcelain').toString().trim(), '', 'ignored release inputs must not dirty checkout');

  const overlay = createManifestOverlay(repo, signedRoot);
  t.after(() => removeManifestOverlay(overlay));
  const executable = join(base, process.platform === 'win32' ? 'probe.exe' : 'probe');
  execFileSync('go', ['build', '-buildvcs=true', '-overlay', overlay.path, '-o', executable, '.'], { cwd: repo, stdio: 'pipe' });
  assert.equal(execFileSync(executable, [], { cwd: repo, encoding: 'utf8' }), 'signed-release-version\n');
  const metadata = execFileSync('go', ['version', '-m', executable], { cwd: repo, encoding: 'utf8' });
  assert.ok(metadata.includes('mod\texample.com/xdev-overlay-probe\tv1.2.3'));
  assert.ok(metadata.includes('vcs.modified=false'));
  assert.ok(!metadata.includes('vcs.modified=true') && !metadata.includes('+dirty'));
});
