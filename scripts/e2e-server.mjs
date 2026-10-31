import {spawn} from 'node:child_process';
import {mkdtempSync,existsSync,readFileSync} from 'node:fs';
import {tmpdir} from 'node:os';
import {join,resolve} from 'node:path';
import {run} from './process.mjs';
const suffix=process.platform==='win32'?'.exe':'';const root=resolve(import.meta.dirname,'..');process.chdir(root);const appURL='http://127.0.0.1:18080';const dir=mkdtempSync(join(tmpdir(),'donate-e2e-'));
run('go',['build','-tags=e2e','-o',`dist/fixture${suffix}`,'./internal/e2efixture']);
const fixture=spawn(resolve(`dist/fixture${suffix}`),[],{windowsHide:true,stdio:'inherit',env:{...process.env,FIXTURE_DIR:dir,FIXTURE_APP_URL:appURL}});let app;const shutdown=()=>{app?.kill();fixture.kill()};process.on('SIGINT',shutdown);process.on('SIGTERM',()=>{shutdown();setTimeout(()=>process.exit(),500)});process.on('exit',shutdown);
const path=join(dir,'environment.json');const deadline=Date.now()+20000;while(!existsSync(path)){if(Date.now()>deadline||fixture.exitCode!==null)throw new Error('Fixture startup failed');await new Promise(r=>setTimeout(r,100))};const env=JSON.parse(readFileSync(path,'utf8'));app=spawn(resolve(`dist/donate${suffix}`),[],{cwd:dir,windowsHide:true,stdio:'inherit',env:{...process.env,...env,LISTEN_ADDR:'127.0.0.1:18080'}});app.on('exit',code=>{fixture.kill();process.exit(code??1)});
