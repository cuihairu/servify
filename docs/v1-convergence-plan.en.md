> **English** | [中文](v1-convergence-plan.md)

# Servify V1.0 Product and Architecture Convergence Plan

> Basis: ChatGPT review (2026-10-04, `docs/review/chatgpt-review-2026-10-04.md`) + this round's re-check (`docs/review/chatgpt-review-2026-10-04-verification.md`).
> Re-check conclusion: the judgment that product positioning needs convergence is【confirmed】; the architecture is over-platformized and should focus polish on the core customer-service product model and the Agent Workspace【confirmed】.
> This document turns the review recommendations into an actionable plan. **This document only plans; execution requires a separate sign-off.**

---

## 0. Goal and Principles

**Goal in one sentence:** converge Servify from the "wide skeleton of modules" stage into the V1.0 state of "an independently deployable Web intelligent customer service with a smooth main chain, an efficient agent workspace, and a usable knowledge chain".

**Positioning principles:**

1. **No new first-level modules.** `apps/server/internal/modules/` is capped at 27 modules; only merge/downgrade is allowed, no new openings.
2. **Conversation is the center of the service process, Ticket is the work object, Customer is the customer hub** — written into the domain docs (`ARCHITECTURE.md` 6.4/6.5/6.6 and `docs/current-architecture.md`).
3. **AI serves customer service; we are not building an AI Agent platform.** Tools stay at 3-4; no Planner/MCP/Multi-agent/Vector DB as the main narrative.
4. **Analytics is an observer** (event projection → read model); it must not drive the business decision chain in reverse.
5. **Event Bus stays in-process (+ optional Redis for multi-instance)**; no Kafka, no full event sourcing (keeping the `ARCHITECTURE.md` non-goal).
6. **Tenant/Workspace only handles governance and isolation**; the concept is pinned down and does not enter the product main narrative.
7. Every phase deliverable has **acceptance criteria**; pass the gate before entering the next phase.

---

## 1. Keep / Downgrade / Delete List

### 1.1 Keep and Enhance (first-layer core modules, 7)

| Module | Enhancement content | Related plan sections |
| --- | --- | --- |
| `conversation` | Identity repositioning: system center; add `ConversationEvent` (timeline projection source) linked with the session state machine | §5.2, §5.4 |
| `agent` | Add online status/load snapshots and workspace-side data contracts (Inbox/Queue query surfaces) | §4 |
| `customer` | Add Profile read model (tags, history, Ticket aggregation, Timeline aggregation) | §5.2, §6 |
| `routing` | Score-based routing engine (skill/language/availability/workload/priority/tier/channel/SLA) | §6 |
| `ticket` | Make the relation to Conversation explicit (`conversation_id`), state machine untouched, SLA attached | §7 |
| `ai` | Productization of the first-answer/assistant chain (structured output, feedback loop, summary retention) | §5.3 |
| `knowledge` | Productization (source/document/chunk/index/version/retrieval analytics, citation display) | §5.4 |

### 1.2 Downgrade to Sub-capability (no longer first-level product concepts; code kept but architectural presence tightened)

Per review §22 and re-check §11, the following modules **do not get their code deleted** (`.claude`/tests/migration risk is high), but:

- The narrative downgrades from "first-level module" to "sub-capability of a core module"; README/docs site no longer list them as separate product chapters;
- Convergence rule: **code locations do not move**; docs and the routing table are centralized; `sla`/`satisfaction`/`shift`/`macro`/`custom_field` attach to `ticket`/`routing`/`agent` combinations; `gamification`/`suggestion` attach to `conversation`+`analytics`; `quality` attaches to the `conversation` analytics view.

| Module | Downgrade destination | Notes |
| --- | --- | --- |
| `sla` | → `ticket` sub-capability (SLA evaluation/violation events) | No domain; thin shell; SLA logic inherently belongs to the ticket lifecycle |
| `satisfaction` | → `ticket` sub-capability (post-close rating) | No domain |
| `shift` | → `agent` sub-capability (scheduling is an availability data source) | No domain |
| `macro` | → `agent` sub-capability (canned replies are an agent tool) | No domain |
| `custom_field` | → `ticket` sub-capability (custom fields act on tickets) | No domain |
| `gamification` | → `analytics` sub-capability (performance scores are read-model derived) | No domain |
| `suggestion` | → `conversation` sub-capability (recommendation/intent assist) | No domain |
| `quality` | → `conversation` analytics and QA perspective | Keeps its domain; untouched for now |
| `statistics` (still scattered in `internal/handlers/statistics_*.go`) | → consolidated into the `analytics` module (including export) | Migration-style consolidation, not a downgrade |

