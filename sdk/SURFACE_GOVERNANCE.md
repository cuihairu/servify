# SDK Surface Governance

## Package Naming And Publishing Strategy

- `@servify/core`: shared browser runtime contracts and Web primitives
- `@servify/react`, `@servify/vue`, `@servify/vanilla`: framework surfaces built on top of `@servify/core`
- `@servify/api-client`: server-side API client surface (auth providers, retry policy, request middleware pipeline)
- `@servify/app-core`: reserved contract package for future mobile SDK work
- future transport packages should follow `@servify/transport-http`, `@servify/transport-websocket`, `@servify/transport-sse`
- `core/react/vue/vanilla/api-client` are eligible for production release today
- remaining reserved packages stay `0.0.0` and `private` until runtime behavior is implemented and reviewed

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

## Example And README Alignment

- every implemented surface package must have a `README.md`
- every example must reference the same package name shown in the matching README
- CI should run `npm -C sdk run test:governance` together with surface smoke tests
