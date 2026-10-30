import { execFileSync, spawnSync } from 'node:child_process';
import { statSync } from 'node:fs';
import { versionPattern } from './release-version.mjs';

const run = (cmd, args) => execFileSync(cmd, args, {encoding: 'utf8', stdio: ['ignore', 'pipe', 'pipe']}).trim();
const sha = process.env.SOURCE_SHA, version = process.env.RELEASE_VERSION;
if (!/^[0-9a-f]{40}$/.test(sha ?? '') || !versionPattern.test(version ?? '')) throw new Error('Invalid release metadata');
const tag = 'v' + version;
if (run('git', ['rev-parse', 'HEAD']) !== sha) throw new Error('Checkout differs from the tested source');
const refs = run('git', ['ls-remote', 'origin', 'refs/tags/' + tag, 'refs/tags/' + tag + '^{}']).split(/\r?\n/).filter(Boolean).map(line => line.split(/\s+/));
const remote = refs.find(([, ref]) => ref.endsWith('^{}')) ?? refs[0];
if (remote?.[0] !== sha) throw new Error('Remote release tag changed after verification');
const assets = ['release-assets/donate-' + version + '-linux-amd64', 'release-assets/SHA256SUMS.txt'];
if (assets.some(path => !statSync(path).isFile() || statSync(path).size === 0)) throw new Error('Release assets must be non-empty files');
const repo = process.env.GITHUB_REPOSITORY;
if (!repo || !/^[\w.-]+\/[\w.-]+$/.test(repo)) throw new Error('Invalid repository');
const existing = spawnSync('gh', ['api', 'repos/' + repo + '/releases/tags/' + tag], {encoding: 'utf8'});
if (existing.status === 0) {
  const release = JSON.parse(existing.stdout);
  if (release.draft) throw new Error('An existing draft release needs explicit review');
  console.log(run('gh', ['release', 'upload', tag, ...assets, '--clobber']));
} else {
  if (!/HTTP 404/.test(existing.stderr ?? '')) throw new Error('Could not check existing release: ' + (existing.stderr || existing.error?.message || 'unknown error'));
  console.log(run('gh', ['release', 'create', tag, '--verify-tag', '--target', sha, '--title', 'V' + version, '--generate-notes', ...assets]));
}
