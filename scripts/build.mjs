import { execFileSync } from 'node:child_process';
import { mkdirSync, writeFileSync, readFileSync } from 'node:fs';
import { createHash } from 'node:crypto';
import { pnpm, run } from './process.mjs';
import { metadata } from './version.mjs';

const git = (...args) => execFileSync('git', args, {encoding: 'utf8', windowsHide: true, stdio: ['ignore', 'pipe', 'ignore']}).trim();
const optionalGit = (...args) => { try { return git(...args); } catch { return ''; } };
const release = process.argv.includes('--release');
const build = metadata({
  commit: git('rev-parse', 'HEAD'),
  exactTag: optionalGit('describe', '--exact-match', '--tags', 'HEAD'),
  nearestTag: optionalGit('describe', '--tags', '--abbrev=0', '--match', 'v[0-9]*'),
  version: process.argv.find(v => v.startsWith('--version='))?.slice(10) ?? JSON.parse(readFileSync('web/package.json', 'utf8')).version,
  release,
  dirty: !!git('status', '--porcelain'),
});
pnpm(['--dir', 'web', 'build']);
mkdirSync('dist', {recursive: true});
const targets = release ? [['linux', 'amd64'], ['linux', 'arm64'], ['windows', 'amd64']] : [[process.platform === 'win32' ? 'windows' : process.platform === 'darwin' ? 'darwin' : 'linux', process.arch === 'arm64' ? 'arm64' : 'amd64']];
const builtAt = new Date().toISOString();
const checksums = [];
for (const [os, arch] of targets) {
  const output = release ? `dist/donate-${build.version}-${os}-${arch}${os === 'windows' ? '.exe' : ''}` : `dist/donate${os === 'windows' ? '.exe' : ''}`;
  run('go', ['build', '-trimpath', '-ldflags', `-s -w -X main.version=${build.version} -X main.commit=${build.commit} -X main.tag=${build.tag} -X main.builtAt=${builtAt}`, '-o', output, './cmd/donate'], {env: {...process.env, CGO_ENABLED: '0', GOOS: os, GOARCH: arch}});
  checksums.push(`${createHash('sha256').update(readFileSync(output)).digest('hex')}  ${output.slice(5)}`);
}
writeFileSync('dist/SHA256SUMS', checksums.join(String.fromCharCode(10)) + String.fromCharCode(10));
