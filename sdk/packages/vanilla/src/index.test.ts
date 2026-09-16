import { afterEach, describe, expect, it, vi } from 'vitest';

import { VanillaServifySDK } from './index';

function jsonResponse(payload: unknown): Response {
  return {
    ok: true,
    status: 200,
    json: async () => payload,
  } as unknown as Response;
}

describe('VanillaServifySDK suggested questions passthrough', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('getInitialQuestions hits the public route and returns data', async () => {
    const fetchMock = vi.fn(async () =>
      jsonResponse({
        success: true,
        data: {
          questions: [{ question: '如何重置密码', source: 'knowledge_doc', score: 1 }],
          meta: { strategy: 'public_knowledge_recency' },
        },
      }),
    );
    vi.stubGlobal('fetch', fetchMock);
    const sdk = new VanillaServifySDK({ apiUrl: 'http://localhost:8080' });

    const result = await sdk.getInitialQuestions(5);

    expect(result.questions[0].question).toBe('如何重置密码');
    expect(fetchMock).toHaveBeenCalledTimes(1);
    const url = String((fetchMock.mock.calls[0] as unknown[])[0]);
    expect(url).toContain('/public/suggestions/initial?limit=5');
  });

  it('getNextQuestions forwards query options to the public route', async () => {
    const fetchMock = vi.fn(async () =>
      jsonResponse({
        success: true,
        data: {
          query: '密码',
          questions: [{ question: '如何重置密码', source: 'knowledge_doc', score: 1 }],
        },
      }),
    );
    vi.stubGlobal('fetch', fetchMock);
    const sdk = new VanillaServifySDK({ apiUrl: 'http://localhost:8080' });

    const result = await sdk.getNextQuestions('密码', { sessionId: 's-1', limit: 3 });

    expect(result.query).toBe('密码');
    const parsed = new URL(String((fetchMock.mock.calls[0] as unknown[])[0]));
    expect(parsed.pathname).toBe('/public/suggestions/next');
    expect(parsed.searchParams.get('query')).toBe('密码');
    expect(parsed.searchParams.get('session_id')).toBe('s-1');
    expect(parsed.searchParams.get('limit')).toBe('3');
  });
});
