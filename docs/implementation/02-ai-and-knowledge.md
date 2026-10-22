# 02 AI And Knowledge

> Status (2026-10-04 doc reconciliation): the A1–A8 task lists are all complete (`[x]`). The V1.0
> convergence B3 additions — AI first-answer persistence (`ai_answers`), feedback submission
> (`POST /api/v1/ai/feedback` session binding), retrieval analytics
> (`GET /api/v1/ai/retrieval-analytics`) — and the Knowledge source registration / document
> versioning / index job HTTP surfaces are not part of this task package; status and evidence follow
> Acceptance Matrix §14. Tool-calling leftovers (Port/PermissionChecker wired as `nil` in production assembly): see [13-ai-agent-loop.md](./13-ai-agent-loop.md).

Scope:

- llm provider
- knowledge provider
- ai orchestrator
- prompt builder
- retriever
- tools
- metrics
- knowledge indexing

## A1 provider contracts

- [x] Create `internal/platform/llm`
- [x] Define `ChatRequest`
- [x] Define `ChatResponse`
- [x] Define `TokenUsage`
- [x] Define `LLMProvider`
- [x] Create `internal/platform/knowledgeprovider`
- [x] Define `SearchRequest`
- [x] Define `KnowledgeHit`
- [x] Define `KnowledgeDocument`
- [x] Define `KnowledgeProvider`

Acceptance:

- Provider interfaces do not expose OpenAI, Dify, or WeKnora compatibility private DTOs

## A2 provider adapters

- [x] Add `llm/openai/provider.go`
- [x] Migrate the model HTTP call logic
- [x] Add `llm/mock/provider.go`
- [x] Add `knowledgeprovider/dify/provider.go`
- [x] Wrap `pkg/dify`
- [x] Add `knowledgeprovider/weknora/provider.go`
- [x] Wrap `pkg/weknora`
- [x] Add `knowledgeprovider/mock/provider.go`

Acceptance:

- The AI module can run under the mock provider, and the runtime can be assembled with `Dify -> WeKnora compatibility -> fallback`

## A3 ai module skeleton

- [x] Create `internal/modules/ai/domain`
- [x] Create `internal/modules/ai/application`
- [x] Create `internal/modules/ai/infra`
- [x] Create `internal/modules/ai/delivery`
- [x] Define `AIRequest`
- [x] Define `AIResponse`
- [x] Define `TaskType`
- [x] Define `RetrievalPolicy`
- [x] Define `ToolPolicy`

Acceptance:

- The AI module exists independently

## A4 query orchestrator

- [x] Add `query_orchestrator.go`
- [x] Inject `LLMProvider`
- [x] Inject `KnowledgeProvider`
- [x] Implement the happy path
- [x] Implement the no-knowledge-retrieval fallback
- [x] Add orchestrator unit tests

Acceptance:

- It can replace the old enhanced AI main flow

## A4.1 runtime bridge

- [x] Add the runtime AI bridge, letting the old runtime switch to `QueryOrchestrator` incrementally
- [x] Let the old runtime entrypoints call the new `QueryOrchestrator`
- [x] Gradually replace the old AI service in the standard-mode entrypoint wiring
- [x] Gradually replace the old AI service in the enhanced-mode entrypoint wiring

Acceptance:

- The new AI module can be reused without changing the existing external handler / runtime interfaces

## A5 prompt and retrieval

- [x] Add `prompt_builder.go`
- [x] Split out the system prompt
- [x] Split out the context prompt
- [x] Split out the knowledge prompt
- [x] Add `retriever.go`
- [x] Support topK
- [x] Support threshold
- [x] Support strategy

Acceptance:

- Prompt and retrieval are separated out of the old service

## A6 tools

- [x] Define `Tool`
- [x] Define `ToolRegistry`
- [x] Define `ToolExecutor`
- [x] First batch:
  - [x] ticket lookup tool
  - [x] customer lookup tool
  - [x] handoff tool

Acceptance:

- Tool calls have schema and permission checks

## A7 ai guardrails and metrics

- [x] Input length limit
- [x] Sensitive-content interception placeholder
- [x] Output truncation
- [x] query_count
- [x] success_count
- [x] provider_usage_count
- [x] token_usage
- [x] latency
- [x] fallback_count

Acceptance:

- Metrics are reported uniformly by the orchestrator

## A8 knowledge module

- [x] Create `internal/modules/knowledge/domain`
- [x] Create `internal/modules/knowledge/application`
- [x] Create `internal/modules/knowledge/infra`
- [x] Create `internal/modules/knowledge/delivery`
- [x] Define `Document`
- [x] Define `DocumentRepository`
- [x] Define `IndexJobRepository`
- [x] Implement CreateDocument
- [x] Implement UpdateDocument
- [x] Implement DeleteDocument
- [x] Implement QueueIndexJob
- [x] Implement RunIndexJob

Acceptance:

- Knowledge documents and indexing become a formal module
