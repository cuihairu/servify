import type { ServerAuthContext, ServerAuthHeaders, ServerAuthProvider } from './contracts/auth';
import type {
  ApiRequestContext,
  ApiRequestMiddleware,
  ApiResponseContext,
  HttpMethod,
  IdempotencyKeyProvider,
} from './contracts/request-pipeline';
import {
  computeRetryDelay,
  normalizeRetryBackoffPolicy,
  shouldRetryRequest,
  type RetryBackoffPolicy,
} from './contracts/retry';

export type RequestFetch = typeof fetch;

export interface ApiClientRequestInput {
  method?: HttpMethod;
  path: string;
  headers?: Record<string, string>;
  query?: Record<string, string>;
  body?: unknown;
  idempotencyKey?: string;
  metadata?: Record<string, unknown>;
  /** 调用方取消信号：外部 abort 直接抛出，不进入重试。 */
  signal?: AbortSignal;
}

export interface FetchRequestPipelineOptions {
  /** 服务端 API 根地址；path 为绝对 URL 时忽略。 */
  baseUrl?: string;
  /** 注入自定义 fetch（SSR/测试/非 DOM 宿主）；缺省探测 globalThis.fetch。 */
  fetch?: RequestFetch;
  /** 单次尝试的硬超时（毫秒），超时按可重试网络错误处理。 */
  timeoutMs?: number;
  /** 重试策略；未提供的字段回落到 normalizeRetryBackoffPolicy 默认值。 */
  retry?: Partial<RetryBackoffPolicy>;
  authProvider?: ServerAuthProvider;
  /** 传给 authProvider.getHeaders 的固定上下文。 */
  authContext?: ServerAuthContext;
  idempotencyKeyProvider?: IdempotencyKeyProvider;
  /** 按数组顺序执行 onRequest；onResponse 只在最终成功响应上按序执行。 */
  middleware?: ApiRequestMiddleware[];
}

const DEFAULT_TIMEOUT_MS = 10_000;
const IDEMPOTENCY_KEY_HEADER = 'idempotency-key';
const DEFAULT_API_KEY_HEADER = 'x-api-key';

export class ApiClientHttpError extends Error {
  readonly status: number;
  readonly headers: Record<string, string>;
  readonly body?: unknown;

  constructor(status: number, headers: Record<string, string>, body?: unknown, message?: string) {
    super(message ?? `request failed with status ${status}`);
    this.name = 'ApiClientHttpError';
    this.status = status;
    this.headers = headers;
    this.body = body;
  }
}

export class ApiClientNetworkError extends Error {
  readonly cause?: unknown;

  constructor(message: string, cause?: unknown) {
    super(message);
    this.name = 'ApiClientNetworkError';
    this.cause = cause;
  }
}

function createDefaultFetch(): RequestFetch {
  const candidate = (globalThis as { fetch?: RequestFetch }).fetch;
  if (typeof candidate !== 'function') {
    throw new Error(
      'fetch is not available in this environment; provide options.fetch to supply one.',
    );
  }
  return candidate.bind(globalThis);
}

function headersToRecord(headers: Headers): Record<string, string> {
  const result: Record<string, string> = {};
  headers.forEach((value, key) => {
    result[key.toLowerCase()] = value;
  });
  return result;
}

function describeError(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}

/**
 * 通用 HTTP 请求管线：超时 + 退避重试 + 认证头 + 幂等键 + 中间件。
 *
 * 与 core 的浏览器端点层（ApiClient）职责分层：本管线面向 server-to-server
 * 与 admin/bot 自动化场景，不绑定具体业务端点。
 */
export class FetchRequestPipeline {
  private readonly options: FetchRequestPipelineOptions;
  private readonly fetchImpl: RequestFetch;
  private readonly retryPolicy: RetryBackoffPolicy;
  private readonly timeoutMs: number;
  private readonly middleware: readonly ApiRequestMiddleware[];

  constructor(options: FetchRequestPipelineOptions = {}) {
    this.options = options;
    this.fetchImpl = options.fetch ?? createDefaultFetch();
    this.retryPolicy = normalizeRetryBackoffPolicy(options.retry);
    this.timeoutMs = options.timeoutMs ?? DEFAULT_TIMEOUT_MS;
    this.middleware = [...(options.middleware ?? [])];
  }

  async request<TBody = unknown>(
    input: ApiClientRequestInput | string,
  ): Promise<ApiResponseContext<TBody>> {
    const normalizedInput = typeof input === 'string' ? { path: input } : input;

    for (let attempt = 1; ; attempt += 1) {
      const context = await this.prepareContext(normalizedInput);

      let response: ApiResponseContext;
      try {
        response = await this.execute(context, normalizedInput.signal);
      } catch (error) {
        // 调用方主动取消不重试、不包装
        if (normalizedInput.signal?.aborted) {
          throw error;
        }
        const wrapped =
          error instanceof ApiClientNetworkError
            ? error
            : new ApiClientNetworkError(describeError(error), error);
        if (!shouldRetryRequest({ attempt, isNetworkError: true }, this.retryPolicy)) {
          throw wrapped;
        }
        await this.sleep(computeRetryDelay(this.retryPolicy, attempt));
        continue;
      }

      if (response.status >= 400) {
        if (!shouldRetryRequest({ attempt, statusCode: response.status }, this.retryPolicy)) {
          throw new ApiClientHttpError(response.status, response.headers, response.body);
        }
        await this.sleep(computeRetryDelay(this.retryPolicy, attempt));
        continue;
      }

      return this.runResponseMiddleware<TBody>(context, response as ApiResponseContext<TBody>);
    }
  }

