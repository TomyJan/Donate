import { test } from 'node:test';
import assert from 'node:assert/strict';
import { execFileSync, spawnSync } from 'node:child_process';
import {
  copyFileSync,
  mkdtempSync,
  mkdirSync,
  readdirSync,
  readFileSync,
  writeFileSync,
  rmSync,
} from 'node:fs';
import { createHash } from 'node:crypto';
import { tmpdir } from 'node:os';
import { join, resolve } from 'node:path';

const git = (cwd, ...args) =>
  execFileSync('git', ['-c', 'commit.gpgsign=false', '-c', 'tag.gpgsign=false', ...args], {
    cwd,
    encoding: 'utf8',
    stdio: ['ignore', 'pipe', 'pipe'],
  }).trim();
function fixture(t) {
  const root = mkdtempSync(join(tmpdir(), 'donate-release-'));
  t.after(() => rmSync(root, { recursive: true, force: true }));
  const repo = join(root, 'repo'),
    remote = join(root, 'remote.git');
  mkdirSync(repo);
  git(root, 'init', '--bare', remote);
  git(repo, 'init', '-b', 'master');
  git(repo, 'config', 'user.name', 'Release test');
  git(repo, 'config', 'user.email', 'release@example.invalid');
  git(repo, 'config', 'commit.gpgsign', 'false');
  git(repo, 'config', 'tag.gpgsign', 'false');
  mkdirSync(join(repo, 'web'));
  mkdirSync(join(repo, 'cmd/donate'), { recursive: true });
  writeFileSync(
    join(repo, 'web/package.json'),
    JSON.stringify({ name: '@donate/web', version: '0.0.1' }) + '\n',
  );
  writeFileSync(join(repo, 'cmd/donate/version.go'), 'package main\n\nvar version = "0.0.1"\n');
  git(repo, 'add', '.');
  git(repo, 'commit', '-m', 'initial');
  git(repo, 'remote', 'add', 'origin', remote);
  git(repo, 'push', 'origin', 'master');
  return { root, repo, remote, sha: git(repo, 'rev-parse', 'HEAD') };
}
function run(script, repo, env = {}, args = []) {
  return spawnSync(process.execPath, [resolve(import.meta.dirname, script), ...args], {
    cwd: repo,
    encoding: 'utf8',
    env: { ...process.env, ...env },
    windowsHide: true,
  });
}

test('version bump uses the highest stable version and supports patch, minor and major', async () => {
  const { nextVersion } = await import('./release-version.mjs');
  assert.equal(nextVersion([], '0.0.1'), '0.0.2');
  assert.equal(nextVersion(['v1.9.9', 'v1.10.2', 'v2.0.0-rc.1', 'noise'], '0.0.1', 'patch'), '1.10.3');
  assert.equal(nextVersion(['v1.10.2'], '0.0.1', 'minor'), '1.11.0');
  assert.equal(nextVersion(['v1.10.2'], '0.0.1', 'major'), '2.0.0');
  assert.equal(nextVersion(['v0.0.1'], '1.2.3'), '1.2.4');
  assert.throws(() => nextVersion([], '01.2.3'));
  assert.throws(() => nextVersion([], '1.2.3', 'oops'));
});

test('version synchronization updates frontend and Go defaults without changing other fields', (t) => {
  const { repo } = fixture(t);
  const result = run('set-release-version.mjs', repo, {}, ['1.2.3']);
  assert.equal(result.status, 0, result.stderr);
  assert.deepEqual(JSON.parse(readFileSync(join(repo, 'web/package.json'), 'utf8')), {
    name: '@donate/web',
    version: '1.2.3',
  });
  assert.match(readFileSync(join(repo, 'cmd/donate/version.go'), 'utf8'), /var version = "1\.2\.3"/);
  const invalid = run('set-release-version.mjs', repo, {}, ['1.2.3;bad']);
  assert.notEqual(invalid.status, 0);
  assert.equal(JSON.parse(readFileSync(join(repo, 'web/package.json'), 'utf8')).version, '1.2.3');
});

test('release accepts an existing annotated remote tag without changing it', (t) => {
  const { root, repo, remote } = fixture(t);
  const synced = run('set-release-version.mjs', repo, {}, ['1.2.3']);
  assert.equal(synced.status, 0, synced.stderr);
  git(repo, 'add', '.');
  git(repo, 'commit', '-m', 'release');
  const source = git(repo, 'rev-parse', 'HEAD');
  git(repo, 'tag', '-a', 'v1.2.3', '-m', 'release');
  git(repo, 'push', '--atomic', 'origin', 'master', 'refs/tags/v1.2.3');
  const output = join(root, 'output');
  const result = run('release-prepare.mjs', repo, {
    RELEASE_TAG: 'v1.2.3',
    GITHUB_REF: 'refs/tags/v1.2.3',
    SOURCE_SHA: source,
    GITHUB_OUTPUT: output,
    DEFAULT_BRANCH: 'master',
  });
  assert.equal(result.status, 0, result.stderr);
  assert.equal(readFileSync(output, 'utf8'), 'sha=' + source + '\nversion=1.2.3\ntag=v1.2.3\n');
  assert.equal(git(remote, 'rev-parse', 'v1.2.3^{}'), source);
});

