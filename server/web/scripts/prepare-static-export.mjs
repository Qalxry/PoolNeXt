import { cpSync, existsSync, rmSync } from 'node:fs';
import { resolve } from 'node:path';
import { normalizeStaticExportRequestIds } from './normalize-static-export.mjs';

const root = process.cwd();
const outDir = resolve(root, 'out');
const buildDir = resolve(root, 'build');

if (!existsSync(resolve(outDir, 'index.html'))) {
  throw new Error('Missing Next.js export output: out/');
}

normalizeStaticExportRequestIds(outDir);

rmSync(buildDir, { recursive: true, force: true });
cpSync(outDir, buildDir, { recursive: true });
rmSync(outDir, { recursive: true, force: true });

if (!existsSync(resolve(buildDir, 'index.html'))) {
  throw new Error(
    'Missing prepared embedded WebUI entrypoint: build/index.html',
  );
}
