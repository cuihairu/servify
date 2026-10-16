> **English** | [中文](architecture-review-2026.md)

# Architecture Assessment: AI / Agent Direction (2026-07)

> ⚠️ **Snapshot note (2026-10-08 documentation reconciliation)**: This document is a snapshot at the 2026-07 assessment point. Its core conclusions — "the AI layer = single-turn RAG, the agent-loop is not yet wired, `Handle` does not call `ToolExecutor`" — have been overturned by later batches: the agent loop (ToolPolicy multi-step tool loop + ChatStream streaming) was wired into the main path on 2026-10-04 (`modules/ai/application/query_orchestrator.go`, verification record at [13-ai-agent-loop.md](./implementation/13-ai-agent-loop.md)); `internal/services` has been removed wholesale with business capabilities converged into `internal/modules/*` (capped at 27 modules), and the platform packages now number 20. For the current authoritative architecture snapshot see [current-architecture.md](./current-architecture.md); this document is retained only as archived assessment history.

> Status: assessment document (no product code changed)
> Scope: Re-examine the Servify architecture in light of the frameworks that have appeared in 2025–2026, focusing on the **AI/Agent framework direction** and giving an architect's objective judgment.
> Companion reading: [Current Architecture Analysis](./current-architecture.md), [Architecture Redesign Plan](./architecture-redesign-plan.md), [V1 Product Scope](./v1-product-scope.md)

---

## TL;DR (Conclusions First)

1. **The Go modular monolith backend is healthy**: the business foundation (multi-tenancy, routing, sessions, voice, tickets, knowledge base) is solid — **do not rewrite it**.
2. **The real state of the AI layer: single-turn RAG**. tool-calling / agent-loop exist only as scaffolding and are **not yet wired**; the streaming interface is defined but unused in the main path. This is the largest "looks like it is there, actually is not" capability gap.
3. **The real product gap**: to let "intelligent customer service" genuinely act over multiple steps (look up a ticket, look up a customer, hand off to a human, ask a follow-up), a **real agent loop** is needed — not more RAG.
4. **Language choice**: TS (Vercel AI SDK / Mastra / LangGraph.js) iterates fastest and has the broadest ecosystem; but the Go side already has **Eino** (ByteDance, "LangGraph for Go", production-validated) and LangChainGo — **mature agent capability is obtainable inside Go, so a TS service is not mandatory**. This is an important correction relative to the first draft of this assessment.
5. **Recommended incremental roadmap** (ordered by architecture impact, smallest first):
   - **Step 0 (optional)**: introduce a **LiteLLM** model gateway to unify multi-model / failover / cost / rate limiting, leaving the existing Go architecture almost untouched.
   - **P0 short-term**: wire the agent loop inside Go (build a minimal loop in-house or bring in Eino; the scaffolding is already in place, 2–3 weeks). See [P0 Implementation Plan](./implementation/13-ai-agent-loop.md).
   - **P1 mid-term**: only if Go-side capability cannot keep up, peel AI orchestration out into a TS service (Vercel AI SDK preferred; Mastra deferred due to supply-chain risk).
   - **P2 edge**: the marketing site is already on Cloudflare and can carry a lightweight edge gateway; **do not** migrate the full stack to Workers.
6. **Not recommended**: a wholesale rewrite, a full-stack move to Cloudflare (the WebRTC / voice / multi-tenant PG cost is too high), and building a complete agent framework in-house while chasing new releases long-term.

---

## 1. Current Architecture (One Page)

| Layer | Current state |
|---|---|
| Backend | Go 1.26, Gin + GORM + Postgres(pgvector) + Redis + gorilla/WS + pion/WebRTC; **modular monolith**, converging from `handlers→services→models` to `modules/*/{domain,application,infra,delivery}` |
| Platform abstraction | 16 platform packages: llm / embedding / knowledgeprovider / aiprovider / realtime / sip … (Ports & Adapters, done quite cleanly) |
| AI orchestration | `modules/ai/application`: QueryOrchestrator + Retriever + PromptBuilder + Guardrails + ToolRegistry/Executor + policy/guardrails/metrics |
| Admin frontend | UmiJS Max + Ant Design Pro (pnpm) |
| SDK | TS workspace (core + react/vue/vanilla), Vite, WebSocket-first |
| Marketing site | Static site, deployed on a Cloudflare Worker (`servify.cuihairu.site`) |
| Docs | VitePress → GitHub Pages |
| Observability | OpenTelemetry(tracing) + Prometheus(metrics) + logrus |

