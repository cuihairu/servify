import { afterEach, describe, expect, it, vi } from 'vitest';

import { PushTokenRegistrationError, RestPushTokenRegistrar } from '../index';

describe('RestPushTokenRegistrar', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('posts the registration payload in the snake_case wire format', async () => {
    const fetchMock = vi.fn(
      async (_url: string, _init?: RequestInit) => new Response(null, { status: 204 }),
    );
    const registrar = new RestPushTokenRegistrar({
      endpoint: 'https://api.example.com/api/v1/push/tokens',
      fetch: fetchMock as unknown as typeof fetch,
      headers: { authorization: 'Bearer tok' },
    });

    await registrar.register({
      token: 'apns-token-1',
      platform: 'ios',
      deviceId: 'device-1',
      environment: 'sandbox',
    });

    const [url, init] = fetchMock.mock.calls[0];
    if (!init) throw new Error('expected fetch init');
    expect(url).toBe('https://api.example.com/api/v1/push/tokens');
    expect(init.method).toBe('POST');
    expect((init.headers as Record<string, string>)['content-type']).toBe('application/json');
    expect((init.headers as Record<string, string>)['authorization']).toBe('Bearer tok');
    expect(JSON.parse(init.body as string)).toEqual({
      token: 'apns-token-1',
      platform: 'ios',
      device_id: 'device-1',
      environment: 'sandbox',
    });
  });

  it('sends unregister as DELETE with a device_id query parameter', async () => {
    const fetchMock = vi.fn(
      async (_url: string, _init?: RequestInit) => new Response(null, { status: 204 }),
    );
    const registrar = new RestPushTokenRegistrar({
      endpoint: 'https://api.example.com/api/v1/push/tokens',
      fetch: fetchMock as unknown as typeof fetch,
    });

    await registrar.unregister('device-42');

    const [url, init] = fetchMock.mock.calls[0];
    if (!init) throw new Error('expected fetch init');
    expect(init.method).toBe('DELETE');
    expect(url).toBe('https://api.example.com/api/v1/push/tokens?device_id=device-42');
    expect(init.body).toBeUndefined();
  });

  it('throws PushTokenRegistrationError with the status on non-2xx responses', async () => {
    const registrar = new RestPushTokenRegistrar({
      endpoint: 'https://api.example.com/push',
      fetch: (async () => new Response('{"error":"denied"}', { status: 403 })) as unknown as typeof fetch,
    });

    await expect(
      registrar.register({ token: 't', platform: 'android', deviceId: 'd' }),
    ).rejects.toBeInstanceOf(PushTokenRegistrationError);
    await expect(
      registrar.register({ token: 't', platform: 'android', deviceId: 'd' }),
    ).rejects.toMatchObject({ status: 403 });
  });

  it('wraps transport failures into PushTokenRegistrationError', async () => {
    const registrar = new RestPushTokenRegistrar({
      endpoint: 'https://api.example.com/push',
      fetch: (async () => {
        throw new TypeError('network down');
      }) as unknown as typeof fetch,
    });

    await expect(registrar.unregister('device-1')).rejects.toMatchObject({
      name: 'PushTokenRegistrationError',
      status: undefined,
    });
  });

  it('requires an endpoint and a fetch implementation', () => {
    expect(() => new RestPushTokenRegistrar({ endpoint: '' })).toThrow('options.endpoint');

    vi.stubGlobal('fetch', undefined);
    expect(
      () => new RestPushTokenRegistrar({ endpoint: 'https://api.example.com/push' }),
    ).toThrow('provide options.fetch');
  });
});
