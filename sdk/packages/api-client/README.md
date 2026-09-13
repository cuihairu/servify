# @servify/api-client

服务端 API 访问层：认证提供者、重试策略、幂等键与请求中间件的通用 HTTP 管线。

面向 server-to-server 调用、admin 自动化与 bot 集成场景；不绑定具体业务端点，
与 `@servify/core` 的浏览器端点层职责分层并存。

## 安装

```bash
npm install @servify/api-client
```

## 快速开始

```ts
import { FetchRequestPipeline } from '@servify/api-client';

const api = new FetchRequestPipeline({
  baseUrl: 'https://api.example.com',
  timeoutMs: 5000,
  retry: { maxAttempts: 3, baseDelayMs: 250 },
  authProvider: {
    getHeaders: async () => ({ authorization: 'Bearer <token>' }),
  },
  idempotencyKeyProvider: {
    generate: ({ path }) => `auto:${path}`,
  },
});

const response = await api.request<{ id: number }>({
  method: 'POST',
  path: '/api/v1/tickets',
  body: { title: '需要帮助' },
  idempotencyKey: 'create-ticket-42',
});
```

字符串路径是 GET 的快捷方式：`await api.request('/health')`。

## 行为

- **超时**：每次尝试由 `AbortController` 硬性限时（`timeoutMs`，默认 10s），
  超时按可重试网络错误处理。
- **重试**：`shouldRetryRequest` + `computeRetryDelay` 指数退避；网络错误与
  `408/409/425/429/500/502/503/504` 默认可重试，策略可覆盖。
- **认证**：`ServerAuthProvider` 产出的 `authorization` / `apiKey`（放在 provider
  `keyName` 头下，默认 `x-api-key`）/ 自定义头；请求级显式头优先于注入头。
- **幂等**：`IdempotencyKeyProvider` 自动生成或请求级 `idempotencyKey` 显式指定，
  落在 `Idempotency-Key` 头。
- **中间件**：`onRequest` 按注册顺序执行；`onResponse` 只在最终成功响应上按序执行
  （失败响应由重试逻辑消费，不进入变换链）。
- **错误**：非重试耗尽后抛 `ApiClientHttpError`（status/headers/body），
  网络层失败抛 `ApiClientNetworkError`；调用方主动 abort 原样抛出、不重试。

## 环境注入

非 DOM 宿主（SSR、测试、受限运行时）可注入自定义 `fetch`；缺省探测
`globalThis.fetch`，不可用时构造报错并提示注入。

## Exported Contracts

- `FetchRequestPipeline` / `ApiClientRequestInput` / `RequestFetch`
- `ApiClientHttpError` / `ApiClientNetworkError`
- `ServerAuthProvider` / `BearerTokenAuthProvider` / `ApiKeyAuthProvider`
- `RetryBackoffPolicy` / `normalizeRetryBackoffPolicy` / `shouldRetryRequest` / `computeRetryDelay`
- `ApiRequestMiddleware` / `IdempotencyKeyProvider`
- `automationExamples`

## 许可

MIT
