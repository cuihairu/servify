> **English** | [中文](release-0.1.0-acceptance.md)

# Servify 0.1.0 Release Acceptance

> Note: this document is the **v0.1.0 historical release acceptance spec** (superseded by v1.0.0; the current spec is in [acceptance-checklist.md](./acceptance-checklist.md)). The `internal/services/*` evidence paths below reflect the time when this doc was written — that directory has since been removed wholesale under P3-2, and the test migration landing points are noted in the acceptance-checklist.

This document defines the release acceptance scope, entry gates, mandatory test functions, evidence requirements, and known boundaries for `Servify v0.1.0`.

Version positioning:

- `v0.1.0` is the first public preview
- Goal: installable, deployable, demoable, with the main chain runnable
- Not: an enterprise production-stable version; not equivalent to `v1.0`

Suggested release messaging:

- It is fine to say: the core customer-service main chain, AI baseline capabilities, and baseline admin operations are ready for preview delivery
- It is not fine to say: enterprise-grade production-stable delivery is complete

## 1. Release Gate

Before releasing `v0.1.0`, all of the following must hold simultaneously:

1. `make build` passes
2. `make release-check CONFIG=./config.yml` passes
3. Core customer-service main chain acceptance all reaches `passed`
4. Auth self-service session chain reaches `passed`
5. At least 1 real provider main path for AI / Knowledge reaches `passed`
6. This document, `acceptance-checklist.md`, README, and deployment docs are synced
7. The release note explicitly states known limitations and non-goals

Veto items:

- Service fails to start
- Database migration fails
- Core interface returns 500 or a structure that does not match the contract
- Core state transitions have no data evidence
- Auth/permission middleware fails
- When the external knowledge provider is unavailable there is no fallback or the error is uncontrollable

## 2. Current Version Boundaries

`v0.1.0` explicitly does not commit to the following:

- Multi-instance HA and cross-instance consistency
- Async durability under persistent message queue semantics
- A complete co-browsing remote-assistance product
- Production-grade integration with a real voice provider
- Customer-side recommended questions / context-associated questions
- Enterprise-grade capacity, load testing, and backup/recovery baselines

Known implementation boundaries that must be called out explicitly in the release note:

- The default event bus is still an in-process implementation
- The voice runtime default wiring still has a mock/provider boundary
- The agent runtime still has in-memory state / legacy compatibility residue
- Compatibility / mock assets are only for dev/demo/acceptance assistance; they are not equivalent to production capability

## 3. Acceptance Evidence Rules

Every function item must have all 4 types of evidence below; otherwise it cannot be marked as `passed`:

1. Code entry: a route, page, CLI, or SDK call exists
2. Automated evidence: unit tests, integration tests, or script validation exist
3. Runtime evidence: real startup, real request, real response
4. Data evidence: before/after changes in DB, state transitions, files, or external dependencies are visible

Suggested recording format:

| Field | Content |
| --- | --- |
| Function item | e.g. "Send conversation message" |
| Page/API | explicit entry |
| Preconditions | data, account, dependent services |
| Steps | request or operation process |
| Expected result | status code, response, state change |
| Automated evidence | test files or commands |
| Manual evidence | curl, screenshot, logs, DB query |
| Whether a 0.1.0 blocker | yes / no |
| Status | unverified / partially passed / passed / blocked |

## 4. Function Package Acceptance Scope

`v0.1.0` is accepted as 8 function packages.

### 4.1 Baseline Runtime

Goals:

- The repo can be built
- The service can be started
- Health, readiness, and metrics surfaces are accessible
- The pre-release minimal baseline check is executable

Functions that must be accepted:

1. Database migration
2. Standard build
3. Service startup
4. Health check
5. Readiness check
6. Metrics exposure
7. Security baseline check
8. Observability baseline check
9. Release readiness check

Acceptance items:

