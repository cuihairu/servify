> **English** | [中文](surface-naming.md)

# Public Naming Governance: legacy/compat Mixed-Name Cleanup and Retention Register (P3-1)

P3-1's ruling convention: **internal compatibility layers stay, but external naming unifies on product semantics**. This document registers two kinds of decisions — mixed names already cleaned up, and names that "look like mixed names but are actually product semantics or storage contracts, deliberately kept". When a similar name comes up later, check this table first, to avoid repeated rulings or mistaken cleanup.

## Cleaned Up (2026-09, P3-1)

| Cleanup item | Location | Notes |
| --- | --- | --- |
| `reconnectAttempts` / `reconnectDelay` config fields | `sdk/packages/core` (`ServifyConfig`, `WebSocketManagerOptions`) | `reconnectPolicy` had long coexisted with more complete semantics; the old fields were old names for the same behavior. The SDK is not published to npm, and there are no consumers in the repository (examples/demo-sdk/admin), so deletion is not an external breaking change |
| `LegacyReconnectOptions` and the second parameter of `normalizeReconnectPolicy` | `sdk/packages/core/src/contracts/reconnect.ts` | The compatibility shim for the old fields, deleted together with the old fields |
| `EnableWeKnora` / `DisableWeKnora` handler wrappers | `apps/server/internal/handlers/ai_handler.go` | Only method-name wrappers from the old routing era, with no production routes mounted; the new routes `/api/v1/ai/knowledge-provider/enable|disable` and `EnableKnowledgeProvider`/`DisableKnowledgeProvider` already exist and are covered by tests |

The SDK-side register is in `sdk/SURFACE_GOVERNANCE.md` (the Breaking Change Checklist requires leaving a trace of removed items).

## Deliberately Kept (not mixed names, do not clean up)

| Retention item | Location | Reason for keeping |
| --- | --- | --- |
| `weknora.*` config keys (`ai.weknora`, `health_checks.weknora`, `WEKNORA_*` environment variables) | `apps/server/internal/config`, `bootstrap/config.go` | P0-5 ruling: WeKnora is the knowledge provider's **product real name** (a compatibility access path), not an old name; `knowledge.provider` is the switch surface, `weknora.*` is that provider's configuration namespace |
| `weknora_usage_count` / `weknora_latency` (`AIMetrics` JSON fields, `servify_ai_weknora_usage_total` metric) | `modules/ai/delivery/contract_types.go`, `handlers/ai_handler.go` | A **parent-child dimension** with `knowledge_provider_usage_count`, not old-vs-new names: totals dispatch counts per active provider (dify/weknora each have their own sub-counts and latency). Deleting would lose the per-provider observability dimension |
| `WeKnoraUsageToday` (analytics `weknora_usage_today`) | `modules/analytics/{contract,domain}` (`contract/types.go` and `domain/entities.go`) | Same as above: a per-provider statistics dimension, coexisting with `knowledge_provider_usage_today` |
| `service_type='weknora'` enum value | `scripts/init-db.sql`, the knowledge mapping module | Storage contract: the provider real-name identifier in DB rows |
| `servify_weknora_mappings` table | `scripts/init-db.sql` | Storage contract: the table name carries the provider real name; renaming means a migration, with no mixed-name benefit |
| `fallback.legacy_kb_enabled` | `config.go` | P0-5 ruling: the semantics are literally "legacy built-in knowledge base switch", `legacy` is an accurate description, not a mixed name |
| configscope section key `weknora` | `platform/configscope/gorm_store.go` | Persistence contract: the section identifier of scoped configuration is stored in the DB |
| `apps/demo-sdk/widget.js`'s `reconnectAttempts` | the demo's own reconnection logic | An internal counter unrelated to the SDK; only a name collision, not an SDK legacy option |

## Judgment Convention (for reuse by later similar rulings)

1. **Two names coexisting for the same behavior** (e.g. reconnectPolicy vs reconnectAttempts) → clean up the old name; a package not published in the repository is not an external breaking change.
2. **A provider real name or sub-strategy dimension** (WeKnora as a concretely compatibility-accessed product) → keep; "unify on product semantics" does not mean erasing the specific provider's name.
3. **Storage contracts** (DB column/table names/enum values/stored section keys) → renaming needs a migration with no consumer benefit; keep by default and register here.
