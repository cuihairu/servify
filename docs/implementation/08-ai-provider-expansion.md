# 08 AI Provider Expansion

Scope:

- LLM provider expansion
- Knowledge provider expansion
- Orchestration pipeline stability
- AI observability and security controls

## A1 provider-matrix

- [x] Inventory current `LLMProvider` and `KnowledgeProvider` contract gaps
- [x] Define the multi-provider configuration matrix
- [x] Define the priority, fallback, and circuit breaking strategy matrix
- [x] Build a declarative model for provider capabilities

Acceptance:

- Adding a provider no longer requires runtime if/else stitching

## A2 llm-provider-expansion

- [x] Add a second real `LLMProvider` adapter placeholder
- [x] Unify the streaming/non-streaming response contract
- [x] Unify the tool calling / function calling contract
- [x] Add provider-level timeout, retry, and error classification

Acceptance:

- The LLM provider no longer defaults to OpenAI as the only path

## A3 knowledge-provider-expansion

- [x] Add a second real `KnowledgeProvider` adapter placeholder
- [x] Abstract index, delete, rebuild, and retrieval consistency semantics
- [x] Define tenant / knowledge base multi-tenancy mapping rules
- [x] Add provider switching regression tests

Acceptance:

- The WeKnora compatibility provider becomes a replaceable implementation, not the only one

## A4 orchestration-hardening

- [x] Add a query orchestration scenario test matrix
- [x] Add a prompt builder contract test matrix
- [x] Add retrieval degradation path tests
- [x] Document AI fallback behavior

Acceptance:

- Orchestration layer behavior stays stable across provider switches

## A5 ai-observability-and-policy

- [x] Add provider-dimension tracing/span labels
- [x] Add token / latency / error classification metrics
- [x] Reserve content safety and policy refusal hooks
- [x] Reserve prompt/version audit record interfaces

Acceptance:

- AI capabilities have the basic observability and policy control points needed before going live