| Function item | Page/API/command | Blocker |
| --- | --- | --- |
| Database migration | `make migrate` | yes |
| Standard build | `make build` | yes |
| Service startup | `go -C apps/server run ./cmd/server` | yes |
| Health check | `GET /health` | yes |
| Readiness check | `GET /ready` | yes |
| Prometheus metrics | `GET <metricsPath>` | yes |
| Security baseline check | `make security-check CONFIG=...` | yes |
| Observability baseline check | `make observability-check CONFIG=...` | yes |
| Pre-release check | `make release-check CONFIG=...` | yes |

Minimum set of automated evidence:

- `apps/server/internal/handlers/health_enhanced_test.go`
- `apps/server/cmd/cli/check_security_baseline_test.go`
- `apps/server/cmd/cli/check_observability_baseline_test.go`
- `scripts/check-release-readiness.sh`

### 4.2 Authentication and Login

Goals:

- An admin can log in to the system
- Token rotation is correct
- Self-service session management works
- Unauthorized access is blocked

Functions that must be accepted:

1. Login
2. Refresh token rotation
3. Current session list
4. Log out current session
5. Log out other sessions
6. Unauthorized interception

Acceptance items:

| Function item | Page/API | Blocker |
| --- | --- | --- |
| Login | `POST /api/v1/auth/login` | yes |
| Refresh rotation | `POST /api/v1/auth/refresh` | yes |
| Current session list | `GET /api/v1/auth/sessions` | yes |
| Log out current session | `POST /api/v1/auth/sessions/logout-current` | yes |
| Log out other sessions | `POST /api/v1/auth/sessions/logout-others` | yes |
| Unauthorized interception | Accessing a protected API | yes |

Minimum set of automated evidence:

- `apps/server/internal/handlers/auth_handler_test.go`
- `apps/server/internal/services/auth_service_test.go`
- `apps/server/internal/middleware/auth_extended_test.go`

### 4.3 Customer Consultation Main Chain

Goals:

- End-to-end from conversation entry through message handling, human takeover, transfer, and close

Functions that must be accepted:

1. Workspace overview shows conversations
2. Conversation detail is readable
3. Message list is readable
4. Sending a message succeeds
5. Taking over a conversation succeeds
6. Transferring a conversation succeeds
7. Closing a conversation succeeds

Acceptance items:

| Function item | Page/API | Blocker |
| --- | --- | --- |
| Workspace overview | `GET /api/omni/workspace` | yes |
| Conversation detail | `GET /api/omni/sessions/:id` | yes |
| Message list | `GET /api/omni/sessions/:id/messages` | yes |
| Send message | `POST /api/omni/sessions/:id/messages` | yes |
| Take over conversation | `POST /api/omni/sessions/:id/assign` | yes |
| Transfer conversation | `POST /api/omni/sessions/:id/transfer` | yes |
| Close conversation | `POST /api/omni/sessions/:id/close` | yes |

Minimum set of automated evidence:

- `apps/server/internal/handlers/conversation_workspace_handler_test.go`
- `apps/server/internal/services/websocket_test.go`

Minimum manual acceptance requirements:

1. Really log in to the admin console
2. Open the workspace
3. View one real conversation
4. Send one agent message
5. Perform one takeover
6. Perform one transfer
7. Perform one close

### 4.4 Ticket Full Flow

Goals:

- When a consultation cannot be resolved immediately, it can enter the ticket full flow

Functions that must be accepted:

1. Create ticket
2. Ticket list
3. Ticket detail
4. Update ticket
5. Assign ticket
6. Add comment
7. Close ticket
8. Ticket statistics
9. Ticket export

Acceptance items:

