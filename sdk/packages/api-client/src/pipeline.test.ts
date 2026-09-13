import { afterEach, describe, expect, it, vi } from 'vitest';

import {
  ApiClientHttpError,
  FetchRequestPipeline,
  type RequestFetch,
} from './pipeline';
import type { ApiKeyAuthProvider } from './contracts/auth';

interface CapturedCall {
  url: string;
  init: RequestInit;
}

function jsonResponse(status: number, body: unknown = { ok: status < 400 }): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'content-type': 'application/json' },
  });
}

function textResponse(status: number, body: string): Response {
  return new Response(body, {
    status,
    headers: { 'content-type': 'text/plain' },
  });
}

function captureFetch(calls: CapturedCall[], responder: () => Response): RequestFetch {
  return ((url: string | URL | Request, init?: RequestInit) => {
    calls.push({ url: typeof url === 'string' ? url : url.toString(), init: init ?? {} });
    return Promise.resolve(responder());
  }) as unknown as RequestFetch;
}

function mockFetch(responder: (...args: unknown[]) => Promise<Response>): RequestFetch {
  return responder as unknown as RequestFetch;
}

function neverResolvingFetchWithAbort(): RequestFetch {
  return ((_url: string | URL | Request, init?: RequestInit) =>
    new Promise<Response>((_resolve, reject) => {
      init?.signal?.addEventListener('abort', () => {
        const error = new Error('The operation was aborted');
        error.name = 'AbortError';
        reject(error);
      });
    })) as unknown as RequestFetch;
}

