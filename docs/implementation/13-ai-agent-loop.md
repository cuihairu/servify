# 13. AI Agent Loop Wiring (P0)

> Status: **main loop wired (verified in the 2026-10-04 documentation reconciliation)** — the `Handle` in
> `query_orchestrator.go` now goes through the `handleWithTools` agent loop (`Chat → execute tools → feed back → repeat`,
> with a `maxSteps` cap); the streaming `streamInteraction` runs `ChatStream` each step and pushes over WS;
> production assembly (`modules/ai/delivery/enhanced_service.go`) has registered the three tools
> `customer_lookup` / `ticket_lookup` / `handoff` and injected `ToolExecutor`,
> and `AIRequest` carries `ToolPolicy{Enabled:true, MaxSteps:5}`.
> **Left over**: Port and PermissionChecker are passed as `nil` at tool registration (tool calls degrade with
> "port not configured"); wiring real data ports and permission checks remains future work; the transfer-to-human keyword
> short circuit is kept as a fallback.
> Related: Architecture Review 2026 · [02 AI and Knowledge](./02-ai-and-knowledge.md) · [10 Module Boundaries](./10-module-boundaries.md)

## Background and Motivation

The review conclusion (see architecture-review-2026): the current AI orchestration is **single-turn RAG**; the `tool` system is implemented but **not wired into the main loop**, and "transfer to human" relies on keyword heuristics. This task wires up the agent loop so the intelligent customer service can **act in multiple steps** (look up customers / look up tickets / transfer to human / ask follow-ups) — this is the key step upgrading the product from a "Q&A bot" to "intelligent customer service".

## Goals / Non-Goals

**Goals**
- `Handle` supports a tool-calling loop (the `stopWhen` equivalent, with a `maxSteps` cap against runaway)
- Wire in the 3 implemented tools: `customer_lookup` / `ticket_lookup` / `handoff`
- Streaming (`ChatStream`) connected to WebSocket (the SDK is already ws-first)

**Non-Goals**
- No new agent framework (this phase develops a minimal loop in-house; the Eino migration is left for P1 evaluation)
- No changes to the business / data / routing / voice modules

## Current State (wiring points · verified in the 2026-10-04 reconciliation; the original "plan (not implemented)" snapshot is outdated)

| Concern | Location | Current State |
|---|---|---|
| Orchestration core | `internal/modules/ai/application/query_orchestrator.go::Handle` | **Loop wired**: `handleWithTools` (Chat → execute tools → feed back → repeat, with a `maxSteps` cap) |
| Tool scaffolding | same directory `tools.go`(`ToolRegistry`/`ToolExecutor`), `tool_customer_lookup.go`, `tool_ticket_lookup.go`, `tool_handoff.go` | **Implemented and registered into the production registry**; Port/PermissionChecker are `nil` for now, and calls degrade with "port not configured" |
| Assembly / invocation | `internal/modules/ai/delivery/enhanced_service.go` (assembly) | `AIRequest` carries `ToolPolicy{Enabled:true, MaxSteps:5}`, `SetToolExecutor` is injected |
| Transfer to human | `internal/modules/ai/application/handoff_policy.go::ShouldTransferToHuman` | Keyword heuristics ("human/agent/complaint"), kept as a fallback short circuit |
| Streaming | `internal/modules/ai/application/query_orchestrator.go::streamInteraction` | **Wired**: `ChatStream` each step, chunks via `modules/ai/delivery/streaming.go` → pushed over WS |
| Trigger entries | `internal/platform/realtime/websocket_hub.go`(realtime) · REST `/api/v1/ai/query` | Both go through `ProcessQuery` / `ProcessQueryStream` |

## Implementation Steps

### Phase 1: non-streaming agent loop (get the loop working first)
1. **The orchestrator receives `ToolExecutor`**: add a field to the `query_orchestrator.go` struct; add a parameter to `NewQueryOrchestrator` (or `SetToolExecutor`); inject `ToolDefinition` when `Handle` builds the `ChatRequest` (`registry.List()` → `llm.ToolDefinition`).
2. **`Handle` adds the loop**: `retrieve → buildPrompt → loop{ Chat(with tools) → if `ToolCalls` empty break → execute each `ToolExecutor.Execute` → feed results back as `tool` role messages → Chat again}`, `maxSteps` cap (default 5); emit a span + metrics each round.
3. **Assembly wiring** (`orchestrated_ai_enhanced.go`): build a `ToolRegistry` registering the 3 tools (Port wired to `CustomerService` / `TicketService` / `HandoffPort.RequestHandoff`); build a `ToolExecutor` (with `PermissionChecker`); set `ToolPolicy.Enabled=true, AllowedTools=[...]` on `AIRequest`; **demote the leading `ShouldTransferToHuman` keyword short circuit to a fallback** (for when the LLM does not call the handoff tool).

### Phase 2: streaming
4. Add `HandleStream` (`ChatStream`, chunks accumulate `tool_call`, fed back after execution); chunks are pushed to the client over WS.
5. Turn on `capabilities` `ChatStream` `Enabled`.
6. Switch the `websocket.go` entry to `HandleStream`; REST keeps the non-streaming `Handle`.

### Phase 3: tests
7. Orchestrator unit tests: a mock LLM returns `tool_call` → verify execution / feed-back / second-round convergence; the `maxSteps` cap; tool disabled skips the loop.
8. Integration: run one multi-step conversation with a mock provider + streaming rendered word by word.

## Choice Notes (in-house vs Eino)

- **P0: develop the minimal loop in-house**: lightest, reuses the existing `ToolExecutor` / `LLMProvider` abstractions, 2–3 weeks.
- **P1: evaluate migrating to [Eino](https://github.com/cloudwego/eino)** (the "Go version of LangGraph"): gains workflow visualization / durable / ADK, at the cost of an extra framework dependency + learning curve.
- **Decision basis**: develop in-house first to validate the convergence and product value, then decide whether switching frameworks is worth it.

## Risks and Rollback

- Tool Port injection needs the business services (`CustomerService` / `TicketService`) — confirm the interfaces already exist or add thin adapters.
- Streaming + the tool state machine are fairly complex; **non-streaming first, then streaming**.
- Keep the existing fallback / circuit breaker / metrics throughout.
- **Rollback**: `ToolPolicy.Enabled=false` reverts to single-turn RAG (zero behavior change).

## Acceptance Criteria

- The service agent can complete multi-step scenarios such as "look up ticket → answer" via tools.
- When `maxSteps` triggers, terminate safely and give a degraded reply.
- Streaming answers render word by word on the WS client.
- The existing AI unit / integration tests stay green; new loop test coverage added.

## Effort

2–3 weeks (consistent with the review).
