import { spawnSync } from 'node:child_process';
export function run(command, args, options = {}) {
  const result = spawnSync(command, args, { stdio: 'inherit', windowsHide: true, ...options });
  if (result.error) throw result.error;
  if (result.status !== 0) throw new Error(`${command} exited with ${result.status}`);
}
export function pnpm(args) {
  const executable = process.env.npm_execpath;
  if (!executable) throw new Error('Run this command with pnpm');
  if (/\.[cm]?js$/.test(executable)) run(process.execPath, [executable, ...args]);
  else run(executable, args);
}
