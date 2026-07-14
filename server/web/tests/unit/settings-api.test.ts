import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { bindEmail, bindWeChat } from '@/features/settings/api/settings';

describe('settings identity binding API', () => {
  const fetchMock = vi.fn();

  beforeEach(() => {
    fetchMock.mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({ success: true, data: null }),
    });
    vi.stubGlobal('fetch', fetchMock);
  });

  afterEach(() => {
    fetchMock.mockReset();
    vi.unstubAllGlobals();
  });

  it('sends email binding credentials in a POST JSON body', async () => {
    await bindEmail('alice@example.com', '123456');

    const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit];
    expect(url).toBe('/api/oauth/email/bind');
    expect(url).not.toContain('alice@example.com');
    expect(init.method).toBe('POST');
    expect(JSON.parse(init.body as string)).toEqual({
      email: 'alice@example.com',
      code: '123456',
    });
  });

  it('sends WeChat binding credentials in a POST JSON body', async () => {
    await bindWeChat('wechat-code');

    const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit];
    expect(url).toBe('/api/oauth/wechat/bind');
    expect(url).not.toContain('wechat-code');
    expect(init.method).toBe('POST');
    expect(JSON.parse(init.body as string)).toEqual({ code: 'wechat-code' });
  });
});
