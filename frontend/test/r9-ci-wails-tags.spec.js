// Wails 3: distributed binaries use the shared desktop build; maintenance
// commands intentionally omit desktop and must not require generated assets.
import { describe, expect, it } from 'vitest';
import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
const root=join(dirname(fileURLToPath(import.meta.url)),'..','..');
const read=p=>readFileSync(join(root,p),'utf8');
describe('Wails 3 distribution contract',()=>{
 it('CI builds distributed binaries through the shared tagged builder',()=>{
  const ci=read('.github/workflows/ci.yaml');
  expect(ci).toContain('node scripts/build.mjs --skip-audit');
  expect(ci).toContain('node scripts/build.mjs --release');
  const build=read('scripts/build.mjs');
  expect(build).toContain("'desktop,production'");
  expect(build).toContain("'-tags', tags");
  expect(ci).toContain('Require complete signature coverage');
  expect(ci).toContain("XDEV_REQUIRE_SIGNATURE_COVERAGE: '1'");
   expect(ci).toContain("prerelease: ${{ contains(github.ref_name, '-') }}");
 });
 it('the Linux generator uses the same GTK3 backend as the desktop build',()=>{
   const ci=read('.github/workflows/ci.yaml');
   expect(ci).toContain('go install -tags gtk3 github.com/wailsapp/wails/v3/cmd/wails3@v3.1.0');
   expect(ci).toContain('libgtk-3-dev libwebkit2gtk-4.1-dev');
  });
  it('local entry points delegate to the same build module',()=>{
  expect(read('build.ps1')).toContain('scripts\\build.mjs');
  expect(read('build.sh')).toContain('scripts/build.mjs');
  expect(read('scripts/build.bat')).toContain('build.mjs');
 });
 it('Windows lint cannot be masked by a later native command',()=>{
   const ci=read('.github/workflows/ci.yaml');
   expect(ci).toMatch(/- name: Lint\s+run: go vet \.\/\.\.\.\s+- name: Test\s+run: go test/);
 });
 it('maintenance compilation does not require embedded frontend assets',()=>{
  expect(read('frontend/embed.go')).toMatch(/^\/\/go:build desktop/);
  expect(read('internal/gui/gui.go')).toMatch(/^\/\/go:build desktop/);
  expect(read('internal/gui/gui_headless.go')).toMatch(/^\/\/go:build !desktop/);
 });
});