test('release refuses an absent tag, wrong source, mismatched versions or off-branch tag', (t) => {
  const { root, repo, sha } = fixture(t);
  const env = {
    RELEASE_TAG: 'v0.0.1',
    GITHUB_REF: 'refs/tags/v0.0.1',
    SOURCE_SHA: sha,
    GITHUB_OUTPUT: join(root, 'output'),
    DEFAULT_BRANCH: 'master',
  };
  assert.notEqual(run('release-prepare.mjs', repo, env).status, 0);
  git(repo, 'tag', 'v0.0.1');
  git(repo, 'tag', 'v9.9.9');
  git(repo, 'push', 'origin', '--tags');
  const accepted = run('release-prepare.mjs', repo, env);
  assert.equal(accepted.status, 0, accepted.stderr);
  const branchEvent = run('release-prepare.mjs', repo, { ...env, GITHUB_REF: 'refs/heads/master' });
  assert.notEqual(branchEvent.status, 0);
  assert.match(branchEvent.stderr, /Release must run on its version tag/);
  assert.notEqual(run('release-prepare.mjs', repo, { ...env, SOURCE_SHA: '' }).status, 0);
  assert.notEqual(run('release-prepare.mjs', repo, { ...env, SOURCE_SHA: 'a'.repeat(40) }).status, 0);
  assert.notEqual(
    run('release-prepare.mjs', repo, { ...env, RELEASE_TAG: 'v9.9.9', GITHUB_REF: 'refs/tags/v9.9.9' })
      .status,
    0,
  );
  git(repo, 'checkout', '-b', 'unreleased');
  writeFileSync(join(repo, 'unreleased'), 'not on default branch');
  git(repo, 'add', '.');
  git(repo, 'commit', '-m', 'unreleased');
  git(repo, 'tag', 'v0.0.2');
  git(repo, 'push', 'origin', 'refs/tags/v0.0.2');
  assert.notEqual(
    run('release-prepare.mjs', repo, {
      ...env,
      RELEASE_TAG: 'v0.0.2',
      GITHUB_REF: 'refs/tags/v0.0.2',
      SOURCE_SHA: git(repo, 'rev-parse', 'HEAD'),
    }).status,
    0,
  );
});

test('version synchronization workflow commits and atomically pushes synchronized versions and tag', (t) => {
  const { root, repo, remote, sha } = fixture(t);
  mkdirSync(join(repo, 'scripts'));
  for (const file of ['release-version.mjs', 'set-release-version.mjs']) {
    copyFileSync(resolve(import.meta.dirname, file), join(repo, 'scripts', file));
  }
  const workflow = readFileSync(
    resolve(import.meta.dirname, '../.github/workflows/release-prepare.yml'),
    'utf8',
  );
  const block = workflow.match(
    /        id: version\r?\n[\s\S]*?        run: \|\r?\n((?:          [^\r\n]*\r?\n)+)/,
  )?.[1];
  assert.ok(block, 'Workflow must expose the synchronization script');
  const script = join(root, 'sync.sh');
  writeFileSync(script, block.replace(/^          /gm, '').replaceAll('\r\n', '\n'));
  const bash =
    process.platform === 'win32' ? resolve(git(repo, '--exec-path'), '../../../bin/bash.exe') : 'bash';
  const output = join(root, 'output');
  const result = spawnSync(bash, [script], {
    cwd: repo,
    encoding: 'utf8',
    env: {
      ...process.env,
      GITHUB_REF: 'refs/heads/master',
      DEFAULT_BRANCH: 'master',
      BUMP: 'minor',
      GITHUB_OUTPUT: output,
    },
    windowsHide: true,
  });
  assert.equal(result.status, 0, result.stderr);
  const source = git(remote, 'rev-parse', 'master');
  assert.notEqual(source, sha);
  assert.equal(git(remote, 'rev-parse', 'v0.1.0^{}'), source);
  assert.equal(JSON.parse(git(remote, 'show', source + ':web/package.json')).version, '0.1.0');
  assert.match(git(remote, 'show', source + ':cmd/donate/version.go'), /var version = "0\.1\.0"/);
  assert.equal(readFileSync(output, 'utf8'), 'sha=' + source + '\ntag=v0.1.0\n');
  const wrongBranch = spawnSync(bash, [script], {
    cwd: repo,
    encoding: 'utf8',
    env: {
      ...process.env,
      GITHUB_REF: 'refs/heads/feature',
      DEFAULT_BRANCH: 'master',
      BUMP: 'patch',
      GITHUB_OUTPUT: output,
    },
    windowsHide: true,
  });
  assert.notEqual(wrongBranch.status, 0);
  assert.equal(git(remote, 'rev-parse', 'master'), source);
});

