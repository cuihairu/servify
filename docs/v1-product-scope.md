# V1 Product Convergence: Web Standalone-site Intelligent Customer Service

This document defines Servify's first-version product goal. It primarily constrains product boundaries and documentation narrative; it does not replace a concrete implementation backlog.

## Goal in One Sentence

V1 first serves one clear scenario:

> Embed a Web intelligent customer service entry into a corporate website, brand standalone site, SaaS site, or docs site, so visitors can ask questions instantly, the AI answers first based on site knowledge, escalates to a human when necessary, and settles complex issues into tickets for continued handling.

This means the first version is an independently deployable, Web-first, AI-collaborative intelligent customer service product — not a platformized customer service middle platform or an omnichannel contact center.

## Design Judgments After the AI Updates

As of 2026-06-19, OpenAI's latest model guide lists `gpt-5.5` as the latest model and recommends the Responses API for reasoning, tool calling, and multi-turn tasks. The official capabilities continue to emphasize structured output, prompt caching, hosted tools, file search, the Agents SDK's orchestration, handoff, guardrails, and tracing capabilities.

These updates will not turn Servify into a "more complex Agent platform". The practical impact is:

- V1's AI layer should be thinner: let the model carry stronger understanding, rewriting, summarization, and tool selection, while the business system keeps only clear contracts.
- V1's output should be more structured: intent recognition, whether to escalate, knowledge sources, confidence, and ticket summaries should go through structured results wherever possible rather than relying on natural-language parsing.
- V1's knowledge chain should be more replaceable: the capability shapes of OpenAI file search / hosted tools can be referenced, but Servify's core should still keep the `KnowledgeProvider` abstraction, serving independent deployment and private knowledge bases.
- V1 does not need multi-Agent workflows first: the handoff, guardrails, and tracing ideas of the Agents SDK are worth borrowing, but the first-version product should not make a "multi-Agent orchestration platform" a user-visible capability.
- V1 should control cost and latency: visitor first answers prioritize low latency and short responses; complex summarization, escalation suggestions, and ticket consolidation use higher reasoning or stronger models.
- V1's prompts should be shorter and more outcome-first: no more stacking lengthy process prompts; instead make the goal, boundaries, available evidence, and output format explicit.

References:

