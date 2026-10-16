# Servify Delivery Priorities

This document is not responsible for listing the full implementation backlog; it answers a more direct question:

In the current repository, what is most worth doing first to move from "code exists" to "deliverable".

Its relationship to [acceptance-checklist.md](./acceptance-checklist.md):

- `acceptance-checklist.md` is responsible for recording acceptance evidence and status of each capability
- This document is responsible for defining the current stage's execution order, trade-off principles, and recovery entry points

## Current Judgment

The current first-version product goal has converged on [Web standalone-site intelligent customer service](./v1-product-scope.md): Web widget access, AI first answer based on the knowledge base, human handover, transfer collaboration, full ticket workflow, and necessary back-office operations.

The three closure risks listed in the old version of this document have all been handled:

1. The production path `inmemory` / `mock` / `legacy` compatibility implementations are closed out (P0 batch; the `services` directory was removed wholesale).
2. Main-chain acceptance closure is complete (P1 batch + V1.0 convergence B1–B4 through the gate; see [acceptance-checklist.md](./acceptance-checklist.md)).
3. Documentation/todo/implementation drift has been governed through the V1.0 convergence B0 document declaration batch and the documentation reconciliation rules.

Trade-offs for follow-up evolution are executed by "evolution direction": the Routing scoring engine, pre-close ticket interception, and so on — without cutting in line to expand already frozen surfaces (voice, multi-channel).

## Execution Order

> The P0/P1/P2/P3 order below is the historical execution sequence, fully executed, and is kept only as methodology archive; the current active backlog is in [todo.md](../todo.md).

### P0 First Close Out Runtime Hard Defects (Complete)

The highest priority is anything that directly affects the runtime boundary and deployment credibility:

1. Event bus durability boundary
2. Multi-instance boundary for agent presence / load / assignment
3. The reality of configuration, startup, health checks, and dependency assembly
4. Whether the mock / disabled / compatibility implementations have a clear production boundary

### P1 Then Close Out Main-Chain Acceptance (Complete)

Before P0 keeps expanding the risk, the next step is to push the main chain from "code and tests basically there" to "evidence exists that it is deliverable":

1. AI / Knowledge main chain
2. Auth self-service session chain
3. Session workspace main operations
4. Other high-value chains still in `部分通过 / 未验 / 阻塞` (partially passed / unverified / blocked)

### P2/P3 Finally the Enhancements (Complete)

Enterprise-grade enhancements, capability expansion, and further product surface rollout must be built on the premise that the first two layers are already stable.

For example:

- New provider extensions
- More complex multi-instance governance
- A more complete remote assistance productized workspace
- Capability surface expansion of SDK / channel / voice
- Multi-agent autonomous workflows or platform-oriented extensions for developers

## Current Priority Tasks

The V1.0 convergence batch (B0–B4) has all passed the gate; there are no in-progress priority tasks. Entry points when resuming work:

1. Evolution direction backlog: the Routing scoring engine, pre-close ticket interception rules, remaining thin-shell module documentation surface closure items.
2. When implementing, register and gate batches per [todo.md](../todo.md)'s batch discipline.

If resuming after an interruption, use the most recent `[-]` item in [todo.md](../todo.md) as the anchor, and do not jump around from memory.

## Trade-off Principles

When the following kinds of conflict appear, decide uniformly by these principles:

1. Prefer fixing a "delivery boundary error" over continuing to add "more feature entry points".
2. Prefer adding real runtime evidence over only adding unit tests.
3. Prefer aligning documentation with implementation over maintaining an optimistic state.
4. When a limitation can be clearly declared, do not disguise it as supported enterprise capability.

## Companion Documents

1. [todo.md](../todo.md)
2. [acceptance-checklist.md](./acceptance-checklist.md)
3. [implementation/README.md](/zh/implementation/README.md)
4. [demo-and-mock-boundaries.md](./demo-and-mock-boundaries.md)
5. [operator-runbook.md](./operator-runbook.md)
