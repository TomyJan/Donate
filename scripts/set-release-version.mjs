import { readFileSync, writeFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { pathToFileURL } from 'node:url';
import { versionPattern } from './release-version.mjs';

export function setReleaseVersion(root, version) {
  if (!versionPattern.test(version)) throw new Error('Version must be x.y.z without leading zeroes');
  const path = resolve(root, 'web/package.json');
  const pkg = JSON.parse(readFileSync(path, 'utf8'));
  pkg.version = version;
  writeFileSync(path, JSON.stringify(pkg, null, 2) + '\n');
  writeFileSync(resolve(root, 'cmd/donate/version.go'), 'package main\n\nvar version = "' + version + '"\n');
}
if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  setReleaseVersion(process.cwd(), process.argv[2]);
}