test('release preparation refuses a missing token before modifying the repository', (t) => {
  const { root, repo, sha } = fixture(t);
  const workflow = readFileSync(
    resolve(import.meta.dirname, '../.github/workflows/release-prepare.yml'),
    'utf8',
  );
  const block = workflow.match(/        run: \|\r?\n((?:          [^\r\n]*\r?\n)+)/)?.[1];
  assert.ok(block, 'Workflow must check the release token before checkout');
  const script = join(root, 'credentials.sh');
  writeFileSync(script, block.replace(/^          /gm, '').replaceAll('\r\n', '\n'));
  const bash =
    process.platform === 'win32' ? resolve(git(repo, '--exec-path'), '../../../bin/bash.exe') : 'bash';
  const missing = spawnSync(bash, [script], {
    cwd: repo,
    encoding: 'utf8',
    env: { ...process.env, RELEASE_TOKEN: '' },
    windowsHide: true,
  });
  assert.notEqual(missing.status, 0);
  assert.match(missing.stdout + missing.stderr, /RELEASE_TOKEN/);
  assert.equal(git(repo, 'rev-parse', 'HEAD'), sha);
  const configured = spawnSync(bash, [script], {
    cwd: repo,
    encoding: 'utf8',
    env: { ...process.env, RELEASE_TOKEN: 'local-test-placeholder' },
    windowsHide: true,
  });
  assert.equal(configured.status, 0, configured.stderr);
});

test('publish refuses a moved remote tag before invoking the release API', (t) => {
  const { repo, sha } = fixture(t);
  git(repo, 'tag', 'v0.0.1');
  git(repo, 'push', 'origin', 'refs/tags/v0.0.1');
  writeFileSync(join(repo, 'later'), 'another source');
  git(repo, 'add', '.');
  git(repo, 'commit', '-m', 'later');
  git(repo, 'tag', '-f', 'v0.0.1');
  git(repo, 'push', '--force', 'origin', 'refs/tags/v0.0.1');
  git(repo, 'checkout', '--detach', sha);
  const result = run('release-publish.mjs', repo, {
    SOURCE_SHA: sha,
    RELEASE_VERSION: '0.0.1',
    GITHUB_REPOSITORY: 'example/donate',
  });
  assert.notEqual(result.status, 0);
  assert.match(result.stderr, /Remote release tag changed after verification/);
});

test('release build produces only Linux amd64 and SHA256SUMS.txt with a matching checksum', (t) => {
  const { repo } = fixture(t);
  mkdirSync(join(repo, 'scripts'));
  for (const file of ['build.mjs', 'process.mjs', 'version.mjs']) {
    copyFileSync(resolve(import.meta.dirname, file), join(repo, 'scripts', file));
  }
  writeFileSync(
    join(repo, 'web/package.json'),
    JSON.stringify({ name: '@donate/web', version: '0.0.1', scripts: { build: 'node --version' } }) + '\n',
  );
  writeFileSync(join(repo, 'go.mod'), 'module example.invalid/donate\n');
  writeFileSync(
    join(repo, 'cmd/donate/main.go'),
    'package main\n\nvar commit, tag, builtAt string\n\nfunc main() {}\n',
  );
  git(repo, 'add', '.');
  git(repo, 'commit', '-m', 'build fixture');
  git(repo, 'tag', 'v0.0.1');
  const result = run('build.mjs', repo, {}, ['--release', '--version=0.0.1']);
  assert.equal(result.status, 0, result.stdout + result.stderr);
  assert.deepEqual(readdirSync(join(repo, 'dist')).sort(), ['SHA256SUMS.txt', 'donate-0.0.1-linux-amd64']);
  const binary = readFileSync(join(repo, 'dist/donate-0.0.1-linux-amd64'));
  assert.equal(binary.subarray(0, 4).toString('hex'), '7f454c46');
  assert.equal(binary.readUInt16LE(18), 62, 'Binary must target x86-64');
  assert.equal(
    readFileSync(join(repo, 'dist/SHA256SUMS.txt'), 'utf8'),
    createHash('sha256').update(binary).digest('hex') + '  donate-0.0.1-linux-amd64\n',
  );
});