| Function item | Page/API | Blocker |
| --- | --- | --- |
| Create ticket | `POST /api/tickets` | yes |
| Ticket list | `GET /api/tickets` | yes |
| Ticket detail | `GET /api/tickets/:id` | yes |
| Update ticket | `PUT /api/tickets/:id` | yes |
| Assign ticket | `POST /api/tickets/:id/assign` | yes |
| Add comment | `POST /api/tickets/:id/comments` | yes |
| Close ticket | `POST /api/tickets/:id/close` | yes |
| Ticket statistics | `GET /api/tickets/stats` | yes |
| Export ticket | `GET /api/tickets/export` | yes |

Minimum set of automated evidence:

- `apps/server/internal/handlers/ticket_handler_test.go`
- `apps/server/internal/modules/ticket/application/command_service_test.go`
- `apps/server/internal/modules/ticket/application/query_service_test.go`

### 4.5 AI and Knowledge Base

Goals:

- AI has at least stable Q&A capability
- The external knowledge provider is controllable, uploadable, and syncable
- Fallback works when the provider errors

Functions that must be accepted:

1. AI query
2. AI status
3. AI metrics
4. Knowledge upload
5. Knowledge sync
6. Enable provider
7. Disable provider
8. Circuit breaker reset
9. Fallback behavior

Acceptance items:

| Function item | Page/API | Blocker |
| --- | --- | --- |
| AI query | `POST /api/v1/ai/query` | yes |
| AI status | `GET /api/v1/ai/status` | yes |
| AI metrics | `GET /api/v1/ai/metrics` | no |
| Knowledge upload | `POST /api/v1/ai/knowledge/upload` | yes |
| Knowledge sync | `POST /api/v1/ai/knowledge/sync` | yes |
| Enable provider | `PUT /api/v1/ai/knowledge-provider/enable` | yes |
| Disable provider | `PUT /api/v1/ai/knowledge-provider/disable` | yes |
| Circuit breaker reset | `POST /api/v1/ai/circuit-breaker/reset` | no |
| Provider fallback | AI main chain | yes |

Minimum set of automated evidence:

- `apps/server/internal/handlers/ai_handler_comprehensive_test.go`
- `apps/server/internal/services/orchestrated_ai_fallback_integration_test.go`

Minimum manual acceptance requirements:

1. At least one real provider query succeeds
2. At least one real document upload succeeds
3. At least one real sync succeeds
4. At least one fallback succeeds when the provider errors

### 4.6 Admin Console Baseline Operations

Goals:

- The console has a minimal operations chain; it is not a pure demo shell

Functions that must be accepted:

1. Dashboard reads real data
2. Customer core CRUD
3. Agent core operations

Acceptance items:

| Function item | Page/API | Blocker |
| --- | --- | --- |
| Dashboard statistics | `GET /api/statistics/dashboard` | yes |
| Create customer | `POST /api/customers` | no |
| Customer list | `GET /api/customers` | no |
| Customer detail | `GET /api/customers/:id` | no |
| Update customer | `PUT /api/customers/:id` | no |
| Customer notes | `POST /api/customers/:id/notes` | no |
| Customer tags | `PUT /api/customers/:id/tags` | no |
| Agent list | `GET /api/agents` | no |
| Agent online | `POST /api/agents/:id/online` | no |
| Agent offline | `POST /api/agents/:id/offline` | no |
| Agent status update | `PUT /api/agents/:id/status` | no |
| Online agent list | `GET /api/agents/online` | no |

Minimum set of automated evidence:

- `apps/server/internal/handlers/customer_handler_test.go`
- `apps/server/internal/handlers/agent_handler_test.go`
- `apps/server/internal/handlers/agent_handler_extended_test.go`
- `apps/server/internal/services/statistics_service_test.go`

### 4.7 Realtime Capabilities

Goals:

- Prove that the WebSocket / WebRTC baseline capabilities are real runtime capabilities, not faked implementations

Functions that must be accepted:

1. WebSocket connection
2. WebSocket send/receive
3. WebSocket statistics
4. WebRTC baseline negotiation chain
5. WebRTC statistics
6. WebRTC connection list

Acceptance items:

