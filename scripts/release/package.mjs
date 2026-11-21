#!/usr/bin/env node
// Canonical updater payload producer. Windows stages a direct executable and
// retains the single-member ZIP; Unix uses single-member tarballs.
import { mkdirSync, mkdtempSync, copyFileSync, readdirSync, createReadStream, writeFileSync, lstatSync } from 'node:fs';
import { resolve, join } from 'node:path';
import { tmpdir } from 'node:os';
import { spawnSync } from 'node:child_process';
import { createHash } from 'node:crypto';
import { pathToFileURL } from 'node:url';

export const platforms = [
 ['windows','amd64'], ['linux','amd64'], ['linux','arm64'], ['darwin','amd64'], ['darwin','arm64'],
];
export function archiveName(version, os, arch) {
 if (!platforms.some(([o,a]) => o === os && a === arch)) throw new Error(`Unsupported release platform: ${os}/${arch}`);
 if (!/^[0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z.-]+)?$/.test(version.replace(/^v/,''))) throw new Error('Invalid release version');
 return `xdev-${os}-${arch}-v${version.replace(/^v/,'')}.${os === 'windows' ? 'zip' : 'tar.gz'}`;
}
function windowsExeName(version, arch) {
 return archiveName(version,'windows',arch).replace(/\.zip$/, '.exe');
}
function run(command,args,cwd) {
 const r=spawnSync(command,args,{cwd,stdio:'inherit'});
 if(r.error) throw r.error;
 if(r.status!==0) throw new Error(`${command} failed (${r.status})`);
}
export async function hashFile(path) {
 const h=createHash('sha256');
 for await (const chunk of createReadStream(path)) h.update(chunk);
 return h.digest('hex');
}
export async function checksums(directory,version) {
 const expected=platforms.map(([os,arch])=>archiveName(version,os,arch));
 const names=readdirSync(directory).filter(n=>n!=='SHA256SUMS' && n!=='SHA256SUMS.sig' && !n.endsWith('.json')).sort();
 for(const name of expected) if(!names.includes(name)) throw new Error(`Missing required update archive: ${name}`);
 for(const [os,arch] of platforms) {
  if(os!=='windows') continue;
  const name=windowsExeName(version,arch);
  if(!names.includes(name)) throw new Error(`Missing required update executable: ${name}`);
  const info=lstatSync(join(directory,name));
  if(!info.isFile() || info.size===0) throw new Error(`Invalid required update executable: ${name}`);
 }
 const lines=[];
 for(const name of names) lines.push(`${await hashFile(join(directory,name))}  ${name}`);
 writeFileSync(join(directory,'SHA256SUMS'),lines.join('\n')+'\n');
 return lines;
}
export function pack(binary,directory,version,os,arch) {
 const name=archiveName(version,os,arch);
 const staging=mkdtempSync(join(tmpdir(),'xdev-package-'));
 const member=os==='windows'?'xdev.exe':'xdev';
 const stagedBinary=join(staging,member);
 copyFileSync(binary,stagedBinary);
 mkdirSync(directory,{recursive:true});
 const output=resolve(directory,name);
 if(os==='windows') {
  if(process.platform==='win32') run('powershell.exe',['-NoProfile','-Command',`Compress-Archive -LiteralPath '${stagedBinary.replaceAll("'","''")}' -DestinationPath '${output.replaceAll("'","''")}' -Force`]);
  else run('zip',['-j',output,stagedBinary]);
  copyFileSync(stagedBinary,resolve(directory,windowsExeName(version,arch)));
 } else run('tar',['-czf',output,'-C',staging,member]);
 // Staging is deliberately confined to an owned temp directory. OS temp cleanup
 // can reclaim it; no recursive deletion is needed in a packaging command.
 return output;
}
async function main() {
 const [mode,...args]=process.argv.slice(2);
 if(mode==='pack') {
  if(args.length!==5) throw new Error('Usage: package.mjs pack BINARY OUT_DIR VERSION OS ARCH');
  console.log(pack(...args));
 } else if(mode==='checksums') {
  if(args.length!==2) throw new Error('Usage: package.mjs checksums OUT_DIR VERSION');
  await checksums(resolve(args[0]),args[1]);
 } else throw new Error('Expected pack or checksums');
}
const invokedPath = process.argv[1];
if (invokedPath && import.meta.url === pathToFileURL(invokedPath).href) await main();
