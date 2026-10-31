import { createHash } from 'node:crypto';
import { readdirSync, readFileSync } from 'node:fs';
import { join } from 'node:path';
import { pnpm } from './process.mjs';

function snapshot() {
  const paths = ['.env.example', 'api/openapi.json', 'web/src/generated/api.ts'];
  for (const name of readdirSync('internal/storage/dbgen')) paths.push(join('internal/storage/dbgen', name));
  return paths.sort().map((path) => [path, createHash('sha256').update(readFileSync(path)).digest('hex')]);
}
const before = JSON.stringify(snapshot());
pnpm(['generate']);
if (JSON.stringify(snapshot()) !== before) throw new Error('Generated contracts or queries drifted. Run pnpm generate and commit the results.');
console.log('Generated contracts and queries are current.');