The foundation is good. The problem is not "a rotten architecture" but that **AI capability stops at RAG and has not reached the agent stage**.

---

## 2. The Real State of the AI Layer (Core Technical Findings)

The real call path read from `apps/server/internal/modules/ai/application/query_orchestrator.go::Handle`:

```
policy hooks → guardrails.ValidateInput
  → retriever.Retrieve (单次向量检索 RAG)
  → promptBuilder.Build (system + 历史 + knowledge 拼接)
  → llmProvider.Chat (单次、非流式)
  → guardrails.SanitizeOutput
  → return AIResponse
```

**Key facts**:

- **No agent loop**. There is no `for { ... tool calls ... }` anywhere in the path. `ToolRegistry` / `ToolExecutor` / `tool_customer_lookup` / `tool_ticket_lookup` / `tool_handoff` are all implemented, but **`Handle` does not call them at all**.
- The `LLMProvider` interface already supports `ChatStream`, `ToolDefinition`, and returning `ToolCalls` (all three adapters — OpenAI / Hypetic / Mock — are present) — **the capabilities are all reserved; they just are not powered on**.
- `PromptBuilder` is plain: it concatenates three segments — system / history / knowledge — with **no token budget / context window management / re-ranking / query rewrite**.
- The multi-provider abstraction for Embedding (OpenAI / TEI / Xinference) and Knowledge (pgvector / Dify / WeKnora) **is done very well** and is the most mature part of the AI layer.

**In one sentence**: this is "a well-structured single-turn RAG plus a set of unwired agent scaffolding". What stands between it and real intelligent customer service is an agent loop, not more retrieval.

---

## 3. A Quick Look at the 2025–2026 Framework Landscape

| Framework | Positioning | Fit with Servify |
|---|---|---|
| **Vercel AI SDK v5/v6** | The most mature agent SDK in the TS ecosystem: `stopWhen` agent loop, `ToolLoopAgent`, typed SSE streaming, `toolApproval` (HITL), `prepareStep`, dynamic tools, v6 unified generateText/Object | ⭐⭐⭐⭐⭐ First choice if going with a TS orchestration layer |
| **Cloudflare Agents SDK + Durable Objects + Workers AI** | Stateful edge agents: each agent has its own state / SQLite, built-in WS / scheduling / MCP, Workers AI edge inference for 50+ models, 18 products announced at Agents Week 2026 | ⭐⭐⭐ Suited to new projects / edge-first; migrating the existing Go backend is costly |
| **Mastra** | TS-native: workflow + agent + RAG + eval + dev studio, good DX | ⭐⭐⭐ Feature-complete; but in 2026-06 the `@mastra` npm organization was supply-chain poisoned (140+ packages), so security-sensitive scenarios need dependency pinning / provenance alongside it |
| **LangGraph (Py/TS)** | Graph-state-machine agents; 1.0 (2025-10) headlines durable execution (resumption from breakpoints), with high enterprise adoption | ⭐⭐⭐ Valuable for complex long-running ticket flows, but it introduces a Python stack |
| **Go: Eino (ByteDance) / LangChainGo** | Eino = "LangGraph for Go": orchestration + ADK + workflow visualization, production-validated at ByteDance; LangChainGo is lighter | ⭐⭐⭐⭐ **Best same-process fit** — LangGraph-grade capability obtained inside Go |
| **Model gateway LiteLLM** | Language-agnostic, self-hosted, unifies multi-model + failover + cost / rate limiting | ⭐⭐⭐⭐ **Most certain ROI, smallest impact** — viable as a standalone first step |

**Reference sources**:

