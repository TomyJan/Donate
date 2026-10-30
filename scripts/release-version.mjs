import { execFileSync } from 'node:child_process';
import { readFileSync } from 'node:fs';
import { pathToFileURL } from 'node:url';

export const versionPattern = /^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)$/;
function parse(value) {
  if (!versionPattern.test(value)) throw new Error('Version must be x.y.z without leading zeroes');
  return value.split('.').map(BigInt);
}
function compare(a, b) {
  for (let i = 0; i < 3; i++) if (a[i] !== b[i]) return a[i] > b[i] ? 1 : -1;
  return 0;
}
export function nextVersion(tags, base, bump = 'patch') {
  if (!['patch', 'minor', 'major'].includes(bump)) throw new Error('Bump must be patch, minor or major');
  const versions = [parse(base), ...tags.filter(tag => tag.startsWith('v') && versionPattern.test(tag.slice(1))).map(tag => parse(tag.slice(1)))];
  versions.sort(compare);
  const version = [...versions.at(-1)];
  const index = {major: 0, minor: 1, patch: 2}[bump];
  version[index]++;
  for (let i = index + 1; i < 3; i++) version[i] = 0n;
  return version.join('.');
}
if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  const tags = execFileSync('git', ['tag', '--list'], {encoding: 'utf8'}).trim().split(/\r?\n/);
  const base = JSON.parse(readFileSync('web/package.json', 'utf8')).version;
  console.log(nextVersion(tags, base, process.argv[2]));
}