- [OpenAI latest model guide](https://developers.openai.com/api/docs/guides/latest-model.md)
- [Migrate to the Responses API](https://developers.openai.com/api/docs/guides/migrate-to-responses)
- [Structured Outputs](https://developers.openai.com/api/docs/guides/structured-outputs)
- [File search](https://developers.openai.com/api/docs/guides/tools-file-search)
- [Prompt guidance](https://developers.openai.com/api/docs/guides/prompt-guidance)
- [Agents SDK](https://developers.openai.com/api/docs/guides/agents)

## V1 Recommended AI Runtime Mode

The more reasonable AI design for the first version is "one main orchestrator + a few explicit tools + structured output", not a "multi-Agent orchestration platform":

1. One main conversation orchestrator is responsible for understanding visitor questions, retrieving knowledge, calling necessary tools, and producing structured decisions.
2. The tool set stays very small:
   - Retrieve knowledge
   - Query the current conversation/customer context
   - Create or suggest creating a ticket
   - Request human takeover
3. Model output is structured wherever possible, at least including:
   - `answer`
   - `citations`
   - `confidence`
   - `next_action`
   - `handoff_reason`
   - `ticket_summary`
4. `next_action` keeps only a few stable states:
   - `answer`
   - `clarify`
   - `handoff`
   - `ticket`
5. Visitor first answers prioritize lower reasoning cost; raise reasoning when summarization, attribution, or long-text consolidation is needed.

This design fits V1 better than "multiple specialized agents handing off to each other", because it:

- Is easier to debug and evaluate
- Is easier to make observable and auditable
- Is easier to control on latency and cost
- Better matches the high-frequency short-conversation shape of website customer service

## Core Capabilities Kept in V1

### 1. Web Embedding Entry

The goal is to let a standalone site integrate at minimal cost:

- One script or SDK configuration mounts the customer service entry
- Supports visitor conversations, message send/receive, real-time state, basic visitor information
- Supports page-context passthrough, e.g. current URL, page title, language, entry source
- Supports basic brand configuration, e.g. name, welcome message, theme color, offline notice
- Supports public configuration and a public knowledge entry, but must not expose management capabilities

### 2. AI First Answer and Knowledge Q&A

The AI's role in V1 is a "customer service collaboration assistant", not a fully autonomous business executor:

- Answer product, usage, after-sales, and documentation questions from the knowledge base
- Clarify first when uncertain, rather than fabricating answers
- Answers should carry traceable sources, or at least keep source references visible to the customer service side
- Give escalation-to-human suggestions for questions that clearly need humans
- Route sensitive, over-privileged, or ungrounded questions through refusal or degradation strategies
- Generate agent-facing summaries for long conversations, instead of making a human read the entire history from scratch

### 3. Knowledge Base Management

V1 needs to make the knowledge base a core asset of customer service first answers:

- Support document create, update, and delete (CRUD and index task management are already exposed over HTTP today: `GET/POST /:id/index-jobs`, `POST /index-jobs/:job_id/retry`, including source registration; search preview is still a P1 enhancement)
- Support site FAQ, product documentation, help docs, after-sales policy, and other content retention
- Support replaceable providers; do not bind core logic to Dify, WeKnora, pgvector, or a specific model vendor
- Support observability information for index status, retrieval hits, and fallback situations

### 4. Human Takeover and Agent Workspace

Problems the AI cannot solve must be able to return to humans:

- Agents can view conversation context and message history
- Agents can take over the current conversation and continue replying
- Support transfer to other agents or queues
- Support basic online status, load, and assignment strategies
- The AI should generate summaries and suggestions for agents to see, rather than implicitly executing high-risk actions on the agent's behalf

### 5. Ticket Full Lifecycle

V1's "problem solved" must not stop at the chat window:

- Conversations can be escalated or settled into tickets
- Tickets retain customer, conversation, AI summary, assignee, status, and comments
- Agents can continue following up, closing, and reviewing the associated conversation
- The back office can see basic statistics and processing state

### 6. Back-office Operations and Security Baseline

The first-version back office keeps only delivery-necessary capabilities:

- Basic management of agents, customers, conversations, knowledge bases, and tickets
- Login, permissions, token invalidation, audit logs
- AI provider, knowledge provider, fallback, health check, and metrics entry points
- A clear dev/demo/mock versus production configuration boundary

## Capability Priority

To prevent V1 from diverging again, capabilities are converged into the three layers below.

### P0 Must Deliver

- Web widget / SDK integration
- Visitor conversations and messages
- AI knowledge Q&A with source citations
- Human takeover, transfer, close
- Ticket creation, follow-up, close
- Knowledge base management and indexing
- Baseline back-office security and auditing

### P1 Can Enhance

- Conversation summaries, agent suggestions, auto-tagging
- Better visitor information collection and page-context utilization
- A clearer assistance-state UI
- Basic operations analytics, e.g. deflection, handoff rate, top unanswered questions

### P2 Not Included in V1 Yet

- Multi-channel
- Voice
- Multi-Agent
- Full remote assistance workspace
- Platformized tenant capabilities
- Deep business-system integration marketplace

## Features Not Done in V1

The capabilities below may keep architecture reservations, but must not become first-version product promises:

- Multi-tenant SaaS platform, plan billing, tenant self-provisioning
- Multi-channel integration for WhatsApp, WeChat, Telegram, App, phone, SIP
- Voice bots, call centers, recording transcription production chains
- Full co-browsing, screen sharing, remote-control workbench
- Multi-Agent autonomous workflows, complex task planning, cross-system automatic execution
- A standard integration marketplace for deep business systems like CRM, orders, payments, and logistics
- A provider marketplace or a developer-facing plugin platform

These are not "never"; in scheduling they must come after the Web standalone-site customer service main chain.

## Remote Assistance's Place in V1

Remote assistance can still be kept as a differentiator, but in V1 it must not outweigh the website intelligent customer service main chain:

- Can be expressed as "in-conversation assistance, guidance, joint troubleshooting, and continuous takeover"
- Can keep the WebSocket / WebRTC runtime, connection states, and later extension entry points
- Must not promise a delivered full remote-control or co-browsing product

In other words, V1 first makes "Web integration → AI first answer → human takeover → transfer collaboration → ticket full lifecycle" a stable product, then enhances remote assistance into a standalone workspace.

## V1 Success Criteria

The first version can be judged converged against these criteria:

- A standalone site can start a real conversation through the Web widget.
- The AI can answer common questions from the knowledge base and degrades explicitly when ungrounded.
- Agents can take over, reply, transfer, and close within the same conversation.
- Conversations can settle into tickets; tickets can be followed up and closed.
- Administrators can maintain knowledge, agents, basic configuration, security, and runtime state.
- Production configuration does not rely on implicit mock / in-memory / legacy capabilities pretending to deliver.
- README, docs site, website, and acceptance checklist all express this chain, no longer writing later extensions as current promises.

## Documentation Convergence Rules

When organizing documentation later, judge in this order:

1. Read this document first to confirm whether a capability belongs to V1.
2. Content belonging to V1 goes into README, docs homepage, deployment, acceptance, and delivery priorities.
3. Content not in V1 but with architectural value goes into architecture or implementation backlog.
4. Content not in V1 and without real implementation evidence can only be a future direction, never written as a delivered capability.
