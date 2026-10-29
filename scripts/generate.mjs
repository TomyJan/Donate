import {mkdirSync} from 'node:fs';
import {run,pnpm} from './process.mjs';
run('go',['run','./cmd/contractgen']);mkdirSync('web/src/generated',{recursive:true});pnpm(['exec','openapi-typescript','api/openapi.json','-o','web/src/generated/api.ts']);run('go',['run','github.com/sqlc-dev/sqlc/cmd/sqlc@v1.31.1','generate']);
