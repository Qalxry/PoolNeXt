import { rmSync } from 'node:fs';
import { resolve } from 'node:path';

const root = process.cwd();

for (const output of ['.next', 'out', 'build', 'tsconfig.tsbuildinfo']) {
  rmSync(resolve(root, output), { recursive: true, force: true });
}
