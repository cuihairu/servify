> **English** | [中文](ai-fallback-behavior.md)

# AI Fallback Behavior

The current fallback rules of the AI orchestration layer are as follows:

1. When the input hits a guardrail, the request is rejected outright and counted in the fallback metric.
2. When knowledge retrieval fails, `QueryOrchestrator` returns an error; the upper layer `OrchestratedEnhancedAIService` switches to the legacy fallback (always on, with no configuration switch; the `fallback.enabled` configuration applies to knowledge-source-level degradation, see `internal/app/server/knowledge_source.go`).
3. When the LLM provider fails, `OrchestratedEnhancedAIService` also takes the fallback path.
4. When retrieval succeeds but there are no sources, the LLM is still allowed to answer normally, and the response strategy is marked `llm` (the prom metric records `primary`); `fallback` refers specifically to orchestration failure falling back to the legacy path.

This behavior document describes the current provider switching and failure-degradation boundaries; it does not freeze implementation details. If retrieval soft-degrade is introduced later, this document and the test matrix should be updated in sync.
