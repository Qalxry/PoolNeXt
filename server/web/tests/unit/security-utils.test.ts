import { describe, expect, it } from 'vitest';

import { getSafeInternalRedirect } from '@/lib/security/redirect';
import {
  getSafeExternalHttpUrl,
  renderSafeMarkdown,
} from '@/lib/security/safe-markdown';

describe('internal redirect validation', () => {
  it('preserves local paths with query strings and fragments', () => {
    expect(getSafeInternalRedirect('/nodes?status=active#selected')).toBe(
      '/nodes?status=active#selected',
    );
  });

  it.each([
    'https://evil.example/path',
    '//evil.example/path',
    '/\\evil.example/path',
    '/%5cevil.example/path',
    '/%2f%2fevil.example/path',
    '/%0d%0aLocation:%20https://evil.example/path',
    '/%zz',
    'javascript:alert(1)',
    '   //evil.example',
  ])('rejects external or ambiguous redirect %s', (redirect) => {
    expect(getSafeInternalRedirect(redirect)).toBe('/');
  });
});

describe('external URL validation', () => {
  it('accepts HTTP(S) links and rejects executable or relative URLs', () => {
    expect(getSafeExternalHttpUrl('https://example.com/release')).toBe(
      'https://example.com/release',
    );
    expect(getSafeExternalHttpUrl('javascript:alert(1)')).toBeNull();
    expect(getSafeExternalHttpUrl('/relative-release')).toBeNull();
  });
});

describe('safe Markdown rendering', () => {
  it('removes executable HTML, event handlers, images, and unsafe URLs', () => {
    const html = renderSafeMarkdown(`
# Release
<script>window.__owned = true</script>
<img src="https://tracker.example/pixel" onerror="alert(1)">
<a href="javascript:alert(1)" onclick="alert(2)">unsafe</a>
`);

    expect(html).toContain('<h1>Release</h1>');
    expect(html).not.toContain('<script');
    expect(html).not.toContain('<img');
    expect(html).not.toContain('javascript:');
    expect(html).not.toContain('onclick');
  });

  it('keeps ordinary links while enforcing safe link attributes', () => {
    const html = renderSafeMarkdown('[notes](https://example.com/release)');

    expect(html).toContain('href="https://example.com/release"');
    expect(html).toContain('target="_blank"');
    expect(html).toContain('rel="noopener noreferrer nofollow"');
  });
});
