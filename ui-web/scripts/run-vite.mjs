import { spawnSync } from 'node:child_process';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const uiDir = path.dirname(path.dirname(fileURLToPath(import.meta.url)));
const viteCLI = path.join(uiDir, 'node_modules', 'vite', 'bin', 'vite.js');

const result = spawnSync(process.execPath, [viteCLI, ...process.argv.slice(2)], {
  cwd: uiDir,
  env: { ...process.env },
  stdio: 'inherit',
});
if (result.error) throw result.error;
if (result.signal) {
  process.kill(process.pid, result.signal);
} else {
  process.exit(result.status ?? 1);
}
