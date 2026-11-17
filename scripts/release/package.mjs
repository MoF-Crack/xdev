#!/usr/bin/env node
// Canonical updater archive producer. The updater consumes exactly one root
// member (xdev or xdev.exe); package validation is mandatory before publishing.
import { mkdirSync, mkdtempSync, copyFileSync, readdirSync, createReadStream, writeFileSync, readFileSync } from 'node:fs';
import { dirname, resolve, join, basename } from 'node:path';
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
 const names=readdirSync(directory).filter(n=>n!=='SHA256SUMS' && !n.endsWith('.json')).sort();
 for(const name of expected) if(!names.includes(name)) throw new Error(`Missing required update archive: ${name}`);
 const lines=[];
 for(const name of names) lines.push(`${await hashFile(join(directory,name))}  ${name}`);
 writeFileSync(join(directory,'SHA256SUMS'),lines.join('\n')+'\n');
 return lines;
}
export function pack(binary,directory,version,os,arch) {
 const name=archiveName(version,os,arch);
 const staging=mkdtempSync(join(tmpdir(),'xdev-package-'));
 const member=os==='windows'?'xdev.exe':'xdev';
 copyFileSync(binary,join(staging,member));
 mkdirSync(directory,{recursive:true});
 const output=resolve(directory,name);
 if(os==='windows') {
  if(process.platform==='win32') run('powershell.exe',['-NoProfile','-Command',`Compress-Archive -LiteralPath '${join(staging,member).replaceAll("'","''")}' -DestinationPath '${output.replaceAll("'","''")}' -Force`]);
  else run('zip',['-j',output,join(staging,member)]);
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
if(import.meta.url===pathToFileURL(process.argv[1]).href) await main();
