# 01 Platform And Runtime

Scope:

- bootstrap
- app wiring
- router assembly
- auth
- event bus
- realtime abstraction
- migration entry

## P1 bootstrap skeleton

- [x] Create `apps/server/internal/app/bootstrap`
- [x] Create `apps/server/internal/app/server`
- [x] Create `apps/server/internal/app/worker`
- [x] Add `bootstrap/app.go`
- [x] Define the `App` struct
- [x] Close out `Config`
- [x] Close out `Logger`
- [x] Close out `DB`
- [x] Close out `Router`
- [x] Close out `Workers`
- [x] Close out `ShutdownHooks`
- [x] Add the `BuildApp(cfg)` entrypoint
- [x] Add `bootstrap/app_test.go`

Acceptance:

- `BuildApp` exists
- No change to existing functional paths

Recovery point:

- The directory and `App` exist, but the entrypoint is not wired up yet

## P2 config logging observability

- [x] Add `bootstrap/config.go`
- [x] Add `bootstrap/logging.go`
- [x] Add `bootstrap/observability.go`
- [x] Switch the server entrypoint to bootstrap config/logging
- [x] Switch the cli entrypoint to bootstrap config/logging
- [x] Hook tracing shutdown uniformly into `ShutdownHooks`

Acceptance:

- Entrypoints no longer re-initialize config/logger/tracing

## P3 database and migration

- [x] Add `bootstrap/database.go`
- [x] Extract the DSN builder
- [x] Extract the GORM builder
- [x] Extract GORM tracing plugin injection
- [x] Add `bootstrap/migrate.go`
- [x] Close out the model migration list
- [x] Keep the server auto-migration compatibility switch

Acceptance:

- `AutoMigrate` is no longer hardcoded across multiple entrypoints

## P4 router assembly

- [x] Add `app/server/router.go`
- [x] Add `app/server/middleware.go`
- [x] Add `app/server/health.go`
- [x] Add `app/server/static.go`
- [x] Extract Gin init
- [x] Extract middleware registration
- [x] Extract static root detection

Acceptance:

- `cmd/server/main.go` no longer creates and assembles the router directly

## P5 workers

- [x] Add `app/worker/jobs.go`
- [x] Define the `Worker` interface
- [x] Wrap the SLA monitor
- [x] Wrap the statistics worker
- [x] Register workers on `App`
- [x] Add worker start/stop test placeholders

Acceptance:

- Background jobs are no longer scattered across entrypoint files

## P6 auth extraction

- [x] Create `internal/platform/auth`
- [x] Add `claims.go`
- [x] Add `validator.go`
- [x] Add `permissions.go`
- [x] Add `resolver.go`
- [x] Add `gin_middleware.go`
- [x] Move JWT parsing out of middleware
- [x] Move perms expansion out of middleware

Acceptance:

- The Gin middleware only does HTTP adaptation

## P7 event bus

- [x] Create `internal/platform/eventbus`
- [x] Define `Event`
- [x] Define `Handler`
- [x] Define `Bus`
- [x] Implement `inmemory_bus.go`
- [x] Add contract tests
- [x] Inject the bus into `App`

Acceptance:

- Modules can communicate via the event bus

## P8 realtime abstraction

- [x] Create `internal/platform/realtime`
- [x] Define `RealtimeGateway`
- [x] Define `RTCGateway`
- [x] Wrap the existing `WebSocketHub`
- [x] Wrap the existing `WebRTCService`

Acceptance:

- The business layer no longer depends directly on realtime structs in the old services
