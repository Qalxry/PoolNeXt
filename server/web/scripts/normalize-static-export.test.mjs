import assert from 'node:assert/strict';
import {
  mkdtempSync,
  mkdirSync,
  readFileSync,
  rmSync,
  writeFileSync,
} from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import test from 'node:test';

import {
  deterministicRequestId,
  normalizeStaticExportRequestIds,
} from './normalize-static-export.mjs';

const firstRandomId = '1Yp3ppeFdEZ4to7ApTXlm';
const secondRandomId = 'eRhXbFOsms5_r7_hkMO_X';

test('normalizes Next.js request IDs per route without changing public text files', () => {
  const directory = mkdtempSync(join(tmpdir(), 'poolnext-static-export-'));
  const nestedDirectory = join(directory, 'oauth');
  mkdirSync(nestedDirectory);

  const htmlPath = join(nestedDirectory, 'github.html');
  const textPath = join(nestedDirectory, 'github.txt');
  const robotsPath = join(directory, 'robots.txt');
  const staticHtmlPath = join(directory, 'standalone.html');
  const htmlPrefix = '<script>self.__next_f.push([\\"$\\",\\"$1\\",\\"';
  const htmlSuffix = '\\",{\\"children\\":[[\\"$\\",\\"$Le\\"]]</script>';
  const textPrefix = '["$","$1","';
  const textSuffix = '",{"children":[["$","$Le"]]';

  writeFileSync(htmlPath, `${htmlPrefix}${firstRandomId}${htmlSuffix}`);
  writeFileSync(textPath, `${textPrefix}${secondRandomId}${textSuffix}`);
  writeFileSync(robotsPath, 'User-agent: *\nDisallow:\n');
  writeFileSync(staticHtmlPath, '<!doctype html><title>Standalone</title>');

  try {
    assert.equal(normalizeStaticExportRequestIds(directory), 2);

    const expectedId = deterministicRequestId('oauth/github');
    assert.match(readFileSync(htmlPath, 'utf8'), new RegExp(expectedId));
    assert.match(readFileSync(textPath, 'utf8'), new RegExp(expectedId));
    assert.equal(
      readFileSync(robotsPath, 'utf8'),
      'User-agent: *\nDisallow:\n',
    );
    assert.equal(
      readFileSync(staticHtmlPath, 'utf8'),
      '<!doctype html><title>Standalone</title>',
    );
  } finally {
    rmSync(directory, { recursive: true, force: true });
  }
});
