# Knowledge Base Selection Landscape and Adaptation Planning

> Status: research and planning draft (2026-09-20). What actually landed is governed by the selection decisions.
> Background: Dify is strictly speaking an LLM application orchestration platform, and the knowledge base (dataset) is only an attached capability — adapting Dify back then as a "knowledge base plugin" (the `difykp` driver) was imprecise positioning. This document inventories the current mainstream knowledge base products, evaluates their adaptation cost against the servify knowledge source contract, and gives recommendations for the road ahead.

## 1. Current State of the servify Knowledge Source Contract

The unified contract lives at `apps/server/internal/platform/knowledgeprovider/provider.go`:

| Operation | Semantics |
| --- | --- |
| `Search` | Retrieval: request → hit list (with scores / citations) |
| `UpsertDocument` | Document write/update; returns the external document ID |
| `DeleteDocument` | Delete by ID |
| `HealthCheck` | Availability probe (the basis for startup-time require / degradation decisions) |
| `RebuildIndex` (optional extension) | Index rebuild |

Seven existing driver implementations (under `apps/server/internal/platform/knowledgeprovider/`):

| Driver | Adaptation target | Positioning assessment |
| --- | --- | --- |
| `ragflowkp` | RAGFlow (InfiniFlow's open-source knowledge base, anchored at v0.27.x) | **A legitimate knowledge base**, one of the primary directions: the deepest document parsing, a complete REST API; tenant scoping fully aligned with dify/weknora, see `scripts/test-ragflow-acceptance.sh` |
| `weknorakp` | WeKnora (Tencent's open-source knowledge base) | **A legitimate knowledge base**, one of the primary directions, see `WEKNORA_INTEGRATION.md` |
| `pgvector` | In-process pgvector (DB + embedding direct setup) | The lightweight self-hosted default path, no external dependencies |
| `difykp` | Dify dataset API | **Imprecise positioning**: Dify is an orchestration platform and the dataset is an attached feature. Kept as a compatibility path; not fit to be the primary knowledge base recommendation |
| `local` | In-process local knowledge engine (local embedder + cosine) | A zero-dependency offline path, enabled by explicit declaration via `knowledge.provider=local` (not a test stub; has its own `local-knowledge` acceptance) |
| `memory` | In-process in-memory implementation | Fully implements the contract but is not wired into production assembly; referenced only by provider-switch tests and acceptance scripts |
| `mock` | Controllable mock implementation (can inject search/delete/health errors) | Referenced by tests only; used for contract regression and as the acceptance counterpart in mock mode |

The selection chain is converged by the `selectKnowledgeSource` facade shared by startup and request level (cut 23 + the RAGFlow kickoff cut): ragflow (health check, degradable) → dify (health check, degradable) → weknora (health check). **pgvector / local are not in the selection chain**: both are global declarations (`knowledge.provider=pgvector|local`) + in-process drivers, handled by the pass-through branches of `BuildAIAssembly` and the request-level `ai_scoped_handler` before the selection chain (`knowledge_source.go:60-62` states explicitly "pgvector does not enter this function: it is a global declaration + an in-process driver, and the caller passes through to the instance assembled at startup"). Adding a new external driver only requires implementing the contract and wiring it into this facade; the selection priority and assembly are existing code paths. The require semantics when the RAGFlow health check fails: it fails immediately only when both dify and weknora are disabled, otherwise it degrades along the chain.

## 2. Selection Dimensions

1. **API completeness**: whether the REST API covers the contract's four operations (retrieval / upload / delete / health check) — this determines driver adaptation cost
2. **Deployment form**: self-hosting (Docker Compose / K8s) fits servify's private-delivery model; SaaS-only is an instant veto
3. **Document parsing capability**: how well complex PDFs / contracts / tables are parsed directly caps retrieval quality
4. **Retrieval capability**: hybrid retrieval (vector + full text), rerank, citation tracing
5. **Tenant / permission model**: multi-tenant knowledge isolation capability (aligned with servify's tenant/workspace scope governance direction)
6. **License and community**: open-source license friendliness, activity, long-term maintenance risk
7. **Resource footprint**: GPU/CPU requirements of the embedding/parsing services

## 3. Inventory of Mainstream Knowledge Base Products (2026-09)

Synthesized from community comparisons and official documentation (sources listed at the end):

### 3.1 RAGFlow (InfiniFlow) — Project Initiated (First-Choice Candidate Delivered)

- **Status**: the driver has landed (`ragflowkp`), version anchored at v0.27.x; highest priority in the selection chain (ragflow → dify → weknora; pgvector direct setup is not in the selection chain, it is a pass-through global declaration via `knowledge.provider=pgvector`); tenant/workspace scoping fully aligned with dify/weknora
- **Positioning**: an open-source enterprise-grade RAG engine, 18k+ GitHub stars, known for DeepDoc deep document understanding (complex PDF/table/layout parsing) and retrieval precision
- **API**: a complete RESTful API (knowledge base creation, document upload, retrieval, etc., API Key auth); the contract's four operations map one to one
- **Deployment**: self-hosted with Docker Compose, fits private delivery
- **Adaptation assessment**: the most direct four-operation mapping; a high retrieval quality ceiling; resource footprint on the heavy side (the parsing service is fairly heavy)
- **Risk**: fast release cadence (currently 0.x); API stability is guarded by version anchoring + the acceptance script (`test-ragflow-acceptance.sh`, dual mock/real modes)

### 3.2 MaxKB (Fit2Cloud / 1Panel) — Second-Choice Candidate

- **Positioning**: oriented toward enterprise knowledge services and business rollout; out of the box, embeds into third-party systems with zero code
- **API**: supports document upload / online document crawling, automatic splitting and vectorization, RAG retrieval; the API is mostly oriented to Q&A scenarios
- **Deployment**: self-hosted with Docker, modest resource requirements
- **Adaptation assessment**: quick to roll out and light to operate; but the API centers on "Q&A", the `Search` contract has to map onto its retrieval interface, and the fine-grained document management API surface is weak
- **Risk**: deep document parsing is weaker than RAGFlow's

### 3.3 FastGPT (labring)

- **Positioning**: easy-mode RAG + workflow orchestration; upload auto-splits/embeds/indexes; the fastest to pick up
- **Adaptation assessment**: it belongs to the same "orchestration platform with a knowledge base" shape as Dify — under the "Dify is imprecisely positioned" convention, FastGPT as a knowledge base plugin has the same positioning problem; better kept as a reference than a primary recommendation
- **Risk**: redundant positioning (servify's existing orchestration-layer needs are carried by the AI module itself)

### 3.4 QAnything (NetEase Youdao)

- **Positioning**: lightweight local knowledge base Q&A, the "Ziyan" model + two-stage retrieval
- **Adaptation assessment**: lightweight and easy to deploy; community activity has slowed in recent years, and long-term maintenance risk needs follow-up confirmation
- **Risk**: maintenance cadence and multi-tenant capability remain unverified

### 3.5 AnythingLLM

- **Positioning**: personal / small-team local knowledge base, lightweight deployment
- **Adaptation assessment**: aimed at personal scenarios; enterprise-grade multi-tenant / permission / audit capabilities do not match servify's delivery scenarios — not recommended

### 3.6 Dify — Positioning Clarified (Existing Compatibility Retained)

- **Positioning**: an LLM application orchestration platform (workflow / Agent / app publishing); the knowledge base is an attached subsystem
- **Adaptation assessment**: the existing `difykp` adapts the dataset API to the unified contract and is retained for compatibility; but its dataset capability (parsing, retrieval configuration) is weaker than a dedicated knowledge base, and depending on an orchestration platform as your knowledge base widens the coupling surface (upgrading the orchestration platform can spill into knowledge retrieval)
- **Recommendation**: the documentation positioning is corrected from "knowledge base plugin" to "orchestration platform integration (dataset compatibility included)"; not recommended by default for new customers

### 3.7 WeKnora (Tencent) — Adapted

- A complete driver and integration documentation already exist (`WEKNORA_INTEGRATION.md`); positioned as one of the primary legitimate-knowledge-base directions
- Differentiation from RAGFlow: WeKnora is lighter to deploy and integrates well with the Chinese ecosystem; RAGFlow has deeper document parsing. The two can coexist as a dual path

### 3.8 The Vector Database Layer (Not a Knowledge Base, but Related)

- pgvector (already adapted, in-process direct setup), Milvus, Qdrant, Weaviate, and Chroma are the storage/retrieval layer beneath "knowledge base engines"
- servify's `pgvector` driver takes the "build your own retrieval layer" route (DB + embedding direct setup), suited to the zero-dependency default path; dedicated knowledge base products take over parsing / chunking / rerank as a bundle

## 4. Adaptation Assessment Matrix

| Dimension | RAGFlow | MaxKB | WeKnora (existing) | Dify (existing) | pgvector (existing) |
| --- | --- | --- | --- | --- | --- |
| Contract four-operation API mapping | Complete | Q&A-centric; weak document management surface | Complete | dataset API (attached capability) | Direct setup (in-process implementation) |
| Deep document parsing | Strong (DeepDoc) | Medium | Medium | Medium | Depends on self-built chunking |
| Deployment form | Self-hosted on Docker | Self-hosted on Docker | Self-hosted on Docker | Self-hosted on Docker | No external dependencies |
| Multi-tenant isolation | Knowledge-base level | Knowledge-base level | Knowledge-base level | dataset level | Carried by the servify data layer |
| Resource footprint | On the high side | Medium | Medium | Medium-high | Lowest |
| Positioning rigor | Knowledge base | Knowledge base | Knowledge base | Orchestration platform (attached) | Self-built retrieval layer |

> API endpoint details are governed by hands-on testing against each product's official documentation; before integration, spike-verify each contract operation item by item.

## 5. Recommended Route

1. **The RAGFlow driver has been initiated and landed**: the most complete REST API, the deepest document parsing, and a self-hosting fit — one of the primary knowledge base directions (forming a dual path with WeKnora: RAGFlow for deep parsing scenarios, WeKnora for the lightweight Chinese ecosystem); follow-up investment shifts to real-environment integration and version tracking (anchored at v0.27.x)
2. **MaxKB as a watched alternative**: integrate on demand when a customer already runs MaxKB; not a default recommendation
3. **Dify demoted in positioning**: the documentation and product convention is corrected from "knowledge base plugin" to "orchestration platform integration (dataset compatibility retained)"; the dify branch in the `selectKnowledgeSource` facade stays for compatibility, with no new capability investment
4. **pgvector stays the default lightweight path**: the default choice for zero-dependency scenarios is unchanged. The new-customer delivery convention has been signed off (2026-09-21): **start with pgvector (zero external configuration, the full chain runs in the first delivery week); when the customer has knowledge base operational needs, enhance with WeKnora (lightweight, Chinese) or RAGFlow (heavy parsing) by parsing depth** — see deployment.md's "default knowledge base convention for new-customer delivery" for the full convention

## 6. Implementation Path (the New-Driver Pattern)

New drivers follow the existing `ragflowkp` / `difykp` / `weknorakp` pattern:

1. `platform/knowledgeprovider/<driver>/`: `Provider` implements the four operations + `HealthCheck` (HTTP client + API Key auth; HTTP details go in `pkg/<driver>/`, with a mock interface seam at the driver layer)
2. `mapping.go`: map external hit/document models → `KnowledgeHit` / `KnowledgeDocument`
3. Add a `config` block (BaseURL / API Key / Timeout), aligned with the secure-config allowlist gates (a new gate must change three allowlist tests)
4. Wire into `configscope` tenant/workspace scoping (resolver + gorm provider/store columns + a versioned migration, modeled on `000009_ragflow_scoped_config`)
5. Wire into the selection chain via the `selectKnowledgeSource` facade (health check / degradation semantics follow the current contract)
6. Sync swag annotations and documentation; align the acceptance script with the `test-ragflow-acceptance.sh` pattern (mock mode is fully self-contained and CI-runnable; real mode rejects private networks)

## 7. Reference Sources

- [MaxKB vs RAGFlow enterprise platform comparison](https://www.maxkb.cn/maxkb-vs-ragflow)
- [In-depth comparison of 5 open-source RAG platforms (Zhihu)](https://zhuanlan.zhihu.com/p/1921226488444880527)
- [In-depth comparison of five AI application frameworks: Dify, FastGPT, RagFlow (Baidu AI Cloud)](https://cloud.baidu.com/article/4510271)
- [RAGFlow, Dify, FastGPT, Cherry Studio, AnythingLLM compared and deployed (Juejin)](https://juejin.cn/post/7520286373594234880)
- [RAG + AI workflows + Agents: a comprehensive comparison of LLM frameworks (Tencent Cloud)](https://cloud.tencent.com/developer/article/2441799)
- [The most detailed comparison of open-source knowledge base RAG projects (QAnything/RAGFlow/Dify/FastGPT etc.)](https://hustyichi.github.io/2024/07/08/compare)
- [RAGFlow HTTP API official documentation](https://ragflow.io)
- [Open-source RAG framework roundup (Volcengine)](https://developer.volcengine.com/articles/7392432442768326667)
