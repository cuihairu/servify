> **English** | [中文](ai-observability-policy.md)

# AI Observability And Policy Hooks

The current AI orchestration layer has reserved the following control points:

- tracing span tags: `ai.task_type`, `ai.retrieval.enabled`, `ai.provider`, `ai.tokens.total`, `ai.latency.ms`
- module metrics: provider usage, provider error, policy rejection, last error category
- `PolicyHook`: can hook in content safety, tenant policy, refusal rules
- `PromptAuditRecorder`: can record prompt version, task type, message count, retrieval hit count

These interfaces are currently contract-first reservations, not bound to any external audit or policy system by default.
