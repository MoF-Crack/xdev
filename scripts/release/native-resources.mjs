// Native resources are rendered from the same version constant as the binary.
// Wails generate syso consumes concrete JSON/XML; it does NOT render templates.
import { existsSync, linkSync, mkdirSync, mkdtempSync, readFileSync, rmSync, statSync, unlinkSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';
import { pathToFileURL } from 'node:url';

// Existing release jobs skip binding generation and may not install the CLI.
// Resolve that missing tool with the exact version already pinned by go.mod;
// a real generator failure is never hidden by a fallback.
export function runPinnedResourceGenerator({ exe, commandArgs, go, wailsVersion, run }) {
  try { run(exe, commandArgs); }
  catch (error) {
    if (error.code !== 'ENOENT') throw error;
    if (!/^v\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?$/.test(wailsVersion)) throw new Error('Invalid pinned Wails resource generator version');
    run(go, ['run', `github.com/wailsapp/wails/v3/cmd/wails3@${wailsVersion}`, ...commandArgs]);
  }
}

export function nativeVersion(value) {
  const match = /^(\d+)\.(\d+)\.(\d+)(?:-([0-9A-Za-z.-]+))?$/.exec(value.replace(/^v/, ''));
  if (!match) throw new Error(`Invalid native resource version: ${value}`);
  const core = match.slice(1, 4).map(Number);
  const prerelease = match[4] || '';
  const revision = Number(prerelease.match(/(?:^|\.)(\d+)$/)?.[1] || 0);
  if ([...core, revision].some(n => !Number.isSafeInteger(n) || n > 65535)) throw new Error('Windows resource version components must fit uint16');
  const appleStage = prerelease.match(/^(alpha|beta|rc)\.(\d+)$/);
  // CFBundleVersion uses Apple's prerelease syntax, not a semver hyphen.
  const apple = appleStage && revision > 0 && revision <= 255
    ? `${core.join('.')}${{ alpha: 'a', beta: 'b', rc: 'fc' }[appleStage[1]]}${revision}`
    : core.join('.');
  return { full: value.replace(/^v/, ''), core: core.join('.'), windows: [...core, revision].join('.'), prerelease, apple };
}

export function renderWindowsResources(root, version) {
  const v = nativeVersion(version);
  const tokens = { XDEV_VERSION: v.full, XDEV_WINDOWS_VERSION: v.windows, XDEV_PRERELEASE_FLAGS: v.prerelease ? 'Prerelease' : '' };
  const render = file => {
    const result = readFileSync(join(root, 'build/windows', file), 'utf8').replace(/\{\{(XDEV_[A-Z_]+)\}\}/g, (token, key) => tokens[key] ?? token);
    if (/\{\{/.test(result)) throw new Error(`Unrendered native resource template: ${file}`);
    return result;
  };
  return { manifest: render('wails.exe.manifest'), info: render('info.json') };
}

// Keep Wails output outside the package until complete. linkSync publishes it
// exclusively: neither an existing nor concurrently-created user file is replaced.
// The GOOS/GOARCH suffix makes Go select only this Windows target's resource.
export function prepareWindowsSyso({ root, version, arch, wails, run }) {
  if (!['amd64', 'arm64', '386'].includes(arch)) throw new Error(`Unsupported Windows resource architecture: ${arch}`);
  const icon = join(root, 'build/windows/icon.ico');
  if (!existsSync(icon)) throw new Error(`Windows icon missing: ${icon}`);
  const rendered = renderWindowsResources(root, version);
  // Same filesystem as root is required for the exclusive hard-link publish.
  mkdirSync(join(root, 'build'), { recursive: true });
  const staging = mkdtempSync(join(root, 'build', '.xdev-native-'));
  const token = staging.split(/[\\/]/).at(-1).replace(/^\.xdev-native-/, '');
  const syso = join(root, `xdev_native_${token}_windows_${arch}.syso`);
  let ownedIdentity;
  const cleanup = () => {
    // Do not delete a path that another process/user has replaced since linking.
    if (ownedIdentity && existsSync(syso)) {
      const current = statSync(syso, { bigint: true });
      if (current.dev === ownedIdentity.dev && current.ino === ownedIdentity.ino) unlinkSync(syso);
    }
    rmSync(staging, { recursive: true, force: true });
  };
  try {
    const manifest = join(staging, 'xdev.exe.manifest');
    const info = join(staging, 'info.json');
    const object = join(staging, 'resources.syso');
    writeFileSync(manifest, rendered.manifest);
    writeFileSync(info, rendered.info);
    run(wails, ['generate', 'syso', '-manifest', manifest, '-info', info, '-icon', icon, '-out', object, '-arch', arch]);
    if (!existsSync(object) || statSync(object).size === 0) throw new Error('Wails did not produce the Windows resource object');
    const machine = readFileSync(object).readUInt16LE(0);
    if (machine !== { amd64: 0x8664, arm64: 0xaa64, '386': 0x14c }[arch]) throw new Error(`Wrong COFF architecture in generated Windows resource: ${arch}`);
    linkSync(object, syso);
    ownedIdentity = statSync(syso, { bigint: true });
    return { syso, cleanup };
  } catch (error) {
    cleanup();
    throw error;
  }
}

const xml = value => value.replaceAll('&', '&amp;').replaceAll('<', '&lt;').replaceAll('>', '&gt;').replaceAll('"', '&quot;').replaceAll("'", '&apos;');
export function macInfoPlist(version) {
  const v = nativeVersion(version);
  const entries = {
    CFBundleName: 'XDev', CFBundleDisplayName: 'XDev', CFBundleExecutable: 'xdev',
    CFBundleIdentifier: 'io.github.xmdemt.xdev', CFBundlePackageType: 'APPL',
    CFBundleIconFile: 'XDev.icns', CFBundleShortVersionString: v.core,
    CFBundleVersion: v.apple, XDevVersion: v.full, NSHighResolutionCapable: true,
    NSPrincipalClass: 'NSApplication',
  };
  return `<?xml version="1.0" encoding="UTF-8"?>\n<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">\n<plist version="1.0"><dict>\n${Object.entries(entries).map(([key, value]) => `<key>${key}</key>${value === true ? '<true/>' : `<string>${xml(value)}</string>`}`).join('\n')}\n</dict></plist>\n`;
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  const [mode, version, output] = process.argv.slice(2);
  if (mode !== 'plist' || !version || !output) throw new Error('Usage: native-resources.mjs plist VERSION OUTPUT');
  writeFileSync(output, macInfoPlist(version));
}
