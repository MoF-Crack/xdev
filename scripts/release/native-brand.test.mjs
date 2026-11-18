import { test } from 'node:test';
import assert from 'node:assert/strict';
import { copyFileSync, existsSync, mkdirSync, mkdtempSync, readFileSync, readdirSync, rmSync, unlinkSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';
import { spawnSync } from 'node:child_process';
import { macInfoPlist, nativeVersion, prepareWindowsSyso, renderWindowsResources, runPinnedResourceGenerator } from './native-resources.mjs';
import { archiveName, pack, platforms } from './package.mjs';

const root = resolve(dirname(fileURLToPath(import.meta.url)), '../..');
const temp = t => {
  const dir = mkdtempSync(join(tmpdir(), 'xdev-native-test-'));
  t.after(() => rmSync(dir, { recursive: true, force: true }));
  return dir;
};
const fixture = t => {
  const dir = temp(t);
  mkdirSync(join(dir, 'scripts/release'), { recursive: true });
  mkdirSync(join(dir, 'internal/version'), { recursive: true });
  mkdirSync(join(dir, 'build/windows'), { recursive: true });
  for (const path of ['scripts/build.mjs', 'scripts/release/native-resources.mjs', 'scripts/release/platform-packages.sh', 'build/windows/info.json', 'build/windows/wails.exe.manifest', 'build/windows/icon.ico', 'build/appicon.png']) copyFileSync(join(root, path), join(dir, path));
  writeFileSync(join(dir, 'go.mod'), 'module nativefixture\n\ngo 1.26\n\nrequire github.com/wailsapp/wails/v3 v3.1.0\n');
  writeFileSync(join(dir, 'internal/version/version.go'), 'package version\nconst Version = "1.2.3-beta.2"\n');
  return dir;
};
const objectFor = arch => {
  const result = Buffer.alloc(20);
  result.writeUInt16LE({ amd64: 0x8664, arm64: 0xaa64, '386': 0x14c }[arch]);
  return result;
};

test('a missing CLI falls back to the exact go.mod Wails version without changing workflow permissions', () => {
  const calls = [];
  runPinnedResourceGenerator({ exe: 'missing-wails3', commandArgs: ['generate', 'syso', '-arch', 'amd64'], go: 'pinned-go', wailsVersion: 'v3.1.0', run: (exe, args) => { calls.push([exe, args]); if (exe === 'missing-wails3') throw Object.assign(new Error('missing'), { code: 'ENOENT' }); } });
  assert.deepEqual(calls[1], ['pinned-go', ['run', 'github.com/wailsapp/wails/v3/cmd/wails3@v3.1.0', 'generate', 'syso', '-arch', 'amd64']]);
});
test('generator failures other than missing executable never trigger a fallback', () => {
  let count = 0;
  assert.throws(() => runPinnedResourceGenerator({ exe: 'wails3', commandArgs: [], go: 'go', wailsVersion: 'v3.1.0', run: () => { count++; throw new Error('bad icon'); } }), /bad icon/);
  assert.equal(count, 1);
});

test('concrete Windows version and manifest replace Wails placeholders', () => {
  const { info, manifest } = renderWindowsResources(root, '1.2.3-beta.2');
  const data = JSON.parse(info);
  assert.equal(data.fixed.file_version, '1.2.3.2');
  assert.equal(data.fixed.product_version, '1.2.3.2');
  assert.equal(data.fixed.flags, 'Prerelease');
  assert.equal(data.info['0000'].ProductName, 'XDev');
  assert.equal(data.info['0000'].ProductVersion, '1.2.3-beta.2');
  assert.match(manifest, /name="io\.github\.xmdemt\.xdev" version="1\.2\.3\.2"/);
  assert.doesNotMatch(info + manifest, /\{\{|com\.wails/);
  assert.equal(nativeVersion('v1.2.3').windows, '1.2.3.0');
  assert.throws(() => nativeVersion('1.2.65536'), /uint16/);
});

test('Wails receives concrete resources and explicit target architecture; cleanup owns only its object', t => {
  const dir = fixture(t);
  const existing = join(dir, 'rsrc_windows_arm64.syso');
  writeFileSync(existing, 'user-owned');
  let output;
  const native = prepareWindowsSyso({ root: dir, version: '1.2.3-beta.2', arch: 'arm64', wails: 'pinned-wails', run: (exe, args) => {
    assert.equal(exe, 'pinned-wails');
    assert.deepEqual(args.slice(0, 2), ['generate', 'syso']);
    const flag = name => args[args.indexOf(name) + 1];
    assert.equal(flag('-arch'), 'arm64');
    assert.equal(flag('-icon'), join(dir, 'build/windows/icon.ico'));
    assert.equal(JSON.parse(readFileSync(flag('-info'), 'utf8')).info['0000'].ProductVersion, '1.2.3-beta.2');
    assert.doesNotMatch(readFileSync(flag('-manifest'), 'utf8'), /\{\{/);
    output = flag('-out');
    writeFileSync(output, objectFor('arm64'));
  } });
  assert.match(native.syso, /_windows_arm64\.syso$/);
  assert.ok(existsSync(native.syso));
  native.cleanup();
  native.cleanup();
  assert.equal(existsSync(native.syso), false);
  assert.equal(existsSync(output), false);
  assert.equal(readFileSync(existing, 'utf8'), 'user-owned');
});

test('cleanup preserves a user replacement at the generated syso path', t => {
  const dir = fixture(t);
  const native = prepareWindowsSyso({ root: dir, version: '1.2.3', arch: 'amd64', wails: 'wails3', run: (_, args) => writeFileSync(args[args.indexOf('-out') + 1], objectFor('amd64')) });
  unlinkSync(native.syso);
  writeFileSync(native.syso, 'replacement owned by user');
  native.cleanup();
  assert.equal(readFileSync(native.syso, 'utf8'), 'replacement owned by user');
});

test('generator failure and wrong architecture fail closed without leaving objects', t => {
  const dir = fixture(t);
  for (const mode of ['throw', 'wrong-arch']) {
    assert.throws(() => prepareWindowsSyso({ root: dir, version: '1.2.3', arch: 'amd64', wails: 'wails3', run: (_, args) => {
      writeFileSync(args[args.indexOf('-out') + 1], objectFor('arm64'));
      if (mode === 'throw') throw new Error('generator failed');
    } }), mode === 'throw' ? /generator failed/ : /Wrong COFF/);
    assert.equal(readdirSync(dir).filter(n => n.endsWith('.syso')).length, 0);
    assert.equal(readdirSync(join(dir, 'build')).filter(n => n.startsWith('.xdev-native-')).length, 0);
  }
});

function mockBuild(dir, { os = 'windows', arch = 'arm64', fail = false } = {}) {
  const mock = join(dir, 'mock-tool.mjs');
  const log = join(dir, 'commands.jsonl');
  writeFileSync(mock, `import { existsSync, readFileSync, writeFileSync, readdirSync, appendFileSync } from 'node:fs';
import { join } from 'node:path';
const args = process.argv.slice(2);
const event = { args, os: process.env.GOOS, arch: process.env.GOARCH, syso: readdirSync(process.cwd()).filter(n => n.endsWith('.syso')) };
if(args[0] === 'generate') {
  const flag = n => args[args.indexOf(n)+1];
  event.info = JSON.parse(readFileSync(flag('-info'),'utf8'));
  const object = Buffer.alloc(20); object.writeUInt16LE({amd64:0x8664,arm64:0xaa64,'386':0x14c}[flag('-arch')]);
  writeFileSync(flag('-out'),object);
}
appendFileSync(process.env.MOCK_LOG,JSON.stringify(event)+'\\n');
if(args[0] === 'build') {
  if(process.env.GOOS === 'windows' && !event.syso.some(n => n.endsWith('_windows_'+process.env.GOARCH+'.syso'))) process.exit(8);
  if(process.env.MOCK_FAIL === '1') process.exit(7);
  writeFileSync(args[args.indexOf('-o')+1],'mock binary');
}
`);
  // Node itself is the executable, so no platform-specific wrapper or shell is
  // needed: --import installs this mock before build.mjs invokes its children.
  const preload = join(dir, 'mock-spawn.mjs');
  writeFileSync(preload, `import child from 'node:child_process';
import { syncBuiltinESMExports } from 'node:module';
const original = child.spawnSync;
child.spawnSync = (exe,args,options) => original(process.execPath,[process.env.MOCK_TOOL,...args],{...options,env:{...options.env,NODE_OPTIONS:''},shell:false});
syncBuiltinESMExports();\n`);
  const result = spawnSync(process.execPath, [join(dir, 'scripts/build.mjs'), '--skip-audit', '--skip-frontend', '--skip-bindings', '--release'], {
    cwd: dir, encoding: 'utf8', env: { ...process.env, NODE_OPTIONS: `--import=${JSON.stringify(pathToFileURL(preload).href)}`, MOCK_TOOL: mock, MOCK_LOG: log, MOCK_FAIL: fail ? '1' : '0', WAILS3: 'pinned-wails', GO: 'go', GOOS: os, GOARCH: arch, XDEV_SIGNING_PUBLIC_KEY: 'fixture-key', XDEV_OUTPUT: join(dir, 'binary') },
  });
  return { result, events: existsSync(log) ? readFileSync(log, 'utf8').trim().split('\n').map(line => JSON.parse(line)) : [] };
}

test('release --skip-bindings still generates syso before go build and cleans after build failure', t => {
  for (const fail of [false, true]) {
    const dir = fixture(t);
    writeFileSync(join(dir, 'user_windows_amd64.syso'), 'keep');
    const { result, events } = mockBuild(dir, { fail });
    assert.equal(result.status, fail ? 7 : 0, result.stdout + result.stderr);
    assert.deepEqual(events.map(e => e.args[0]), ['test', 'generate', 'build']);
    assert.equal(events[1].args[1], 'syso');
    assert.equal(events[1].info.info['0000'].ProductVersion, '1.2.3-beta.2');
    assert.equal(events[2].os, 'windows');
    assert.equal(events[2].arch, 'arm64');
    assert.ok(events[2].syso.some(n => n.endsWith('_windows_arm64.syso')));
    assert.deepEqual(readdirSync(dir).filter(n => n.endsWith('.syso')), ['user_windows_amd64.syso']);
  }
});

test('a non-Windows GOOS never asks for syso even on a Windows builder', t => {
  const dir = fixture(t);
  const { result, events } = mockBuild(dir, { os: 'linux' });
  assert.equal(result.status, 0, result.stdout + result.stderr);
  assert.deepEqual(events.map(e => e.args[0]), ['test', 'build']);
  assert.ok(events[1].args.includes('desktop,production,gtk3'));
  assert.equal(events[1].os, 'linux');
});

test('Apple plist has an actual app executable/icon and valid beta CFBundleVersion', () => {
  const plist = macInfoPlist('1.0.0-beta.2');
  for (const pair of ['CFBundleExecutable</key><string>xdev', 'CFBundleIconFile</key><string>XDev.icns', 'CFBundlePackageType</key><string>APPL', 'CFBundleShortVersionString</key><string>1.0.0', 'CFBundleVersion</key><string>1.0.0b2', 'XDevVersion</key><string>1.0.0-beta.2']) assert.ok(plist.includes(pair), pair);
  assert.equal(nativeVersion('1.2.3-rc.1').apple, '1.2.3fc1');
});

// Real shell control flow, fake native packagers: the mocks inspect each staged
// consumer input before the script's EXIT trap removes its private staging.
const bash = process.env.BASH || (process.platform === 'win32' ? '' : 'bash');
test('DMG consumes XDev.app, generated icns, plist and optional Applications link', { skip: !bash && 'Set BASH to run shell packaging contracts on Windows' }, t => {
  const dir = fixture(t);
  mkdirSync(join(dir, 'build/bin'), { recursive: true });
  writeFileSync(join(dir, 'build/bin/xdev'), '#!/bin/sh\nexit 0\n');
  const mocks = join(dir, 'mocks'); mkdirSync(mocks);
  // Git Bash cannot create a dangling /Applications symlink on Windows. Check
  // that exact requested link through a mock there; Unix exercises real ln.
  if (process.platform === 'win32') writeFileSync(join(mocks, 'ln'), '#!/bin/bash\n[[ "$1" == -s && "$2" == /Applications && "$3" == */dmg/Applications ]] || exit 9\nprintf link > "$PWD/link-observed"\n');
  writeFileSync(join(mocks, 'sips'), '#!/bin/bash\n[[ "$1" == -z && -f "$4" && "$5" == --out ]] || exit 5\ncp "$4" "$6"\n');
  writeFileSync(join(mocks, 'iconutil'), '#!/bin/bash\n[[ "$1" == -c && "$2" == icns && "$4" == -o ]] || exit 5\n[[ $(find "$3" -name "*.png" | wc -l) == 10 ]] || exit 6\nprintf icns > "$5"\n');
  writeFileSync(join(mocks, 'hdiutil'), `#!/bin/bash
while [[ "$1" != -srcfolder ]]; do shift; done
APP="$2/XDev.app"
[[ -x "$APP/Contents/MacOS/xdev" && -s "$APP/Contents/Resources/XDev.icns" ]] || exit 10
cp "$APP/Contents/Info.plist" "$PWD/plist-observed"
if [[ -L "$2/Applications" ]]; then printf link > "$PWD/link-observed"; fi
printf dmg > "\${@: -1}"
`);
  const driver = `export PATH="$PWD/mocks:$PATH"\nchmod +x mocks/*\n"$NATIVE_NODE" scripts/release/native-resources.mjs plist 1.0.0-beta.2 ignored.plist\nexport NODE="$NATIVE_NODE"\nbash scripts/release/platform-packages.sh darwin arm64 v1.0.0-beta.2\n`;
  for (const link of ['1', '0']) {
    rmSync(join(dir, 'link-observed'), { force: true });
    const result = spawnSync(bash, ['-c', driver], { cwd: dir, encoding: 'utf8', env: { ...process.env, NATIVE_NODE: process.execPath.replaceAll('\\', '/'), XDEV_DMG_APPLICATIONS_LINK: link } });
    assert.equal(result.status, 0, result.stdout + result.stderr);
    assert.match(readFileSync(join(dir, 'plist-observed'), 'utf8'), /1\.0\.0b2/);
    assert.equal(existsSync(join(dir, 'link-observed')), link === '1');
    assert.ok(existsSync(join(dir, 'dist/xdev-darwin-arm64-v1.0.0-beta.2.dmg')));
  }
});

test('deb and rpm receive desktop and hicolor icon in addition to the executable', { skip: !bash && 'Set BASH to run shell packaging contracts on Windows' }, t => {
  const dir = fixture(t);
  mkdirSync(join(dir, 'build/bin'), { recursive: true });
  writeFileSync(join(dir, 'build/bin/xdev'), '#!/bin/sh\nexit 0\n');
  const mocks = join(dir, 'mocks'); mkdirSync(mocks);
  writeFileSync(join(mocks, 'sudo'), '#!/bin/bash\nexit 0\n');
  writeFileSync(join(mocks, 'sha256sum'), '#!/bin/bash\ncat >/dev/null\nexit 0\n');
  writeFileSync(join(mocks, 'curl'), `#!/bin/bash
while [[ "$1" != -o ]]; do shift; done
cat > "$2" <<'APPIMAGETOOL'
#!/bin/bash
[[ "$1" == --appimage-extract-and-run ]] || exit 13
[[ -s "$2/xdev.png" && -s "$2/.DirIcon" && -s "$2/usr/share/applications/xdev.desktop" ]] || exit 14
cmp "$2/xdev.png" build/appicon.png || exit 15
cmp "$2/.DirIcon" build/appicon.png || exit 16
printf appimage > "$3"
APPIMAGETOOL
`);
  writeFileSync(join(mocks, 'fpm'), `#!/bin/bash
TYPE=""; ROOT=""; OUT=""
while [[ $# -gt 0 ]]; do
  case "$1" in -t) TYPE="$2"; shift;; -C) ROOT="$2"; shift;; -p) OUT="$2"; shift;; esac
  shift
done
[[ -x "$ROOT/usr/bin/xdev" && -s "$ROOT/usr/share/applications/xdev.desktop" && -s "$ROOT/usr/share/icons/hicolor/256x256/apps/xdev.png" ]] || exit 11
cmp "$ROOT/usr/share/icons/hicolor/256x256/apps/xdev.png" build/appicon.png || exit 12
cp "$ROOT/usr/share/applications/xdev.desktop" "$PWD/$TYPE-desktop-observed"
printf package > "$OUT"
`);
  const result = spawnSync(bash, ['-c', 'export PATH="$PWD/mocks:$PATH"; chmod +x mocks/*; bash scripts/release/platform-packages.sh linux amd64 v1.0.0-beta.2'], { cwd: dir, encoding: 'utf8', env: process.env });
  assert.equal(result.status, 0, result.stdout + result.stderr);
  for (const type of ['deb', 'rpm']) {
    assert.match(readFileSync(join(dir, `${type}-desktop-observed`), 'utf8'), /Exec=xdev\nIcon=xdev\n/);
    assert.ok(existsSync(join(dir, `dist/xdev-1.0.0-beta.2-linux-amd64.${type}`)));
  }
});

test('native installers do not change the five canonical single-binary archive names', () => {
  assert.equal(platforms.length, 5);
  assert.deepEqual(platforms.map(([os, arch]) => archiveName('1.0.0-beta.2', os, arch)), [
    'xdev-windows-amd64-v1.0.0-beta.2.zip', 'xdev-linux-amd64-v1.0.0-beta.2.tar.gz', 'xdev-linux-arm64-v1.0.0-beta.2.tar.gz', 'xdev-darwin-amd64-v1.0.0-beta.2.tar.gz', 'xdev-darwin-arm64-v1.0.0-beta.2.tar.gz',
  ]);
});

test('all five canonical archives still contain exactly one root binary', t => {
  const dir = temp(t);
  const binary = join(dir, 'xdev');
  writeFileSync(binary, 'canonical binary fixture');
  for (const [os, arch] of platforms) {
    const output = pack(binary, join(dir, 'dist'), '1.0.0-beta.2', os, arch);
    // Windows ships bsdtar, which also reads zip; GNU tar uses unzip for zip.
    const result = os === 'windows' && process.platform !== 'win32'
      ? spawnSync('unzip', ['-Z1', output], { encoding: 'utf8' })
      : spawnSync('tar', ['-tf', output], { encoding: 'utf8' });
    assert.equal(result.status, 0, String(result.error || '') + result.stderr);
    assert.deepEqual(result.stdout.trim().split(/\r?\n/), [os === 'windows' ? 'xdev.exe' : 'xdev']);
  }
});

// Parse the linked PE resource directory instead of treating an ICO/syso file as
// embedding proof. This optional integration test uses the actual Wails CLI and
// Go linker, with an isolated tiny main package (no GUI dependencies).
function peResources(bytes) {
  const pe = bytes.readUInt32LE(0x3c);
  assert.equal(bytes.toString('ascii', pe, pe + 4), 'PE\0\0');
  const optional = pe + 24;
  const directory = optional + (bytes.readUInt16LE(optional) === 0x20b ? 112 : 96);
  const resourceRva = bytes.readUInt32LE(directory + 16);
  assert.notEqual(resourceRva, 0, 'PE must have a resource directory');
  const sections = optional + bytes.readUInt16LE(pe + 20);
  const offsetOf = rva => {
    for (let i = 0; i < bytes.readUInt16LE(pe + 6); i++) {
      const section = sections + i * 40;
      const start = bytes.readUInt32LE(section + 12);
      if (rva >= start && rva < start + Math.max(bytes.readUInt32LE(section + 8), bytes.readUInt32LE(section + 16))) return bytes.readUInt32LE(section + 20) + rva - start;
    }
    throw new Error(`Unmapped PE RVA ${rva}`);
  };
  const base = offsetOf(resourceRva);
  const records = [];
  const walk = (relative, path = []) => {
    const table = base + relative;
    const count = bytes.readUInt16LE(table + 12) + bytes.readUInt16LE(table + 14);
    for (let i = 0; i < count; i++) {
      const entry = table + 16 + i * 8;
      const id = bytes.readUInt32LE(entry);
      const next = bytes.readUInt32LE(entry + 4);
      if (next & 0x80000000) walk(next & 0x7fffffff, [...path, id]);
      else {
        const data = base + next;
        const offset = offsetOf(bytes.readUInt32LE(data));
        const size = bytes.readUInt32LE(data + 4);
        records.push({ path: [...path, id], bytes: bytes.subarray(offset, offset + size) });
      }
    }
  };
  walk(0);
  return records;
}
const actualBinary = process.env.XDEV_NATIVE_BINARY;
test('actual desktop PE consumes source ICO and the current version constant', { skip: !actualBinary && 'Set XDEV_NATIVE_BINARY to inspect a built desktop executable' }, () => {
  const records = peResources(readFileSync(actualBinary));
  const ico = readFileSync(join(root, 'build/windows/icon.ico'));
  const icons = records.filter(r => r.path[0] === 3);
  assert.equal(icons.length, ico.readUInt16LE(4));
  for (let i = 0; i < ico.readUInt16LE(4); i++) {
    const entry = 6 + i * 16;
    const payload = ico.subarray(ico.readUInt32LE(entry + 12), ico.readUInt32LE(entry + 12) + ico.readUInt32LE(entry + 8));
    assert.ok(icons.some(r => r.bytes.equals(payload)), 'actual RT_ICON must consume source ICO payload bytes');
  }
  assert.ok(records.some(r => r.path[0] === 14));
  const version = readFileSync(join(root, 'internal/version/version.go'), 'utf8').match(/const Version = "([^"]+)"/)[1];
  const expected = nativeVersion(version);
  const manifest = records.find(r => r.path[0] === 24)?.bytes.toString('utf8');
  assert.ok(manifest?.includes('io.github.xmdemt.xdev'));
  assert.ok(manifest?.includes(expected.windows));
  const versionBytes = records.find(r => r.path[0] === 16)?.bytes;
  assert.ok(versionBytes?.includes(Buffer.from(expected.full, 'utf16le')));
  assert.ok(versionBytes?.includes(Buffer.from('XDev', 'utf16le')));
  const fixed = versionBytes.indexOf(Buffer.from([0xbd, 0x04, 0xef, 0xfe]));
  assert.ok(fixed >= 0, 'VS_FIXEDFILEINFO signature');
  const numbers = expected.windows.split('.').map(Number);
  assert.equal(versionBytes.readUInt32LE(fixed + 8), numbers[0] * 65536 + numbers[1]);
  assert.equal(versionBytes.readUInt32LE(fixed + 12), numbers[2] * 65536 + numbers[3]);
});

const wails = process.env.WAILS3;
test('real Wails + Go link embeds matching ICO payloads, manifest and full beta version', { skip: !wails && 'Set WAILS3 to run real PE embedding verification' }, t => {
  const dir = fixture(t);
  writeFileSync(join(dir, 'go.mod'), 'module nativefixture\n\ngo 1.20\n');
  writeFileSync(join(dir, 'main.go'), 'package main\nfunc main() {}\n');
  const run = (exe, args) => {
    const result = spawnSync(exe, args, { cwd: dir, encoding: 'utf8', env: { ...process.env, GOOS: 'windows', CGO_ENABLED: '0' } });
    assert.equal(result.status, 0, String(result.error || '') + result.stdout + result.stderr);
  };
  const ico = readFileSync(join(dir, 'build/windows/icon.ico'));
  const payloads = [];
  for (let i = 0; i < ico.readUInt16LE(4); i++) {
    const entry = 6 + i * 16;
    payloads.push(ico.subarray(ico.readUInt32LE(entry + 12), ico.readUInt32LE(entry + 12) + ico.readUInt32LE(entry + 8)));
  }
  for (const arch of ['amd64', 'arm64']) {
    const native = prepareWindowsSyso({ root: dir, version: '1.2.3-beta.2', arch, wails, run });
    const output = join(dir, `${arch}.exe`);
    try {
      const result = spawnSync(process.env.GO || 'go', ['build', '-o', output, '.'], { cwd: dir, encoding: 'utf8', env: { ...process.env, GOOS: 'windows', GOARCH: arch, CGO_ENABLED: '0', GOWORK: 'off' } });
      assert.equal(result.status, 0, String(result.error || '') + result.stdout + result.stderr);
    } finally { native.cleanup(); }
    const records = peResources(readFileSync(output));
    const icons = records.filter(r => r.path[0] === 3);
    assert.equal(icons.length, payloads.length);
    for (const payload of payloads) assert.ok(icons.some(r => r.bytes.equals(payload)), 'linked RT_ICON bytes must match source ICO payload');
    assert.ok(records.some(r => r.path[0] === 14), 'RT_GROUP_ICON must be linked');
    const manifest = records.find(r => r.path[0] === 24)?.bytes.toString('utf8');
    assert.match(manifest, /io\.github\.xmdemt\.xdev/);
    assert.match(manifest, /1\.2\.3\.2/);
    const version = records.find(r => r.path[0] === 16)?.bytes;
    assert.ok(version?.includes(Buffer.from('1.2.3-beta.2', 'utf16le')), 'RT_VERSION preserves full semver');
    assert.ok(version?.includes(Buffer.from('XDev', 'utf16le')));
  }
});
