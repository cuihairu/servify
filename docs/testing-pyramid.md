# Testing Pyramid

## Integration build tag coverage

Current `integration` coverage is concentrated in `apps/server/internal/handlers` (HTTP handler to runtime wiring) and spread across `apps/server/internal/modules/*` infra/application/delivery packages plus `internal/platform/realtime` — 38 test files over 15 packages.

Covered areas:

- agent
- ai
- app market
- automation
- custom fields
- customer
- gamification
- knowledge docs
- macro
- satisfaction
- session transfer
- shift
- sla
- statistics
- suggestion
- ticket
- websocket
- workspace

Specialized integration tag:

- `integration && sqlite_integration`
  - `apps/server/internal/modules/ticket/infra/gorm_repository_test.go`
  - `apps/server/internal/modules/ticket/infra/gorm_repository_scope_integration_test.go`

Previously missing from the integration layer and now covered:

- `voice` protocol HTTP runtime path
- orchestrated `ai` provider fallback path

## Test layer responsibilities

- Unit tests
  - Pure module logic, adapters, DTO mapping, prompt building, retry policy, SDK package behavior
- Integration tests
  - HTTP handler to runtime wiring, database-backed service flows, provider fallback wiring, protocol routing
- Smoke tests
  - Small high-signal checks that protect critical surfaces from obvious regressions

## Minimal smoke suite

Run with:

- `./scripts/run-smoke-tests.sh`

Current smoke set (see `scripts/run-smoke-tests.sh` for the authoritative list):

- conversation handler / application / infra status mapping
- ticket handler: create/get/list/assign, custom fields + export, related conversations
- voice handler integration path
- orchestrated AI provider fallback (`./apps/server/internal/modules/ai/delivery`)
- full-project `go build ./...`
- `npm -C sdk run test:examples`

Why these are in the smoke set:

- `conversation`/`ticket`
  - the customer-facing main chain: session, message, ticket assignment
- `voice`
  - exercises protocol route registration and stateful runtime behavior
- `ai`
  - verifies orchestrated provider failure still degrades to fallback safely
- `sdk examples`
  - ensures example entry points still point at the supported SDK surfaces
