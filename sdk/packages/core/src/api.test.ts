import { afterEach, describe, expect, it, vi } from 'vitest';

import { ApiClient } from './api';

describe('ApiClient P0-6 漂移收口', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('getCustomerSessions 返回 unsupported 错误且不发请求', async () => {
    const fetchMock = vi.fn();
    vi.stubGlobal('fetch', fetchMock);

    const client = new ApiClient({ baseUrl: 'https://api.example.com' });
    const result = await client.getCustomerSessions(42);

    expect(result).toEqual({
      success: false,
      error: 'Customer session listing is not exposed by the current server contract.',
    });
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it('真实路由的会话读取仍走 GET /api/omni/sessions/:id', async () => {
    const fetchMock = vi.fn(async (_url: string | URL, _init?: RequestInit) => ({
      ok: true,
      status: 200,
      statusText: 'OK',
      json: async () => ({ success: true, data: { id: 7, visitor_id: 'v1' } }),
    }));
    vi.stubGlobal('fetch', fetchMock);

    const client = new ApiClient({ baseUrl: 'https://api.example.com' });
    const result = await client.getSession(7);

    expect(result.success).toBe(true);
    expect(fetchMock).toHaveBeenCalledTimes(1);
    const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit];
    expect(url).toBe('https://api.example.com/api/omni/sessions/7');
    expect(init.method).toBe('GET');
  });
});