describe('FetchRequestPipeline', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
    vi.useRealTimers();
  });

  it('joins baseUrl, path and query into the request URL', async () => {
    const calls: CapturedCall[] = [];
    const pipeline = new FetchRequestPipeline({
      baseUrl: 'https://api.example.com/',
      fetch: captureFetch(calls, () => jsonResponse(200)),
    });

    await pipeline.request({ path: '/tickets/1', query: { page: '2' } });

    expect(calls[0].url).toBe('https://api.example.com/tickets/1?page=2');
    expect(calls[0].init.method).toBe('GET');
  });

  it('uses an absolute path as-is even when baseUrl is configured', async () => {
    const calls: CapturedCall[] = [];
    const pipeline = new FetchRequestPipeline({
      baseUrl: 'https://api.example.com',
      fetch: captureFetch(calls, () => jsonResponse(200)),
    });

    await pipeline.request({ path: 'https://other.example.com/internal/x' });

    expect(calls[0].url).toBe('https://other.example.com/internal/x');
  });

  it('throws a clear error when neither baseUrl nor an absolute path is available', async () => {
    const pipeline = new FetchRequestPipeline({
      fetch: captureFetch([], () => jsonResponse(200)),
    });

    await expect(pipeline.request({ path: 'relative/path' })).rejects.toThrow(
      'options.baseUrl must be configured',
    );
  });

  it('serializes object bodies as JSON with a content-type and skips bodies on GET', async () => {
    const calls: CapturedCall[] = [];
    const pipeline = new FetchRequestPipeline({
      baseUrl: 'https://api.example.com',
      fetch: captureFetch(calls, () => jsonResponse(200)),
    });

    await pipeline.request({ method: 'POST', path: '/tickets', body: { title: 'a' } });
    await pipeline.request({ method: 'GET', path: '/tickets', body: { ignored: true } });

    expect(calls[0].init.body).toBe('{"title":"a"}');
    expect(
      (calls[0].init.headers as Record<string, string>)['content-type'],
    ).toBe('application/json');
    expect(calls[1].init.body).toBeUndefined();
  });

  it('parses JSON bodies and falls back to raw text for non-JSON responses', async () => {
    const jsonPipeline = new FetchRequestPipeline({
      baseUrl: 'https://api.example.com',
      fetch: mockFetch(async () => jsonResponse(200, { value: 42 })),
    });
    const textPipeline = new FetchRequestPipeline({
      baseUrl: 'https://api.example.com',
      fetch: mockFetch(async () => textResponse(200, 'plain-body')),
    });
    const emptyPipeline = new FetchRequestPipeline({
      baseUrl: 'https://api.example.com',
      fetch: mockFetch(async () => new Response(null, { status: 204 })),
    });

    await expect(jsonPipeline.request('/json')).resolves.toMatchObject({
      status: 200,
      body: { value: 42 },
    });
    await expect(textPipeline.request('/text')).resolves.toMatchObject({
      status: 200,
      body: 'plain-body',
    });
    await expect(emptyPipeline.request('/empty')).resolves.toMatchObject({
      status: 204,
      body: undefined,
    });
  });

  it('retries retryable statuses then succeeds, and reports ApiClientHttpError when exhausted', async () => {
    const fetchMock = vi
      .fn<() => Promise<Response>>()
      .mockResolvedValueOnce(jsonResponse(503))
      .mockResolvedValueOnce(jsonResponse(503))
      .mockResolvedValueOnce(jsonResponse(200));
    const pipeline = new FetchRequestPipeline({
      baseUrl: 'https://api.example.com',
      fetch: mockFetch(fetchMock),
      retry: { maxAttempts: 3, baseDelayMs: 1 },
    });

    await expect(pipeline.request('/flaky')).resolves.toMatchObject({ status: 200 });
    expect(fetchMock).toHaveBeenCalledTimes(3);

    const exhausted = new FetchRequestPipeline({
      baseUrl: 'https://api.example.com',
      fetch: mockFetch(vi.fn(async () => jsonResponse(429))),
      retry: { maxAttempts: 3, baseDelayMs: 1 },
    });

    await expect(exhausted.request('/limited')).rejects.toBeInstanceOf(ApiClientHttpError);
    await expect(exhausted.request('/limited')).rejects.toMatchObject({ status: 429 });
  });

  it('does not retry non-retryable statuses', async () => {
    const fetchMock = vi.fn(async () => jsonResponse(400, { error: 'bad_request' }));
    const pipeline = new FetchRequestPipeline({
      baseUrl: 'https://api.example.com',
      fetch: mockFetch(fetchMock),
      retry: { maxAttempts: 3, baseDelayMs: 1 },
    });

    await expect(pipeline.request('/bad')).rejects.toMatchObject({
      name: 'ApiClientHttpError',
      status: 400,
    });
    expect(fetchMock).toHaveBeenCalledTimes(1);
  });

  it('retries network errors and surfaces ApiClientNetworkError when exhausted', async () => {
    const fetchMock = vi
      .fn<() => Promise<Response>>()
      .mockRejectedValueOnce(new TypeError('network down'))
      .mockRejectedValueOnce(new TypeError('network down'))
      .mockResolvedValueOnce(jsonResponse(200));
    const pipeline = new FetchRequestPipeline({
      baseUrl: 'https://api.example.com',
      fetch: mockFetch(fetchMock),
      retry: { maxAttempts: 3, baseDelayMs: 1 },
    });

    await expect(pipeline.request('/network')).resolves.toMatchObject({ status: 200 });
    expect(fetchMock).toHaveBeenCalledTimes(3);

    const alwaysFailing = new FetchRequestPipeline({
      baseUrl: 'https://api.example.com',
      fetch: mockFetch(vi.fn(async () => {
        throw new TypeError('network down');
      })),
      retry: { maxAttempts: 2, baseDelayMs: 1 },
    });

    await expect(alwaysFailing.request('/network')).rejects.toMatchObject({
      name: 'ApiClientNetworkError',
    });
  });

  it('backs off exponentially between retry attempts', async () => {
    vi.useFakeTimers();
    const fetchMock = vi
      .fn<() => Promise<Response>>()
      .mockResolvedValueOnce(jsonResponse(503))
      .mockResolvedValueOnce(jsonResponse(503))
      .mockResolvedValueOnce(jsonResponse(200));
    const pipeline = new FetchRequestPipeline({
      baseUrl: 'https://api.example.com',
      fetch: mockFetch(fetchMock),
      retry: { maxAttempts: 3, baseDelayMs: 250, maxDelayMs: 1000 },
    });

    const pending = pipeline.request('/backoff');
    await vi.advanceTimersByTimeAsync(0);
    expect(fetchMock).toHaveBeenCalledTimes(1);

    await vi.advanceTimersByTimeAsync(249);
    expect(fetchMock).toHaveBeenCalledTimes(1);
    await vi.advanceTimersByTimeAsync(1);
    expect(fetchMock).toHaveBeenCalledTimes(2);

    await vi.advanceTimersByTimeAsync(500);
    expect(fetchMock).toHaveBeenCalledTimes(3);

    await expect(pending).resolves.toMatchObject({ status: 200 });
  });

  it('aborts a hanging attempt after timeoutMs and reports it as a network error', async () => {
    const pipeline = new FetchRequestPipeline({
      baseUrl: 'https://api.example.com',
      fetch: neverResolvingFetchWithAbort(),
      timeoutMs: 20,
      retry: { maxAttempts: 1 },
    });

    await expect(pipeline.request('/hangs')).rejects.toThrow('timed out after 20ms');
  });

  it('propagates caller cancellation without retrying', async () => {
    const fetchMock = vi.fn(async () => {
      const error = new Error('The operation was aborted');
      error.name = 'AbortError';
      throw error;
    });
    const controller = new AbortController();
    controller.abort();
    const pipeline = new FetchRequestPipeline({
      baseUrl: 'https://api.example.com',
      fetch: mockFetch(fetchMock),
      retry: { maxAttempts: 3, baseDelayMs: 1 },
    });

    await expect(
      pipeline.request({ path: '/cancelled', signal: controller.signal }),
    ).rejects.toMatchObject({ name: 'AbortError' });
    expect(fetchMock).toHaveBeenCalledTimes(1);
  });

  it('runs onRequest in order and onResponse once on the final successful response', async () => {
    const calls: string[] = [];
    const callsForCapture: CapturedCall[] = [];
    const pipeline = new FetchRequestPipeline({
      baseUrl: 'https://api.example.com',
      fetch: captureFetch(callsForCapture, () => jsonResponse(200)),
      middleware: [
        {
          name: 'm1',
          onRequest: (context) => {
            calls.push('m1');
            return { ...context, headers: { ...context.headers, 'x-m1': '1' } };
          },
          onResponse: (_context, response) => {
            calls.push('m1-res');
            return response;
          },
        },
        {
          name: 'm2',
          onRequest: (context) => {
            calls.push('m2');
            return context;
          },
        },
      ],
    });

    await pipeline.request('/with-middleware');

    expect(calls).toEqual(['m1', 'm2', 'm1-res']);
    expect(
      (callsForCapture[0].init.headers as Record<string, string>)['x-m1'],
    ).toBe('1');
  });

  it('injects auth provider headers and lets explicit request headers win', async () => {
    const calls: CapturedCall[] = [];
    const pipeline = new FetchRequestPipeline({
      baseUrl: 'https://api.example.com',
      fetch: captureFetch(calls, () => jsonResponse(200)),
      authProvider: {
        getHeaders: async () => ({
          authorization: 'Bearer auto-token',
          headers: { 'x-tenant': 'acme' },
        }),
      },
    });

    await pipeline.request({
      path: '/secure',
      headers: { authorization: 'Bearer manual-token' },
    });

    const headers = calls[0].init.headers as Record<string, string>;
    expect(headers['authorization']).toBe('Bearer manual-token');
    expect(headers['x-tenant']).toBe('acme');
  });

  it('places api key auth under the provider keyName header', async () => {
    const calls: CapturedCall[] = [];
    const apiKeyProvider: ApiKeyAuthProvider = {
      kind: 'api_key',
      keyName: 'x-servify-admin-key',
      getHeaders: async () => ({ apiKey: 'k-123' }),
    };
    const pipeline = new FetchRequestPipeline({
      baseUrl: 'https://api.example.com',
      fetch: captureFetch(calls, () => jsonResponse(200)),
      authProvider: apiKeyProvider,
    });

    await pipeline.request('/admin');

    expect(
      (calls[0].init.headers as Record<string, string>)['x-servify-admin-key'],
    ).toBe('k-123');
  });

  it('attaches an idempotency key from the provider, preferring an explicit key', async () => {
    const calls: CapturedCall[] = [];
    const pipeline = new FetchRequestPipeline({
      baseUrl: 'https://api.example.com',
      fetch: captureFetch(calls, () => jsonResponse(200)),
      idempotencyKeyProvider: {
        generate: ({ path }) => `auto:${path}`,
      },
    });

    await pipeline.request({ method: 'POST', path: '/tickets' });
    await pipeline.request({ method: 'POST', path: '/tickets', idempotencyKey: 'manual-key' });

    expect(
      (calls[0].init.headers as Record<string, string>)['idempotency-key'],
    ).toBe('auto:/tickets');
    expect(
      (calls[1].init.headers as Record<string, string>)['idempotency-key'],
    ).toBe('manual-key');
  });

  it('throws a clear error when no fetch implementation is available', () => {
    vi.stubGlobal('fetch', undefined);

    expect(() => new FetchRequestPipeline()).toThrow('provide options.fetch');
  });
});
