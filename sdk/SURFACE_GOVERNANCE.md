# SDK Surface Governance

## Package Naming And Publishing Strategy

- `@servify/core`: shared browser runtime contracts and Web primitives
- `@servify/react`, `@servify/vue`, `@servify/vanilla`: framework surfaces built on top of `@servify/core`
- `@servify/api-client`: server-side API client surface (auth providers, retry policy, request middleware pipeline)
- `@servify/react-native`: headless mobile binding (mobile capability set, storage/snapshot wiring, provider and hooks)
- `@servify/app-core`: mobile foundation surface (storage adapter, offline queue, session snapshot, push token registrar)
- future transport packages should follow `@servify/transport-http`, `@servify/transport-websocket`, `@servify/transport-sse`
- `core/react/vue/vanilla/api-client/app-core/react-native` are eligible for production release today
- future reserved packages stay `0.0.0` and `private` until runtime behavior is implemented and reviewed

## Public API Review Boundary

- anything exported from `src/index.ts` is public API
- deep imports from `src/**` are internal-only and may change without notice
- new exports require review for naming, lifecycle ownership, and cross-surface consistency
- contract-first additions are preferred over implicit runtime flags

## Breaking Change Checklist

- confirm whether any `src/index.ts` export was removed, renamed, or changed semantically
- confirm package README usage snippets still match the exported API
- confirm example apps still compile against the published entrypoints
- confirm still-reserved packages remain design-time only unless explicitly promoted
- add migration notes before changing transport/auth/session contracts

## Legacy Naming Cleanup (P3-1, 2026-09)

Removed from the public surface (was a legacy alias pair for the same behavior,
replaced by `ServifyConfig.reconnectPolicy` since P2):

- `ServifyConfig.reconnectAttempts` / `ServifyConfig.reconnectDelay`
  (config fields; use `reconnectPolicy` — defaults are unchanged: 5 attempts, 1s base delay)
- `LegacyReconnectOptions` and the second parameter of
  `normalizeReconnectPolicy` (internal contract module, not exported from
  `src/index.ts`)

Internal rename with no API impact: `WebSocketManager`'s private retry counter
is now `reconnectAttemptCount` to avoid confusion with the removed config
fields.

Deliberately retained (provider real name or storage contract, not legacy
naming — server-side counterparts recorded in `docs/surface-naming.md`):
`weknora.*` configuration keys, `weknora_usage_count` / `weknora_latency`
metrics fields (per-provider sub-counters alongside
`knowledge_provider_usage_count`), and the `service_type='weknora'` enum.

## Example And README Alignment

- every implemented surface package must have a `README.md`
- every example must reference the same package name shown in the matching README
- CI should run `npm -C sdk run test:governance` together with surface smoke tests

## Example Build Strategy

- `@servify/*` packages are not published to the npm registry; example apps must
  consume them through a vite `resolve.alias` pointing at the built
  `packages/*/dist/index.esm.js` — never through a `dependencies` entry
  (an `npm install` with `"@servify/*"` in dependencies fails with 404)
- examples that ship a `package.json` must commit their `package-lock.json`
- CI builds every example (`vite build`) after `npm -C sdk run build`, so an
  example that stops compiling against the published entrypoint fails CI
