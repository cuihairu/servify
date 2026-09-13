import type { PushTokenRegistrar, PushTokenRegistration } from '../contracts/push';

export type PushFetch = typeof fetch;

export interface RestPushTokenRegistrarOptions {
  /** push 注册 API 端点(绝对 URL);register 走 POST,unregister 走 DELETE。 */
  endpoint: string;
  /** 注入自定义 fetch;缺省探测 globalThis.fetch。 */
  fetch?: PushFetch;
  /** 附加请求头(如认证);content-type 由实现自带。 */
  headers?: Record<string, string>;
  /** 单次请求硬超时(毫秒),默认 10000。 */
  timeoutMs?: number;
}

export class PushTokenRegistrationError extends Error {
  readonly status?: number;

  constructor(message: string, status?: number) {
    super(message);
    this.name = 'PushTokenRegistrationError';
    this.status = status;
  }
}

const DEFAULT_TIMEOUT_MS = 10_000;

function createDefaultFetch(): PushFetch {
  const candidate = (globalThis as { fetch?: PushFetch }).fetch;
  if (typeof candidate !== 'function') {
    throw new Error(
      'fetch is not available in this environment; provide options.fetch to supply one.',
    );
  }
  return candidate.bind(globalThis);
}

/**
 * REST 形态的 PushTokenRegistrar:线格式与服务端 API 风格一致
 * (snake_case:token/platform/device_id/environment)。服务端 push
 * 端点尚未落地,端点与 fetch 全部注入,不预设具体 API 路径。
 */
export class RestPushTokenRegistrar implements PushTokenRegistrar {
  private readonly options: RestPushTokenRegistrarOptions;
  private readonly fetchImpl: PushFetch;
  private readonly timeoutMs: number;
  readonly endpoint: string;

  constructor(options: RestPushTokenRegistrarOptions) {
    if (!options.endpoint) {
      throw new Error('RestPushTokenRegistrar requires options.endpoint');
    }
    this.options = options;
    this.endpoint = options.endpoint;
    this.fetchImpl = options.fetch ?? createDefaultFetch();
    this.timeoutMs = options.timeoutMs ?? DEFAULT_TIMEOUT_MS;
  }

  async register(input: PushTokenRegistration): Promise<void> {
    await this.request({
      method: 'POST',
      url: this.endpoint,
      body: {
        token: input.token,
        platform: input.platform,
        device_id: input.deviceId,
        environment: input.environment,
      },
    });
  }

  async unregister(deviceId: string): Promise<void> {
    const url = new URL(this.endpoint);
    url.searchParams.set('device_id', deviceId);
    await this.request({ method: 'DELETE', url: url.toString(), body: undefined });
  }

  private async request(init: {
    method: 'POST' | 'DELETE';
    url: string;
    body: Record<string, unknown> | undefined;
  }): Promise<void> {
    const controller = new AbortController();
    const timeoutId = setTimeout(() => controller.abort(), this.timeoutMs);

    try {
      const response = await this.fetchImpl(init.url, {
        method: init.method,
        headers: {
          ...(init.body !== undefined ? { 'content-type': 'application/json' } : {}),
          ...this.options.headers,
        },
        body: init.body !== undefined ? JSON.stringify(init.body) : undefined,
        signal: controller.signal,
      });

      if (response.status < 200 || response.status >= 300) {
        throw new PushTokenRegistrationError(
          `push token ${init.method === 'POST' ? 'registration' : 'unregistration'} failed with status ${response.status}`,
          response.status,
        );
      }
    } catch (error) {
      if (error instanceof PushTokenRegistrationError) {
        throw error;
      }
      if (error instanceof Error && error.name === 'AbortError') {
        throw new PushTokenRegistrationError('push token request timed out');
      }
      throw new PushTokenRegistrationError(describeError(error));
    } finally {
      clearTimeout(timeoutId);
    }
  }
}

function describeError(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}
