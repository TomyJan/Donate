import { execFileSync } from 'node:child_process';
import { readdirSync } from 'node:fs';
const run=(cmd,args)=>execFileSync(cmd,args,{encoding:'utf8',stdio:'inherit'});
const sha=process.env.SOURCE_SHA,version=process.env.RELEASE_VERSION;
if (!/^[0-9a-f]{40}$/.test(sha ?? '') || !/^\d+\.\d+\.\d+$/.test(version ?? '')) throw new Error('Invalid release metadata');
run('git',['tag',`v${version}`,sha]);
run('git',['push','origin',`refs/tags/v${version}`]);
const assets=readdirSync('release-assets').map(name=>`release-assets/${name}`);
run('gh',['release','create',`v${version}`,'--verify-tag','--target',sha,'--title',`V${version}`,'--generate-notes',...assets]);