### 1.3 Delete / Freeze List

| Item | Action | Rationale |
| --- | --- | --- |
| `api_key`, `email`, `push`, `webhook`, `translation`, `app_integration`, `auth` (thin shell inside the module) | **Freeze**: no new capability surface, excluded from all product copy | Multi-channel/open platform is a P2+ extension; note: what is frozen for auth is capability-surface expansion — its login/refresh/2FA/OIDC logic actually lives in `modules/auth/application` (Register/Login/RefreshToken/TOTP/OIDC) + the `platform/auth` policy layer, not a pure placeholder |
| `voice` and its extensions (SIP/PSTN/transcription) | **Frozen as an extension boundary**: keep as is (has domain + provider), no V1 resources invested | Review §14, v1-product-scope P2 |
| Any new first-level module | **Forbidden** (architecture gate: new capabilities must first find a home as a sub-capability of an existing module) | Review §11 |
| New channels (Telegram/WeCom/WhatsApp/Mobile UI/deep SIP integration) | Frozen | v1-product-scope `V1 不做的功能` |

---

## 2. Domain Boundaries (pinned-down version)

### 2.1 First-layer Relations (governance spec, written into `ARCHITECTURE.md` 6.4-6.6)

```text
Customer（客户中心）
   └── Conversation（服务过程中心，唯一核心聚合）
         ├── Message / Participant / ChannelBinding / ConversationEvent
         ├── AI interaction         （ai 模块，经 QueryOrchestrator）
         ├── Agent interaction      （agent 模块：接管/协作/转接）
         ├── Routing                （routing 模块：排队/评分/分配/转接）
         └── Ticket                 （ticket 模块：后续工作项，持有 conversation_id）
```

Rules (architecture gates):