  private async prepareContext(input: ApiClientRequestInput): Promise<ApiRequestContext> {
    let context: ApiRequestContext = {
      method: input.method ?? 'GET',
      path: input.path,
      headers: { ...(input.headers ?? {}) },
      query: input.query ? { ...input.query } : undefined,
      body: input.body,
      idempotencyKey: input.idempotencyKey,
      metadata: input.metadata ? { ...input.metadata } : undefined,
    };

    for (const middleware of this.middleware) {
      if (middleware.onRequest) {
        context = await middleware.onRequest(context);
      }
    }

    context = await this.applyAuthHeaders(context);
    return this.applyIdempotencyKey(context);
  }

  private async applyAuthHeaders(context: ApiRequestContext): Promise<ApiRequestContext> {
    const provider = this.options.authProvider;
    if (!provider) {
      return context;
    }

    const authHeaders: ServerAuthHeaders = await provider.getHeaders(this.options.authContext);
    const headers = { ...context.headers };

    if (authHeaders.authorization !== undefined && headers['authorization'] === undefined) {
      headers['authorization'] = authHeaders.authorization;
    }
    if (authHeaders.apiKey !== undefined) {
      const keyName = this.apiKeyHeaderName(provider);
      if (headers[keyName] === undefined) {
        headers[keyName] = authHeaders.apiKey;
      }
    }
    for (const [key, value] of Object.entries(authHeaders.headers ?? {})) {
      const normalizedKey = key.toLowerCase();
      if (headers[normalizedKey] === undefined) {
        headers[normalizedKey] = value;
      }
    }

    return { ...context, headers };
  }

  private apiKeyHeaderName(provider: ServerAuthProvider): string {
    return 'keyName' in provider && typeof provider.keyName === 'string'
      ? provider.keyName
      : DEFAULT_API_KEY_HEADER;
  }

  private applyIdempotencyKey(context: ApiRequestContext): ApiRequestContext {
    let key = context.idempotencyKey;
    if (key === undefined && this.options.idempotencyKeyProvider) {
      key = this.options.idempotencyKeyProvider.generate(context);
    }
    if (key === undefined || context.headers[IDEMPOTENCY_KEY_HEADER] !== undefined) {
      return context;
    }
    return {
      ...context,
      headers: { ...context.headers, [IDEMPOTENCY_KEY_HEADER]: key },
    };
  }

  private async execute(
    context: ApiRequestContext,
    externalSignal?: AbortSignal,
  ): Promise<ApiResponseContext> {
    const headers = { ...context.headers };
    let body: string | undefined;
    if (context.body !== undefined && context.method !== 'GET') {
      if (typeof context.body === 'string') {
        body = context.body;
      } else {
        body = JSON.stringify(context.body);
        if (headers['content-type'] === undefined) {
          headers['content-type'] = 'application/json';
        }
      }
    }

    const controller = new AbortController();
    let timedOut = false;
    const timeoutId = setTimeout(() => {
      timedOut = true;
      controller.abort();
    }, this.timeoutMs);
    const onExternalAbort = () => controller.abort();
    externalSignal?.addEventListener('abort', onExternalAbort, { once: true });

    try {
      const response = await this.fetchImpl(this.buildUrl(context), {
        method: context.method,
        headers,
        body,
        signal: controller.signal,
      });

      const text = await response.text();
      let parsedBody: unknown;
      if (text.length > 0) {
        try {
          parsedBody = JSON.parse(text);
        } catch {
          parsedBody = text;
        }
      }

      return {
        status: response.status,
        headers: headersToRecord(response.headers),
        body: parsedBody,
      };
    } catch (error) {
      if (timedOut) {
        throw new ApiClientNetworkError(`request timed out after ${this.timeoutMs}ms`, error);
      }
      throw error;
    } finally {
      clearTimeout(timeoutId);
      externalSignal?.removeEventListener('abort', onExternalAbort);
    }
  }

  private buildUrl(context: ApiRequestContext): string {
    const rawPath = context.path.startsWith('/') ? context.path.slice(1) : context.path;
    const baseUrl = this.options.baseUrl?.replace(/\/+$/, '');
    const base = baseUrl === undefined ? undefined : `${baseUrl}/`;

    let url: URL;
    try {
      url = new URL(rawPath, base);
    } catch {
      throw new Error(
        'request path must be an absolute URL or options.baseUrl must be configured',
      );
    }

    for (const [key, value] of Object.entries(context.query ?? {})) {
      url.searchParams.set(key, value);
    }
    return url.toString();
  }

  private async runResponseMiddleware<TBody>(
    context: ApiRequestContext,
    response: ApiResponseContext<TBody>,
  ): Promise<ApiResponseContext<TBody>> {
    let current = response;
    for (const middleware of this.middleware) {
      if (middleware.onResponse) {
        current = await middleware.onResponse<TBody>(context, current);
      }
    }
    return current;
  }

  private sleep(ms: number): Promise<void> {
    return new Promise((resolve) => setTimeout(resolve, ms));
  }
}
