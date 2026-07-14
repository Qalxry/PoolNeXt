import { createHash } from 'node:crypto';
import { readdirSync, readFileSync, writeFileSync } from 'node:fs';
import { extname, join, relative, sep } from 'node:path';

const htmlRequestIdPattern =
  /(\[\\"\$\\",\\"\$1\\",\\")[A-Za-z0-9_-]{21}(\\",\{\\"children\\":\[\[\\"\$\\",\\"\$L)/g;
const textRequestIdPattern =
  /(\["\$","\$1",")[A-Za-z0-9_-]{21}(",\{"children":\[\["\$","\$L)/g;

function walkFiles(directory) {
  return readdirSync(directory, { withFileTypes: true }).flatMap((entry) => {
    const path = join(directory, entry.name);
    return entry.isDirectory() ? walkFiles(path) : [path];
  });
}

export function deterministicRequestId(route) {
  return createHash('sha256')
    .update(`poolnext-static-route:${route}`)
    .digest('base64url')
    .slice(0, 21);
}

export function normalizeStaticExportRequestIds(exportDirectory) {
  let normalizedFiles = 0;

  for (const file of walkFiles(exportDirectory)) {
    const extension = extname(file);
    const pattern =
      extension === '.html' ? htmlRequestIdPattern : textRequestIdPattern;
    if (extension !== '.html' && extension !== '.txt') {
      continue;
    }

    const source = readFileSync(file, 'utf8');
    const relativePath = relative(exportDirectory, file).split(sep).join('/');
    const route = relativePath.slice(0, -extension.length);
    const requestId = deterministicRequestId(route);
    let matches = 0;
    const normalized = source.replace(pattern, (_match, prefix, suffix) => {
      matches += 1;
      return `${prefix}${requestId}${suffix}`;
    });

    const isNextRouteHtml =
      extension === '.html' && source.includes('self.__next_f.push');
    if (matches > 1 || (isNextRouteHtml && matches !== 1)) {
      throw new Error(
        `Expected one Next.js request ID in ${relativePath}, found ${matches}`,
      );
    }
    if (matches === 1) {
      writeFileSync(file, normalized);
      normalizedFiles += 1;
    }
  }

  if (normalizedFiles === 0) {
    throw new Error('No Next.js static request IDs were normalized');
  }

  return normalizedFiles;
}