1. **Ticket must be derived via Conversation** (`ticket.conversation_id NOT NULL` enforced from V1, historical data backfilled); no module may hang Conversation under Ticket in reverse.
2. `routing`, `ai`, `agent` only get read references to Conversation plus in-session status updates; write entry points converge into the `conversation` module application.
3. Module dependency direction allows only: `delivery → application → domain`; cross-module goes through in-module contracts or events; `models.go` penetration is forbidden (see §5.1).
4. Each first-layer core module gets a `domain/doc.go` "Owns / center position" comment block (Conversation's center position is pinned).

### 2.2 Horizontal Capabilities (not part of the business layering)

`Identity/Auth` (`platform/auth` + `modules/auth`), `Tenant/Workspace` (governance), `Realtime` (`platform/realtime`), `Event Bus` (`platform/eventbus`, in-process), `Observability`, `Storage`, `SDK` (`sdk/`, Web first).

### 2.3 Extension Boundary (outside the V1 module graph)

Remote Assist (the in-conversation assist entry stays), Voice/SIP, multi-channel, Mobile, deep business integrations.

---

## 3. Data Model

### 3.1 Core Table Convergence (continuously kept/added/migrated)

Current state: shared models are centralized in `apps/server/internal/models/models.go` (30+ tables, shared across modules). Convergence target: **each core module owns its own models and repositories**, `models.go` keeps only cross-module shared base types and the migration pipeline.

| Domain | Tables (current → target) | Notes |
| --- | --- | --- |
| conversation | `sessions`, `messages` (centrally defined in `models.go`) → + `conversation_participants` (independent table, currently embedded in Session or a FK), `channel_bindings`, **`conversation_events` (new)** | `conversation_events` is the persistence source of the Service Timeline: `{conversation_id, event_type, actor, payload, occurred_at, tenant_id, workspace_id}` |
| ticket | `tickets`, `ticket_comments`, `ticket_files`, `ticket_statuses`, `ticket_custom_field_values` (`models.go`) + **`tickets.conversation_id` (not implemented: neither model nor migration has this column, `session_id` is used today, `models.go:138`)** | Post-close rating (satisfaction) anchors on the ticket |
| customer | `customers` (`models.go`) + Profile read-model view (tags, history, Ticket aggregation) | — |
| agent | `agents` (`models.go`) + `agent_status`/load snapshot table (new or merged in) | Feeds routing scoring and workspace queries |
| routing | `transfer_records`, `waiting_records` (`models.go`) + **`routing_assignments` (persisted)** + `queue_entries` (currently in-memory → optional initial persistence) | Scoring factors land in tables (skills/language/priority/tier come from the opposite-side tables) |
| ai / knowledge | Existing retrieval/document tables + **`ai_answers` (answers/sources/confidence persisted)**, **`answer_feedback` ("was this helpful" rating)** | For the feedback loop |
| analytics | Existing `daily_stats` (`models.go`) etc., keeping read-model status | Statistics come from event projection, outside the business write path |
| sla | `sla_configs`, `sla_violations` kept | Downgraded to a ticket sub-capability; tables untouched |
| security | `audit_logs` (`models.go`) + **PII policy tables (retention rules, delete/export jobs)** | Implements review §18-② |
| tenant/workspace | `tenant_configs`, `workspace_configs` (`models.go`) + **core business tables gain `tenant_id`/`workspace_id` columns** | Ticket first (done); within V1 extended to conversation/customer/ticket attachments/routing/ticket comments |

### 3.2 tenant/workspace Persistence Rules (done in one step in V1)

- New SQL migrations: add `tenant_id`/`workspace_id` to `conversation`, `ticket`, `customer`, `knowledge` (document tables), `routing` records (default-workspace semantics backfilled in one migration).
- Repository queries are forced through scope filtering (the existing ticket repository pattern is the template).
- Concepts pinned (written into `docs/tenant-workspace-boundaries.md`, aligned with review §17):

```text
Tenant = 企业/组织；Workspace = 一个运营空间；Agent = 坐席；Customer = 客户；
Conversation = 一次服务过程；Ticket = 后续工作项
```

---

## 4. Agent Workspace (V1 top priority, highest ROI)

Location: rework `apps/admin/src/pages/Conversation` + add `apps/admin/src/pages/Workspace`, replacing the scattered-CRUD back-office reception experience.

### 4.1 Page Structure (review §7 three columns)

```text
┌──────────────┬──────────────────────────┬──────────────┐
│ Inbox/Queue  │ Conversation 会话区      │ Customer     │
│              │                          │ Profile      │
│ 🔴 12 排队中  │ AI summary（可折叠）     │ 标签          │
│ 🟡 8 AI 接待中│ Timeline（新）           │ 历史会话      │
│ 🟢 23 进行中  │ Messages（流式/富文本）   │ Tickets 汇   │
│              │ Reply（AI 建议/改写）     │ Knowledge    │
│ My tickets   │ 动作：接管/转接/创建工单  │ 建议（浮动）  │
└──────────────┴──────────────────────────┴──────────────┘
```

### 4.2 Features and Acceptance (one page completes the whole flow)

| # | Capability | Acceptance criteria |
| --- | --- | --- |
| W1 | Inbox aggregation (queued/in progress/my tickets, count badges) | Agent sees queue and load on the first screen after login, no page hopping |
| W2 | Single-conversation handling: takeover, reply, transfer, close | The whole chain completes inside the conversation page (current `Conversation/index.tsx` capabilities kept and extended) |
| W3 | AI summary + suggested replies/rewrites (copilot integration) | Reuse the three `POST /api/v1/ai/copilot` action types; one-click trigger in the conversation page |
| W4 | Knowledge suggestion sidebar | Retrieval called against the current conversation, with citations (relevance shown) |
| W5 | Customer panel (profile/tags/history/Ticket aggregation/Timeline) | Customer 360 viewable without leaving the conversation page |
| W6 | Timeline component (review §19) | Conversation page shows the `conversation_events` projection stream (10:32 created → 10:35 handoff → …) |
| W7 | Ticket linkage | One-click ticket creation/update from the conversation page; the Ticket page stays but as a "work object" view |
| W8 | Performance baseline | Conversation page first-screen render ≤ 1s (local dev); WS message latency no worse than today |

### 4.3 Intentionally Not Built

- No V1 full build of a standalone remote-assistance workspace UI (`AssistReviewPanel` stays as is);
- No mobile agent client.

---

## 5. AI (serving customer service, not a platform)

### 5.1 Existing Pieces Untouched

`QueryOrchestrator` / `PromptBuilder` / `Retriever` / `ToolRegistry` / `Guardrails` / `handoff_policy` / streaming (delta frames) / SessionHistoryLoader all kept (re-check §8 confirmed them reasonable).

### 5.2 Structured Output Convergence (aligned with v1-product-scope)

- First-answer output keeps the `answer/citations/confidence/next_action/handoff_reason/ticket_summary` structure; `next_action ∈ {answer, clarify, handoff, ticket}` is the stable state set; any proposed new state must first pass the "must not break the V1 main chain" check.

### 5.3 Feedback Loop (new, within P1)

- Endpoint: `POST /api/v1/ai/feedback` (rate one ai-answer as `helpful/not_helpful + optional comment`), persisted to `answer_feedback`;
- Entry points on both the conversation page and the visitor-side widget ("Was this helpful?" on the widget side, review §21/P4);
- The admin Knowledge page shows top unanswered / low-confidence questions (reusing analytics read-model projections).

### 5.4 Explicit Off-limits (architecture gates)

No introduction of: Agent Framework/MCP/Planner/Workflow orchestration/multi-Agent autonomy/model routing marketplace. The tool set stays at 3-4 (retrieve knowledge, conversation/customer context, create ticket, request human takeover). Anything beyond must first be written back into this document and agreed.

---

## 6. Routing (from "assignment" to a "scoring engine", incremental, no rewrite)

### 6.1 Current State (kept)

`queue/assignment/transfer/skill match/availability/load balance` already exist (`routing/domain/entities.go:5-57`); escalation goes through SLA+automation (`models.go`; `automation/application/service.go:427`) — untouched in V1.

### 6.2 Evolution (within P2, review §6 scoring model)

1. **Scorer integration point**: add a `Scorer` interface inside `routing/application` (multi-factor weighted); default factors aggregated:
   - `skill` (existing SkillMatchPolicy), `language` (conversation's first language/Agent languages), `availability` (online status + shift data source), `workload` (count of in-flight conversations), `priority` (conversation priority + customer tier), `tier` (customer tiering), `channel`, `SLA` (remaining deadline, weighted by violation threshold).
2. The assignment flow stays: `Conversation → Routing Engine（Scorer）→ Candidate Agents 排序 → Assignment`; scores and factors land in `routing_assignments` for audit and analytics projection.
3. Manual transfer and priority policy keep humans ahead of automation: scoring only affects the "automatically recommended candidate ordering", never auto-executes a transfer (consistent with current handoff semantics; manual/keyword triggers unchanged).

### 6.3 Acceptance

- With two agent groups of differing skill/language, assignment results are reproducible and match expected weights;
- Load factor: idle agents rank ahead of fully-loaded agents (unit test + 1 integration case);
- Scores and factors are visible (admin Routing page shows the rationale of each assignment, read/written via `routing_assignments`).

---

## 7. Ticket (work object, not peer to Conversation)

### 7.1 Relation Pinned

- `tickets.conversation_id` made explicit; migration backfills history (`conversation_id` nullable = legacy, required for new data). **Not implemented**: neither the `models.go` Ticket struct nor any migration has a `conversation_id` column; sessions are currently linked via `session_id` (`models.go:138`); this item remains planned.
- Lifecycle anchored to the conversation: when a Conversation closes, unfinished work must require first creating a ticket or an explicit downgrade (pre-close interception rule, within P1).
- `sla`, `satisfaction`, `custom_field` semantically belong to Ticket sub-capabilities (§1.2); code locations don't move but README/docs no longer give them separate narratives.

### 7.2 State Machine and Events (core untouched, only seams added)

- Keep the existing state transitions and `ticket.*` events (`ticket/application/events.go`);
- New projection: `ticket.*` events also enter `conversation_events` (Timeline fully covers the customer's view).

---

## 8. Knowledge (productization, P3)

Added per review §10 (incrementally, without tearing down the provider abstraction):

1. **Source model**: `knowledge_sources` (metadata registry for markdown/website/PDF/FAQ/API); documents attach to a source;
2. **Versions and indexing**: document version number + `index_jobs` linked to versions; failure retry and visible status (an index job prototype already exists; add versions and a status enum convergence);
3. **Retrieval analytics**: read `retrieval analytics` (top Q&A, zero-hit queries, low-confidence rate) → displayed on the Knowledge admin page;
4. **Citation visualization**: both visitor and agent sides render Sources (`📄 refund-policy.md relevance 0.91`); the Q&A chain depends on §5.3 feedback.

---

## 9. Horizontal (Analytics / Events / Security / Tenant)

### 9.1 Analytics (observer, foundation untouched)

- Keep the `analytics/delivery/eventbus_subscriber.go` count projections; consolidate the `statistics_*` still scattered in legacy handlers into the analytics module (including export);
- V1 milestone metrics (review §21/P6 spec): First Response Time, Resolution Time, AI Resolution Rate, AI Handoff Rate, Agent Load, Queue Time, Ticket Resolution Rate, CSAT (once satisfaction lands).

### 9.2 Event Bus

- Keep `inmemory` as default + `redis` for multi-instance; the event contract list updates `ARCHITECTURE.md` §11 in sync as modules converge;
- New events (`conversation.events`, `ai.answer_generated`, `ai.feedback_received`) are consumed for projection only and never enter the business write path.

### 9.3 Security (fix two things, keep the rest as is)

1. **Doc correction**: change the `README.md:238` security table to what is actually implemented (refresh/sessions/logout-current/2FA already landed; this re-check's evidence `router_auth.go:55,65-72`);
2. **Customer Data Boundary (within P2)**: PII inventory (name/email/phone/chat transcripts/attachments/order field index), `audit_logs` extended with data retention policy fields, new "data export" and "data deletion (with cascade erasure)" admin surfaces, configurable retention periods.

### 9.4 Tenant/Workspace

Persistence per §3.2 + pinned concepts; the pages entering V1 (Agent Workspace) scope-filter from day one.

---

## 10. Implementation Priority (phase gates, each with acceptance)

| Phase | Content | Deliverable | Acceptance gate |
| --- | --- | --- | --- |
| **P0 (immediately after this review)** | ① Re-check doc committed (done: verification report) ② README/docs drift fixes (§9.3-1, current-architecture snapshot update) ③ `ARCHITECTURE.md` 6.4-6.6 pins Conversation's center and Ticket's subordinate relation + `domain/doc.go` Owns comments | Docs + architecture statements | `make local-check` green; docs site free of stale spec residue |
| **P1 (product chain enhancement)** | Agent Workspace three-column workspace (all of §4); Conversation gets `conversation_events` persistence and the Timeline component; satisfaction/macro/shift narrative downgraded into place | Workspace can process a full conversation end to end | W1-W8 acceptance table fully passed; e2e: visitor enters → AI first answer → handoff → agent reply → ticket creation → close, full-chain automated case passes; `make release-check` green |
| **P2 (Routing + Security)** | Scorer scoring engine (§6.2); PII boundary (§9.3-2); statistics consolidated into analytics; core business tables backfilled with tenant/workspace | Score-based routing + data compliance surface | All three §6.3 acceptance cases pass; PII cases (export/delete/retention) all pass; migrations reversible on both empty and existing databases |
| **P3 (Knowledge productization)** | Sources/versions/retrieval analytics/citation display (§8); feedback loop (§5.3) | Full knowledge base management flow | Knowledge admin page completes the chain source→document→version→retrieval analytics→feedback review; `README_KNOWLEDGE.md` updated |
| **P4 (convergence and release)** | Thin-shell module narrative convergence (all of §1.2's doc surface); `TASKS.md`/acceptance matrix unified with the V1.0 spec; release `v1.0.0` | V1.0 release | Cumulative acceptance matrix all green across gates; `make release-check`+`security-check`+`observability-check` green; release notes written in the V1 convergence voice |

**Parallelizable without conflict**: P1's workspace and P2's Routing scoring both depend on the current conversation/routing boundaries and can proceed on two tracks; P3 depends on P2's analytics consolidation (read-model foundation); P4 is cumulative wrap-up.

---

## 11. V1.0 Acceptance Criteria (master gate)

Adopts the success criteria from `docs/v1-product-scope.md` and turns them into an actionable checklist:

1. An independent site starts a real conversation through the Web widget and completes AI first answer → human takeover → transfer → ticket creation → ticket close inside the conversation page (automated e2e case is the authority);
2. Agent Workspace is the single reception entry (back-office CRUD pages only serve configuration and never appear on the reception path);
3. AI degrades explicitly when it lacks grounding (guardrail); answers carry citations; feedback is queryable;
4. Routing assignments are explainable (factors visible) + load/skill priority is reproducible;
5. Core tables persist tenant/workspace; the admin surface scope-filters across the whole chain;
6. Production configuration does not rely on mock/in-memory pretending-to-deliver; PII export/deletion works;
7. Docs site, README, website, and acceptance checklist express the V1 main chain in one voice (reusing review §24's positioning language);
8. 27 modules, no more; thin-shell modules complete the downgrade narrative on the doc surface.

## 12. Execution Method (when handing to Code Agents)

Each phase splits into independent task entries (e.g. `docs/implementation/13-v1-convergence/`), each containing: target file paths, dependencies, acceptance commands (`make local-check` / specified unit tests / e2e scripts), and forbidden actions (e.g. "must not add first-level modules"). Execution order strictly follows the §10 gates; any gate not passed blocks the next phase.

---

## 13. Non-goals (explicitly out of scope for V1.0)

Multi-channel, voice/SIP/recording-transcription production chain, full remote assistance workspace, multi-Agent/Workflow/MCP/model routing, CRM/order/payment deep integration, provider marketplace, multi-tenant SaaS/billing, mobile agent client, Kafka/Event Sourcing, microservice split, new first-level modules.
