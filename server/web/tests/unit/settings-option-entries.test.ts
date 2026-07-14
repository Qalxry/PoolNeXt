import { describe, expect, it } from 'vitest';

import { includeNonEmptyOptionEntries } from '@/features/settings/lib/option-entries';

describe('includeNonEmptyOptionEntries', () => {
  it('omits blank optional secrets so existing values are preserved', () => {
    expect(
      includeNonEmptyOptionEntries(
        [['SMTPServer', 'smtp.example.com']],
        [
          ['SMTPToken', ''],
          ['GitHubClientSecret', '   '],
        ],
      ),
    ).toEqual([['SMTPServer', 'smtp.example.com']]);
  });

  it('trims and includes secrets that the user explicitly replaces', () => {
    expect(
      includeNonEmptyOptionEntries(
        [],
        [['ClashSecret', '  replacement-secret  ']],
      ),
    ).toEqual([['ClashSecret', 'replacement-secret']]);
  });
});
