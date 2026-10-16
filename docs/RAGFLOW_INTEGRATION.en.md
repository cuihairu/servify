> **English** | [中文](RAGFLOW_INTEGRATION.md)

# External Knowledge Base Integration Guide: RAGFlow Knowledge Provider (ragflowkp)

## Project Overview

RAGFlow (InfiniFlow's open-source RAG engine) is one of the primary directions for the servify knowledge base, positioned alongside WeKnora as the two "legitimate knowledge base" paths: **RAGFlow for deep document parsing scenarios** (DeepDoc parsing of complex PDFs / tables / layouts is its strength), **WeKnora for the lightweight Chinese ecosystem** (see [WEKNORA_INTEGRATION.md](WEKNORA_INTEGRATION.md)). Selection background and the full product landscape are in [KNOWLEDGE_BASE_LANDSCAPE.md](KNOWLEDGE_BASE_LANDSCAPE.md).

The driver is implemented and wired into the selection chain at highest priority:

```
ragflow → dify → weknora → pgvector 直配
```

- When ragflow is enabled and its health check passes, it is selected ahead of dify / weknora
- On health check failure it degrades along the chain (warn log); it fails immediately only when both dify and weknora are disabled and startup requires a healthy provider
- The relative order of dify / weknora is unchanged; the only behavioral change is ragflow inserted at the head of the chain

Version anchor: **RAGFlow v0.27.x** (the API surface is anchored to this; run the acceptance script regression before upgrading).

## Technical Architecture

### Code Layers

| Layer | Location | Responsibility |
| --- | --- | --- |
| HTTP client | `apps/server/pkg/ragflow/` | REST wrapper, two-tier envelope decision, multipart upload; does not import internal |
| driver | `apps/server/internal/platform/knowledgeprovider/ragflow/` | Contract four-operation implementation, delete-then-recreate upsert, retrieval parameter fallback |
| Selection chain | `apps/server/internal/app/server/knowledge_source.go` | `selectKnowledgeSource` facade, shared by startup-level and request-level |
| Tenant scoping | `internal/platform/configscope` + `internal/models` + migration 000009 | tenant/workspace override chain |

### RAGFlow API Facts (v0.27.x)

- Auth: `Authorization: Bearer ragflow-xxx`; base `{base_url}/api/v1`
- Uniform wrapper `{"code":<int>,"data":...,"message":...}`, success when `code==0`; **two-tier decision** between HTTP status code and business code
- Retrieval `POST /retrieval`: `{question, dataset_ids, page_size, similarity_threshold, knn_top_k}`; response `data.chunks[]{content, similarity, document_id, document_keyword, dataset_id}`
- Upload `POST /datasets/{id}/documents`: multipart `file` field; the driver constructs the upload in memory as `<标题>.txt`; response data is an array, take `[0]`; **after upload `run:"UNSTART"` does not parse automatically**
- Trigger parsing `POST /datasets/{id}/chunks`: `{"document_ids":[...]}`, asynchronous pipeline, the driver does not wait
- Dedup check `GET /datasets/{id}/documents?name=<文件名>`: RAGFlow has no idempotent upsert, so the driver deletes the old document and creates a new one itself
- Delete `DELETE /datasets/{id}/documents`: **with a JSON body** `{"ids":[...]}`
- Health check `GET /datasets?id={DatasetID}&page=1&page_size=1`: authenticated check that the configured dataset exists (verifies BaseURL, credentials, and dataset together)

### Delete-then-Recreate Upsert Semantics

RAGFlow upload does not overwrite a same-named document; the driver's `UpsertDocument` runs in order:

1. `ListDocuments(datasetID, "<标题>.txt")` dedup check by filename
2. On hit, `DeleteDocuments` to remove the old document
3. `UploadDocument` to upload the new content
4. `ParseDocuments` triggers the async parse and returns immediately (does not wait for parsing to complete; parse failures converge by re-raising the error and retrying)

### Retrieval Parameter Fallback Chain

| Parameter | Priority | Fallback |
| --- | --- | --- |
| TopK | Request-level > `ragflow.search.top_k` | 10 |
| Similarity threshold | Request-level > `ragflow.search.score_threshold` | 0.2 |

## Quick Start

### 1. Deploy RAGFlow

Self-host with Docker Compose per the [RAGFlow official docs](https://ragflow.io) (default service port 9380). After deployment:

1. Log into the Web UI and create a knowledge base (dataset); record the dataset ID
2. Generate an API Key on the API page (`ragflow-` prefix)

### 2. Configure servify

The ragflow block in `config.yml` (env placeholder form; staging/production templates default to `enabled: false`):

```yaml
ragflow:
  enabled: ${RAGFLOW_ENABLED}
  base_url: "${RAGFLOW_BASE_URL}"      # 如 http://ragflow-host:9380
  api_key: "${RAGFLOW_API_KEY}"
  dataset_id: "${RAGFLOW_DATASET_ID}"
  timeout: 30s
  search:
    top_k: 10
    score_threshold: 0.2
```

Security gate: when `ragflow.enabled=true` and `api_key` or `dataset_id` is empty, **startup is refused** (aligned with the dify/weknora zero-tolerance gate). A boolean placeholder with no env set fails safe (the `expandEnvVarsInConfig` mechanism, same shape as dify/weknora).

### 3. Verify the Integration

```bash
# 状态确认：knowledge_provider 应为 ragflow
curl -H "Authorization: Bearer <token>" http://127.0.0.1:8080/api/v1/ai/status
```

## Acceptance

### mock Mode (fully self-contained, CI-runnable)

```bash
make ragflow-acceptance
```

The script is fully self-contained: build the real servify binary → embedded python3 RAGFlow mock (stateful, request-tracing) → self-start the service on sqlite (ragflow pointed at the mock) → unauthenticated 401 negative case → assert `provider=ragflow` on `/api/v1/ai/status` → retrieval hit → upload ×2 (the second call verifies delete-then-recreate) → sync → full CRUD chain on the `/api/knowledge-docs` admin surface (create with `external_id`, update via delete-then-recreate, delete cleans up by external id and the read-back returns 404, all asserted against the mock trace) → mock trace assertions. Evidence lands in `scripts/test-results/ragflow-acceptance/` (manifest plus key responses/traces committed; admin-auth.json containing the acceptance JWT is not committed).

### real Mode (requires a real RAGFlow environment)

```bash
RAGFLOW_ACCEPTANCE_MODE=real \
SERVIFY_URL=http://<servify-host>:<port> \
RAGFLOW_URL=http://<ragflow-host>:9380 \
RAGFLOW_API_KEY=ragflow-xxx \
RAGFLOW_DATASET_ID=<dataset-id> \
./scripts/test-ragflow-acceptance.sh
```

real mode rejects localhost/private-network RAGFlow addresses (to prevent a local mock being mistaken for real-run evidence); the pass criteria align with the weknora real convention: provider healthy + retrieval hit + upload/sync success. After the run, `bash scripts/validate-acceptance-manifest.sh <evidence>/manifest.json` validates the evidence set.

### Tenant / Workspace Scoping

Admin scoped config supports a ragflow section (aligned with dify/weknora semantics):

- Storage: `tenant_configs.rag_flow_json` / `workspace_configs.rag_flow_json` (migration `000009_ragflow_scoped_config`)
- Override order: system → tenant → workspace → runtime, field-level merge (`enabled` sticky-true, non-empty strings override, non-zero numbers override)
- An AI service constructed at request level rebuilds the selection chain from the ragflow config resolved for the current tenant/workspace

## Secure Configuration

- staging / production templates set `ragflow.enabled: false`; enabling requires an explicit config change plus complete credentials
- `api_key` is a sensitive key: in templates it may only be `""` or the `${RAGFLOW_API_KEY}` placeholder (scanned by the secure-config template gate)
- Production recommendation: place RAGFlow and servify on the same private network segment, and use an internal address for BaseURL; inject the API Key via environment variables and do not write it to config files

## Troubleshooting

| Symptom | Investigation |
| --- | --- |
| `/ai/status` reports `knowledge_provider` is not ragflow | Check whether `ragflow.enabled` is true; check the log `RagFlow health check failed` — a health check failure degrades along the selection chain |
| Startup reports `ragflow is enabled but ragflow.api_key is empty` | The security gate fired; supply the env or turn off enabled |
| Upload succeeds but retrieval returns nothing | Parsing on the RAGFlow side is an async pipeline; after `POST chunks` is triggered you must wait for RAGFlow to finish parsing; check the document parsing status in the RAGFlow Web UI |
| Dedup check did not delete the old document | Confirm the old document's filename in RAGFlow is `<标题>.txt` (the driver dedups by upload filename) |
| `ragflow api error code=xxx` | Business-code error, look at the message; `ragflow http 4xx` is the HTTP layer (usually auth/path issues) |

## Current Status and Next Steps

- All four driver operations plus HealthCheck are implemented (100% test coverage)
- mock-mode acceptance is fully self-contained and in CI, manifest committed
- Tenant/workspace scoping fully aligned with dify/weknora
- ⬜ real-mode end-to-end run evidence against a real RAGFlow environment (awaiting external environment and credentials; same blocker as P1-1)
- ⬜ RAGFlow version tracking (anchored at v0.27.x; run `make ragflow-acceptance` regression before upgrading)
- Not doing: dataset name → id resolution (config supplies DatasetID directly, aligned with dify), `RebuildIndex` (RAGFlow manages its own parsing pipeline)

## Related Resources

- [RAGFlow official documentation](https://ragflow.io)
- [KNOWLEDGE_BASE_LANDSCAPE.md](KNOWLEDGE_BASE_LANDSCAPE.md) — full selection landscape
- [WEKNORA_INTEGRATION.md](WEKNORA_INTEGRATION.md) — the other side of the dual path
- [configuration-scopes.md](configuration-scopes.md) — the tenant/workspace config override mechanism
