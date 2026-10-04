// Package domain contains AI domain concepts and orchestration-oriented types.
//
// AI serves customer service, not the AI itself (V1.0 convergence, see
// docs/v1-convergence-plan.md §5): one orchestrator plus a small set of
// tools (knowledge retrieval, session/customer context, ticket creation,
// human handoff) with structured output. It must not grow into an
// agent framework (no planner / MCP / multi-agent topics in V1).
package domain