| Function item | Page/API | Blocker |
| --- | --- | --- |
| WebSocket connection | `GET /api/v1/ws` | yes |
| WebSocket statistics | `GET /api/v1/ws/stats` | no |
| WebRTC statistics | `GET /api/v1/webrtc/stats` | no |
| WebRTC connection list | `GET /api/v1/webrtc/connections` | no |

Minimum set of automated evidence:

- `apps/server/internal/services/websocket_test.go`
- `apps/server/internal/services/websocket_processing_test.go`
- `apps/server/internal/services/webrtc_test.go`
- `apps/server/internal/services/webrtc_stats_test.go`

### 4.8 Public Entry Points

Goals:

- If the README and website describe public entry points to the outside world, then at minimum acceptable capability must exist

Functions that must be accepted:

1. Portal config accessible anonymously
2. Public knowledge accessible anonymously
3. Public CSAT accessible and submittable anonymously

Acceptance items:

| Function item | Page/API | Blocker |
| --- | --- | --- |
| Portal config | `GET /public/portal/config` | no |
| Public knowledge list | `GET /public/kb/docs` | no |
| Public knowledge detail | `GET /public/kb/docs/:id` | no |
| Public CSAT query | `GET /public/csat/:token` | no |
| Public CSAT submit | `POST /public/csat/:token/respond` | no |

Minimum set of automated evidence:

- `apps/server/internal/handlers/csat_public_handler.go`
- Corresponding public entry handler tests

## 5. Functions That May Not Block the `0.1.0` Release

The following functions may continue to exist in the backlog, but should not block `v0.1.0`:

1. Customer-side recommended questions / context-associated questions
2. Satisfaction console full operations surface
3. Macros, automation, gamification, shifts
4. A more complete session transfer admin console
5. Real voice provider integration
6. Multi-instance HA
7. Capacity load testing and performance baselines
8. Enterprise-grade backup/recovery drills

## 6. Documentation Acceptance Scope

Before releasing `v0.1.0`, the following docs must be synced:

| Doc | Requirement |
| --- | --- |
| `README.md` | version positioning, core capabilities, and known limitations are explicit |
| `docs/local-development.md` | local run steps are executable |
| `docs/deployment.md` | minimal deployment, config layering, and dependency notes are clear |
| `docs/acceptance-checklist.md` | corresponding function item statuses are backfilled |
| `docs/release-versioning.md` | version and changelog process is correct |
| `docs/demo-and-mock-boundaries.md` | demo/mock/prod boundaries are clear |

## 7. Content the Release Note Must Include

When releasing `v0.1.0`, the release note must at least include:

1. Version positioning: the first public preview
2. Capabilities in place:
   - Core customer-service main chain
   - AI baseline Q&A and knowledge chain
   - Console baseline operations surface
   - WebSocket/WebRTC baseline capabilities
3. Known limitations:
   - event bus durability boundary
   - voice/provider boundary
   - single-instance priority
   - not a production-grade complete remote-assistance product
4. Upgrade/deployment instructions
5. Verified environment and execution method

## 8. Currently Suggested Execution Order

If `v0.1.0` is the target, the following order is suggested to close out:

1. Baseline runtime gaps: `GET /ready`, `GET <metricsPath>`, `make build`
2. Auth self-service session chain connected
3. Conversation workspace connected
4. Ticket high-frequency operations full flow
5. AI / Knowledge real provider acceptance closed out
6. `acceptance-checklist.md` backfilled
7. README / deployment / release note synced

## 9. Final Release Decision Criteria

The following conditions must be met to release `v0.1.0`:

1. All blocker function items are `passed` in this file and in `acceptance-checklist.md`
2. All release gate commands pass
3. Known limitations are publicly stated
4. Demo/mock capabilities are not misdescribed as production-stable capabilities

If any blocker still has `partially passed`, `unverified`, or `blocked`, then `v0.1.0` must not be released.