- [Vercel AI SDK 5](https://vercel.com/blog/ai-sdk-5) · [AI SDK 6](https://vercel.com/blog/ai-sdk-6) · [Tool Calling](https://ai-sdk.dev/docs/ai-sdk-core/tools-and-tool-calling) · [Loop Control](https://ai-sdk.dev/docs/agents/loop-control)
- [Cloudflare Workers AI](https://www.cloudflare.com/products/workers-ai/) · [Durable Objects](https://developers.cloudflare.com/durable-objects/) · [Agents SDK](https://github.com/cloudflare/agents)

---

## 4. The Core Tension: Go Backend vs the TS Agent Ecosystem

- Business / concurrency / performance: **a Go strength**; there is no need to touch the modular monolith.
- Agent framework iteration speed / ecosystem breadth: **TS wins decisively**. Every new capability a Go implementation tracks (streaming protocol, HITL, eval, new model features) has to be built in-house, which is a long-term burden.
- The project already has a TS SDK team stack; using TS for the AI orchestration layer would **unify the language and type contract** with the SDK / frontend.

The tension is real, but it is not "pick one of two" — **AI orchestration** can be peeled out on its own into TS while business and data stay in Go.

---

## 5. Four Evolution Paths + Recommendation

### Path A: Wire the Agent Loop Inside Go (Recommended Short-Term)

Wire the existing `ToolExecutor` into `Handle`, implement a `stopWhen` equivalent of `while(toolCalls){ 执行→回灌→再 Chat }`, and connect `ChatStream` to the WebSocket.

- **Pros**: no language switch, 2–3 weeks in magnitude, makes the assistant "able to act" immediately, reuses all of the scaffolding.
- **Cons**: long-term you still have to track streaming / HITL / eval yourself.

### Path B: Split AI Orchestration into a Standalone TS Service (Recommended Mid-Term)

Extract `modules/ai` into a TS service (Vercel AI SDK or Mastra) that the Go backend calls over internal HTTP / gRPC; Go keeps business / data / routing / voice.

- **Pros**: access to the most mature agent capabilities, unified with the SDK, fast iteration, independently deployable / scalable.
- **Cons**: two runtimes, cross-service calls, and the team needs to know TS (the SDK stack already exists, so the bar is low).

### Path C: Full-Stack Migration to Cloudflare (Workers + DO + Agents SDK) ⚠️ Not Recommended

- Cost: WebRTC remote assistance (pion), PSTN / SIP voice, multi-tenant Postgres+pgvector, Redis sessions — under the Workers edge model these either cannot run or must be rebuilt.
- Suited to a "new project", not to "migrating an existing mature backend".
- **A worthwhile small step**: the marketing site is already on Cloudflare, so a lightweight edge gateway (auth / rate limiting / edge AI fallback) can be added alongside it.

### Path D: Model Routing Gateway (LiteLLM / Portkey) ➕ Supplementary, Not Core

Solves only multi-provider / cost / observability / degradation, **not agent capability**. It can accompany A / B but does not stand on its own.

---

## 6. Build vs Buy (Must Be Faced Directly)

If the goal is merely "to have a usable intelligent assistant", **Chatwoot + an AI extension** (open source) or a commercial customer-service SaaS would save a great deal of engineering effort. The price is **giving up product differentiation and control over the data**. Servify has already gone as far as building a modular monolith plus a multi-provider abstraction in-house; both the sunk cost and the differentiation requirement point toward "keep building in-house", but that is a strategic choice to be signed off by the decision-maker, not a technical one.

---

## 7. Recommended Incremental Roadmap (Three Phases)

| Phase | Action | Output | Magnitude |
|---|---|---|---|
| **Step 0 (optional)** | Introduce the LiteLLM model gateway to unify multi-model / failover / cost / rate limiting | Swappable models, lower cost, observability; the Go architecture barely moves | A few days |
| **P0 short-term** | Wire the agent loop inside Go: add a tool execution loop in `Handle` + connect `ChatStream` to WS; hook up the customer / ticket / handoff tools | The assistant can act over multiple steps and answer with streaming | 2–3 weeks |
| **P1 mid-term** | Peel AI orchestration out into a TS service (Vercel AI SDK / Mastra), called by Go over an internal API; add eval + HITL (human handoff approval) | Iteration speed ×N, unified type contract | 1–2 months |
| **P2 edge** | Add a lightweight Cloudflare gateway beside the marketing site; evaluate Workers AI for high-frequency fallback inference to cut cost | Low edge latency, reduced server load | On demand |

---

## 8. What Not to Touch

- A wholesale rewrite / language swap of the Go backend
- A full-stack move to Cloudflare Workers (the voice / WebRTC / multi-tenant PG cost is too high)
- Building a complete agent framework in-house and chasing new releases long-term
- Adopting a heavy framework such as LangGraph right now (over-engineering)

---

## Appendix: Optional Follow-Up Actions When Moving Forward

1. Produce a concrete implementation plan for **P0 (wiring the agent loop inside Go)**: which files change and how to connect the tool loop and streaming.
2. Produce a comparison implementation plan for **P1 (the TS orchestration service)**: boundary split plus technology selection (Vercel AI SDK vs Mastra).
3. Keep the status quo and retain only this assessment.

This document contains assessment conclusions only and no code changes.
