# 07 SDK Multi Surface

Scope:

- Wrap up the Web SDK
- Keep refining the future API SDK reservation
- Keep refining the future App SDK reservation
- External contract stability

Constraints:

- The web surface shipped first; API and app surfaces are implemented as well (`sdk/packages/*`, `sdk/android`, `sdk/ios`; acceptance records in `sdk/ACCEPTANCE-M3.md` / `sdk/ACCEPTANCE-M4.md`)
- All surfaces share the single contract core (`sdk/packages/core`) and the PROTOCOL fixtures

## M1 web-sdk-completion

- [x] Add a `core` capability negotiation test matrix
- [x] Add reconnect policy implementation and tests
- [x] Add token refresh hook implementation and tests
- [x] Add smoke tests for `react/vue/vanilla` against the unified contract
- [x] Clean up current lint warnings

Acceptance:

- The Web SDK reaches a sustainably evolving state and no longer relies on implicit behavior

## M2 api-sdk-reservation

- [x] Define the server-side auth contract in `api-client`
- [x] Define the retry/backoff contract
- [x] Define the idempotency/request middleware contract
- [x] Define bot/admin automation usage examples

Acceptance:

- A later server-to-server SDK can directly reuse the existing contracts

## M3 app-sdk-reservation

- [x] Define the offline queue contract in `app-core`
- [x] Define the push token registration contract
- [x] Define the reconnect/session restore contract
- [x] Define the mobile storage abstraction contract

Acceptance:

- The future mobile SDK will not simply copy the Web SDK structure

## M4 surface-governance

- [x] Define package naming and release strategy
- [x] Define the public API review boundary
- [x] Add a breaking change checklist
- [x] Add examples and package README alignment checks

Acceptance:

- The SDK external surface has explicit governance rules

## M5 transport-evolution

- [x] Refine the `transport-http` package structure
- [x] Refine the `transport-websocket` package structure
- [x] Reserve SSE / webhook callback transport contracts
- [x] Reserve the API/App/Web shared serializer contract

Acceptance:

- The transport layer becomes a truly independent capability instead of scattered implementations inside core
