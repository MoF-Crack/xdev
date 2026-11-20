import { test } from 'node:test';
import assert from 'node:assert/strict';
import { mkdtempSync, writeFileSync, readFileSync, rmSync } from 'node:fs';
import { join } from 'node:path';
import { tmpdir } from 'node:os';
import { archiveName, checksums, platforms, hashFile } from './package.mjs';

test('canonical assets match updater naming for every supported platform', () => {
 assert.equal(archiveName('v1.2.3','windows','amd64'),'xdev-windows-amd64-v1.2.3.zip');
 assert.equal(archiveName('1.2.3','darwin','arm64'),'xdev-darwin-arm64-v1.2.3.tar.gz');
 assert.equal(archiveName('1.2.3','linux','arm64'),'xdev-linux-arm64-v1.2.3.tar.gz');
 assert.throws(()=>archiveName('../bad','linux','amd64'));
 assert.throws(()=>archiveName('1.2.3','other','amd64'));
});
test('final checksums refuse partial releases and include all staged assets', async t => {
 const directory=mkdtempSync(join(tmpdir(),'xdev-release-test-'));
 t.after(()=>rmSync(directory,{recursive:true,force:true}));
 writeFileSync(join(directory,archiveName('1.2.3','linux','amd64')),'partial');
 await assert.rejects(checksums(directory,'1.2.3'),/Missing required update archive/);
 for(const [os,arch] of platforms) writeFileSync(join(directory,archiveName('1.2.3',os,arch)),`${os}/${arch}`);
 const rawName='xdev-windows-amd64-v1.2.3.exe';
 await assert.rejects(checksums(directory,'1.2.3'),/Missing required update executable: xdev-windows-amd64-v1\.2\.3\.exe/);
 writeFileSync(join(directory,rawName),'');
 await assert.rejects(checksums(directory,'1.2.3'),/Invalid required update executable/);
 writeFileSync(join(directory,rawName),'direct Windows binary');
 writeFileSync(join(directory,'extra.dmg'),'installer');
 const first=await checksums(directory,'1.2.3');
 assert.equal(first.length,7);
 const expected=await hashFile(join(directory,'extra.dmg'));
 assert.ok(first.includes(`${expected}  extra.dmg`));
 const rawHash=await hashFile(join(directory,rawName));
 assert.ok(first.includes(`${rawHash}  ${rawName}`));
 const second=await checksums(directory,'1.2.3');
 assert.deepEqual(second,first);
 assert.equal(readFileSync(join(directory,'SHA256SUMS'),'utf8'),first.join('\n')+'\n');
});
