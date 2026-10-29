import { execFileSync } from 'node:child_process';
const git = (...args) => execFileSync('git',args,{encoding:'utf8'}).trim();
const sha=process.env.SOURCE_SHA,version=process.env.RELEASE_VERSION;
if (!/^[0-9a-f]{40}$/.test(sha ?? '') || !/^\d+\.\d+\.\d+$/.test(version ?? '')) throw new Error('Invalid release metadata');
if (git('rev-parse','HEAD') !== sha) throw new Error('Release source SHA differs from the checked source');
git('tag',`v${version}`,sha);
