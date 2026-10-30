# Knowledge Base Selection Policy (project-initiation criteria / selection-chain priority / new-customer default)

> Status: **decided on the user's behalf pending confirmation** (decided during the
> 2026-10-15 inspection; the user may overturn it). Corresponds to the todo item
> "Knowledge base selection decision: RAGFlow project-initiation criteria,
> selection-chain priority, new-customer default criteria (pending product
> confirmation)". This document consolidates the criteria scattered across
> `docs/KNOWLEDGE_BASE_LANDSCAPE.md` §3-§6 and `docs/deployment.md`
> ("new-customer delivery default knowledge base criteria (decided 2026-09-21)")
> into a single decision record. **The conclusion is consistent with the current
> code, zero code changes**; the only remaining gap is in §5.
>
> **2026-10-30 已代决复核（docs/review/2026-10-30-decisions.md 第 5 项）**:
> 维持既有代决——代码默认 `Knowledge.Provider: ""` 保持空、三外部 provider 默认
> `Enabled: false`,交付有条件时以 pgvector 起步;选择优先级维持
> `pgvector 直配 → local → ragflow → dify → weknora`;新增增强投入集中 RAGFlow,
> 维护 WeKnora,Dify 冻结新增能力,MaxKB 仅观察。**代码默认空 ≠ 开箱即用知识库**——
> 每个交付必须明确所选 provider 及其前提,不得把代码默认解释为已启用。不改代码默认、
> 不调整兼容顺序、不新增 MaxKB 接入、不扩展 Dify 新能力(冻结能力不停止修复兼容问题)。

## 1. Current State (measured 2026-10-15)

- Startup priority (`apps/server/internal/app/server/ai_runtime.go:91-106`):
  direct `pgvector` → `local` → external chain → no knowledge source.
  pgvector/local are globally declared and take precedence over the external chain
  (explicit in the `:89-95` comments).
- External selection chain (`app/server/knowledge_source.go:64-162`):
  **`ragflow → dify → weknora`**, with per-hop `Enabled` gating + optional health
  check
  + degradation; when `requireHealthy` is set and no source is available,
  startup fails.
- Code default: `Knowledge.Provider: ""` (`config/config.go:1287-1292`), the
  three external providers default to `Enabled: false` — a fresh install yields
  the "no knowledge source" base assembly; `config.yml` (dev) separately ships
  `knowledge.provider: "pgvector"`.
- Provider capability matrix: pgvector (self-hosted in-process, requires a pg
  extension + embedding), local (zero external dependencies, sqlite/postgres dual
  track), ragflow (external service, API anchored to v0.27.x), dify (the
  dataset side-capability of an orchestration platform), weknora (external
  service, the only `RebuildableProvider`).

## 2. Comparison Table

### 2.1 New-customer default criteria

| Option | Form | Pros | Cons |
| --- | --- | --- | --- |
| A. Change the code default to `pgvector` | Change `config.Default()` | A knowledge source on first launch | pgvector requires a pg extension + an embedding service, which directly breaks the sqlite deployment form; it implicitly introduces an external dependency, violating the "zero external configuration" criteria |
| B. Keep the code default `""`, and have the delivery template/docs declare `pgvector` | Current state | Safe default (no unexpected outbound calls); sqlite/evaluation forms remain usable; the decided criteria land on the delivery side | The delivery side must declare it proactively; the documentation criteria must follow delivery |
| C. Change the code default to `local` | Change `config.Default()` | Zero external dependencies, sqlite usable | Deviates from the 2026-09-21 decision (that decision was to start with pgvector); vector-retrieval quality is weaker than the external engines |

### 2.2 Selection-chain priority

| Option | Order | Pros | Cons |
| --- | --- | --- | --- |
| A. Current `ragflow → dify → weknora` | Code as-is | Consistent with LANDSCAPE §3.1 (RAGFlow first) / §3.6 (Dify as fallback); already locked by 7 selection-chain unit tests | Dify ranks before weknora (a historical-compatibility slot) |
| B. `ragflow → weknora → dify` | dify sunk to the bottom | More consistent with "Dify is positioned as fallback, with no new investment" | Changes 7 unit tests and three docs; dify is an existing compatibility path, and sinking it breaks existing customers' fallback expectations |

### 2.3 Project-initiation criteria

| Direction | Conclusion | Rationale |
| --- | --- | --- |
| RAGFlow | **Heavy re-parsing enhancement path, already initiated and landed** (driver + acceptance + scoped config) | LANDSCAPE §3.1: the most complete REST API, the deepest DeepDoc parsing, Docker self-hosting |
| WeKnora | **Lightweight Chinese-ecosystem path** (already adapted, one of the dual paths) | LANDSCAPE §3.7, §5.1 |
| Dify | **Retained for compatibility, no new investment** (positioning = orchestration platform; dataset is a side-capability) | LANDSCAPE §3.6, §5.3 |
| MaxKB | Watched as an alternative (integrate on demand when customers already have one) | LANDSCAPE §3.2, §5.2 |
| FastGPT / QAnything / AnythingLLM | Not initiated (overlapping positioning or not recommended) | LANDSCAPE §3.3-§3.5 |

## 3. Recommendation (decided on behalf)

1. **New-customer default criteria = Option B**: the code default stays `""`
   (the no-knowledge-source base), and "starting with pgvector" is carried as
   **delivery-side criteria** by the delivery template and `docs/deployment.md`;
   when a customer has knowledge-base operation needs, choose WeKnora
   (lightweight Chinese) or RAGFlow (heavy re-parsing) enhancement by parsing
   depth. Rationale: pgvector's prerequisites of a pg extension + embedding must
   not enter the implicit code default; the safe default (no knowledge source
   rather than unexpected outbound calls) takes priority.
2. **Selection-chain priority = Option A (the current order)**:
   `direct pgvector → local → ragflow → dify → weknora → no knowledge source`.
   dify staying ahead of weknora is an existing-compatibility slot and does not
   conflict with "no new investment".
3. **Project-initiation criteria = the recommended criteria**: RAGFlow is the only
   new investment direction (heavy re-parsing), the WeKnora dual path is
   maintained, Dify's capabilities are frozen, and MaxKB is watched.

## 4. Implementation Status

The recommended criteria are **fully consistent with the current code**, with zero
code changes this round; this document is the consolidation of the decision and
the reference anchors (the original criteria were scattered in two places:
LANDSCAPE §3-§6 and the decision paragraph of deployment.md).

## 5. Remaining Gap

**Real-environment acceptance evidence**: the real-mode end-to-end evidence for
Dify / WeKnora / RAGFlow still depends on external services and credentials (see
the todo item "real-mode knowledge base acceptance"; the script entry point
`*_ACCEPTANCE_MODE=real` is ready); the mock-mode evidence has already been filed
(`docs/acceptance-checklist.md:79-81,107-114`). **What is missing is external
service addresses and credentials, not in-repo work.**

## 6. How to Overturn

Overturning the criteria = editing this document + the decision paragraph of
`docs/deployment.md` + the note in `todo.md`;
only if options A/C are chosen (changing the code default) do `config.Default()`
and the acceptance baseline need to change.