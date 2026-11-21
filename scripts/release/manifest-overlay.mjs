import { existsSync, lstatSync, mkdtempSync, readdirSync, rmSync, writeFileSync } from 'node:fs';
import { join, resolve } from 'node:path';
import { tmpdir } from 'node:os';

const isManifestInput = name => !name.startsWith('.') && (name.endsWith('.yaml') || name.endsWith('.yaml.sig'));

// Create a Go -overlay mapping from the checkout's manifest paths to the
// signed release inputs. Keeping generated signatures outside the Git working
// tree preserves accurate Go VCS metadata while embedding the exact signed
// bytes. The caller owns the returned tempDir and must remove it after build.
export function createManifestOverlay(repositoryRoot, signedInputRoot) {
  const root = resolve(repositoryRoot);
  const signedRoot = resolve(signedInputRoot);
  const targetDir = join(root, 'manifests');
  const sourceDir = join(signedRoot, 'manifests');
  const publicKeyPath = join(signedRoot, 'keys', 'pub.key');
  if (!existsSync(sourceDir) || !lstatSync(sourceDir).isDirectory()) {
    throw new Error(`Signed manifest input directory is missing: ${sourceDir}`);
  }
  if (!existsSync(publicKeyPath) || !lstatSync(publicKeyPath).isFile()) {
    throw new Error(`Signed release public key is missing: ${publicKeyPath}`);
  }

  const required = readdirSync(targetDir).filter(isManifestInput).sort();
  const supplied = readdirSync(sourceDir).filter(isManifestInput).sort();
  if (required.length === 0) throw new Error(`No manifest inputs found in checkout: ${targetDir}`);
  const missing = required.filter(name => !supplied.includes(name));
  const unexpected = supplied.filter(name => !required.includes(name) &&
    !(name.endsWith('.yaml.sig') && required.includes(name.slice(0, -4))));
  if (missing.length || unexpected.length) {
    throw new Error(`Signed manifest input set differs from checkout (missing: ${missing.join(', ') || 'none'}; unexpected: ${unexpected.join(', ') || 'none'})`);
  }

  const replace = {};
  for (const name of supplied) {
    const source = join(sourceDir, name);
    const target = join(targetDir, name);
    if (!lstatSync(source).isFile()) throw new Error(`Signed manifest input is not a regular file: ${source}`);
    replace[target] = source;
  }
  const tempDir = mkdtempSync(join(tmpdir(), 'xdev-build-overlay-'));
  const overlayPath = join(tempDir, 'go-overlay.json');
  try {
    writeFileSync(overlayPath, JSON.stringify({ Replace: replace }));
  } catch (error) {
    rmSync(tempDir, { recursive: true, force: true });
    throw error;
  }
  return { path: overlayPath, tempDir, signedRoot, publicKeyPath, fileCount: supplied.length };
}

export function removeManifestOverlay(overlay) {
  if (overlay?.tempDir) rmSync(overlay.tempDir, { recursive: true, force: true });
}
