import { marked } from 'marked';
import sanitizeHtml from 'sanitize-html';

const allowedTags = [
  'h1',
  'h2',
  'h3',
  'h4',
  'h5',
  'h6',
  'p',
  'br',
  'blockquote',
  'ul',
  'ol',
  'li',
  'strong',
  'em',
  'del',
  'code',
  'pre',
  'hr',
  'table',
  'thead',
  'tbody',
  'tr',
  'th',
  'td',
  'a',
];

export function getSafeExternalHttpUrl(value: string | null | undefined) {
  if (!value) {
    return null;
  }
  try {
    const parsed = new URL(value);
    if (parsed.protocol !== 'https:' && parsed.protocol !== 'http:') {
      return null;
    }
    return parsed.toString();
  } catch {
    return null;
  }
}

export function renderSafeMarkdown(markdown: string) {
  const rendered = marked.parse(markdown, {
    async: false,
    gfm: true,
    breaks: false,
  }) as string;

  return sanitizeHtml(rendered, {
    allowedTags,
    allowedAttributes: {
      a: ['href', 'title', 'target', 'rel'],
    },
    allowedSchemes: ['http', 'https', 'mailto'],
    allowProtocolRelative: false,
    disallowedTagsMode: 'discard',
    transformTags: {
      a: (_tagName, attributes) => ({
        tagName: 'a',
        attribs: {
          ...attributes,
          target: '_blank',
          rel: 'noopener noreferrer nofollow',
        },
      }),
    },
  });
}
