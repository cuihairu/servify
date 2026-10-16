> **English** | [中文](modules-dependency-map.md)

# Modules vs Legacy Services/Models Dependency Map

> Data snapshot: full scan of `apps/server/internal/modules` on 2026-09-15 (command at the end, reproducible).
> Purpose: decision basis for closing out P1-7 boundaries — where to cut first, in what order, and into what shape.

## 1. modules → internal/services (legacy service layer)

**Current state: 0 direct links (closed); final state reached — legacy `internal/services` was removed wholesale with P3-2 (concluded 2026-09-19, 18 cuts), this section's aliases die with it, and the table below is archived as history.**

The cut this round: contract types sink down into each module, and legacy `internal/services` references the same definitions in reverse through **type aliases** — both sides are the same Go type, not two parallel DTOs, so the wire contract and existing tests drift zero.

| Module | Contract type | Definition location | Legacy alias location |
| --- | --- | --- | --- |
| ai | `AIResponse` | `modules/ai/delivery/contract_types.go` | `services/ai.go` |
| ai | `AIMetrics`, `EnhancedAIResponse` | `modules/ai/delivery/contract_types.go` | `services/ai_enhanced.go` |
| customer | `CustomerCreateRequest` and 8 other request/response types | `modules/customer/api/contract_types.go` (package `customerapi`) | `services/customer_service.go` |
| knowledge | `KnowledgeDocCreateRequest` and 2 other request types | `modules/knowledge/application/contract_types.go` | `services/knowledge_doc_service.go` |

Notes:

- ai's contracts live in delivery rather than application because `modules/ai/application/types.go` already has a same-named but differently-shaped `AIResponse` (used inside the orchestrator), avoiding a name collision.
- customer's contracts live in `modules/customer/api` (package `customerapi`) rather than application because swag (API documentation generation) indexes type qualifiers in annotations by **package declaration name**, and `application` is a duplicate name across modules and would be ambiguous; the annotation in `customer_handler.go` writes `customerapi.CustomerX` directly, which is the only hook that lets swag parse successfully without flags. This constraint has been generalized: any type referenced by swag annotations (directly, or through a `models.`/`services.` alias chain) must live in a package whose declaration name is unique repo-wide (quality's `qualitydomain` is exactly this case; the annotation can keep `models.QualityReview` unchanged with zero artifact drift, proven in practice). knowledge/ai handlers have no swag annotations and are not subject to this constraint.
- Anti-regression gate: `go test ./scripts -run TestModulesDoNotImportLegacyServices` (parser-level import check, including test files), now included in CI script-checks.
- **2026-09-30 reference surface closed**: the transitional entries on the models side of §2 (type aliases, delivery status constant aliases, the `NewEmbedding` forwarding function) were cleared to zero references inside modules and switched to direct owner-package links (suggestion/ai → `knowledgedomain`, webhook → `voiceinfra`, sla tests → `automationdomain`; same-type alias direct link, compile-time equivalent, zero behavior change), pinned at the time by the guard `TestModulesDoNotUseMigratedTypeAliases` — parser-level, dynamically resolving the transitional entry set from the models package source (new migrations are automatically included; an empty resolution fails the guard itself, preventing false greens), including test files, now in CI script-checks. **2026-10-14 final state**: all the aliases themselves were deleted (`models/automation.go`, `webhook.go`, `embedding.go` removed with the migration; `models.go` stripped of 9 alias blocks and 5 owner imports; the last consumers — AutoMigrate registration surface, platform provider, handlers tests — were switched to direct owner-package links, swag artifacts zero drift), and the guard was retired with them — once the transitional entry set is permanently empty, the old guard is left with only a self-referential mismatch; a stronger lower-level premise now takes over: `TestModelsDoesNotImportModulePackages` (models must not import `internal/modules`; the only carrier of aliases/constant aliases/forwarding functions is an owner-package import, so banning the premise bans the reflux, with no type inventory to maintain).

## 2. modules → internal/models (shared model layer)

**Current state: 15 modules, 78 non-test files link directly to `internal/models` (re-measured 2026-10-08; the numbers in this section drift with migration batches, trust the re-measure).** Top types by reference count:

| Type | References | Category |
| --- | --- | --- |
| `Ticket` / `Session` / `Agent` / `Message` / `User` / `Customer` | 35~260 | Shared domain core (largest cross-module fan-out) |
| `AgentGroup` / `CustomField` / `TicketComment` / `TicketStatus` / `TransferRecord` / `WaitingRecord` | 29~61 | Cross-module secondary core |
| `WebhookEndpoint` / `WebhookDelivery` | 65~75 | Module-owned (webhook, **migrated into** `modules/webhook/domain/models.go`, legacy side `models.WebhookEndpoint` etc. are aliases, delivery status constants migrated along; legacy aliases deleted 2026-10-14) |
| `AutomationTrigger` / `AutomationTimer` | 31~87 | Module-owned (automation, **migrated into** `modules/automation/domain/models.go`, including `AutomationRun`; legacy aliases deleted 2026-10-14) |
| `QualityReview` | 62 | Module-owned (quality, **migrated into** `modules/quality/domain/models.go`, package declaration uses the unique `qualitydomain`; legacy aliases deleted 2026-10-14) |
| `RemoteAssistSession` / `RemoteAssistAnnotation` | 33~46 | Module-owned (assist, **migrated into** `modules/assist/domain/models.go`, package declaration uses the unique `assistdomain`; legacy aliases deleted 2026-10-14) |
| `VoiceCall` | 25 | Module-owned (voice, **migrated into** `modules/voice/infra/models.go`; legacy aliases deleted 2026-10-14) |
| `KnowledgeDoc` | 32 | Module-owned (knowledge, **migrated into** `modules/knowledge/domain/models.go`, including `KnowledgeIndexJob` and the vector column value object `Embedding`; legacy aliases and the `NewEmbedding` forwarding function deleted 2026-10-14) |
| `DailyStats` | 21 | Statistics aggregate (analytics reads and writes, other modules read only) |

Layer distribution (non-test file counts):

| Module | application | delivery | infra |
| --- | --- | --- | --- |
| agent | 3 | 3 | 3 |
| ai | 1 | 1 | - |
| assist | 2 | 2 | 1 |
| automation | 3 | 2 | 1 |
| conversation | - | 2 | 1 |
| customer | 3 | 2 | 1 |
| knowledge | - | 2 | 1 |
| quality | 3 | 1 | 1 |
| routing | - | 4 | 1 |
| ticket | 1 | 3 | 1 |
| webhook | 2 | 2 | 1 |
| analytics / gamification / suggestion / voice | - | - | 1~3 each |

## 3. Split Strategy (finalized, P1-7 blocking item cleared)

1. **Keep the shared domain core as a shared kernel**: `User` / `Ticket` / `Session` / `Agent` / `Message` / `Customer` and other high-frequency cross-module types are not scattered into a single module; they stay in the shared model layer (currently `internal/models`), but references from modules are closed to "read-only domain shape", with persistence details (GORM scope, hook) handled by each module's infra layer. **Reference surface closure achieved (2026-09-30), alias final-state cleanup complete (2026-10-14)**: references to migrated type aliases inside modules were cleared (18 files mechanically replaced with direct owner-package links), and then all transitional alias bodies in `internal/models` were deleted. The gate also changed generations: `TestModulesDoNotUseMigratedTypeAliases` (self-referential mismatch once the set is permanently empty) retired, and `TestModelsDoesNotImportModulePackages` took over — parser-level assertion that models must not import `internal/modules`, so the modules → models one-directional layering cannot regress; with no carrier, there is no reflux.
2. **Migrate module-owned types into their owning modules step by step**: webhook / automation / quality / assist / voice / knowledge owned models migrate using the same cut of "first switch delivery/application references to in-module types + transitional legacy aliases", prioritized by "small reference surface, no cross-module consumers": voice → gamification → suggestion → webhook → automation → quality → assist.
   - **Migration log**: ①2026-09-15 voice complete — the three GORM models `VoiceCall` / `VoiceRecording` / `VoiceTranscript` moved into `modules/voice/infra/models.go` (infra is voice's only consumer, defining them directly in infra eliminates the mapping layer); `internal/models` keeps the type aliases; the AutoMigrate registration in `bootstrap/migrate.go` and webhook's snapshot read-only path are unchanged through the aliases; all six voice/infra files (including tests) dropped the `internal/models` import. ②2026-09-15 webhook complete — `models/webhook.go` moved file-by-file into `modules/webhook/domain/models.go` (`WebhookEndpoint` / `WebhookDelivery` + four delivery status constants; all three module layers consume them, hence domain rather than infra); `internal/models` keeps type and constant aliases; migrate registration and handlers tests unchanged through aliases. ③2026-09-15 automation complete — `models/automation.go` moved file-by-file into `modules/automation/domain/models.go` (`AutomationTrigger` / `AutomationRun` / `AutomationTimer`; `AutomationRun.Trigger`'s association with the same-file type migrates self-consistently); `internal/models` keeps the type aliases; migrate registration, legacy `services/automation_*`, and handlers tests unchanged through aliases. ④2026-09-16 quality complete — `QualityReview` moved into `modules/quality/domain/models.go`, package declaration uses the unique `qualitydomain` (the first migration target referenced by a swag annotation: `quality_handler.go`'s `@Success 200 {object} models.QualityReview` resolves through the models alias chain, proven zero swag artifact drift and all 29 fields intact); `internal/models` keeps the type alias; migrate registration and handlers tests unchanged. ⑤2026-09-16 assist complete — `RemoteAssistSession` / `RemoteAssistAnnotation` moved into `modules/assist/domain/models.go`, package declaration uses the unique `assistdomain` (assist_handler.go / assist_recording_handler.go have 5 swag annotation references to `models.RemoteAssistX`, resolving through the alias chain, proven zero artifact drift); migrate registration and handlers unchanged. ⑥2026-09-16 knowledge complete (**the last cut; module-owned type migration fully closed**) — `KnowledgeDoc` / `KnowledgeIndexJob` moved into `modules/knowledge/domain/models.go`; the vector column value object `Embedding` / `NewEmbedding` moved file-by-file from `models/embedding.go` into `domain/embedding.go` (it is KnowledgeDoc's column type; without moving it, domain would depend back on models into a cycle); `internal/models` keeps the type aliases and the `NewEmbedding` forwarding function; migrate registration, the pgvector provider, legacy `services/ai*` / `knowledge_doc_service`, and handlers unchanged through aliases; knowledge's handler has no swag annotation, so the domain package declaration stays the generic `domain` without needing a unique name; six files of the knowledge module (infra×1 / delivery×2 + tests×3) all dropped the `internal/models` import; `Embedding`'s Scan/Value tests moved with the file into the domain package to preserve coverage.
   - **Order correction (2026-09-15)**: gamification and suggestion were checked type by type and have **no module-owned models types** — gamification consumes only shared core (`Ticket` / `CustomerSatisfaction` / `Agent` / `User`), and suggestion consumes only `Ticket` and knowledge's own `KnowledgeDoc` (within knowledge's migration scope). Neither needs migration; the remaining order is corrected to: ~~voice → gamification → suggestion →~~ webhook ✅ → automation ✅ → quality ✅ → assist ✅ → knowledge ✅ (all complete).
3. **Completed contract-type migrations do not roll back**: the aliases in section 1 are a transitional state; the final state is that the aliases are deleted together with the corresponding compatibility layer in legacy `internal/services` when it is removed (following P3-2 services/modules final boundary closure). P3-2 concluded on 2026-09-19 with the services layer removed wholesale, and section 1's aliases died with it — that final state is reached (added as a note on 2026-09-30 review).
4. **No new additions**: new module code must not import `internal/services` (enforced by CI gate); new modules do not reference "module-owned" category types in `internal/models` by default.

## 4. Reproduction Scan Command

```bash
# modules -> services 直连（应为空）
grep -rln '"servify/apps/server/internal/services' apps/server/internal/modules --include='*.go'

# modules -> models 直连按模块计数
for d in apps/server/internal/modules/*/; do
  m=$(basename "$d")
  hits=$(grep -rln '"servify/apps/server/internal/models' "$d" --include='*.go' | grep -vc _test)
  echo "$m: $hits"
done

# models 类型引用频次
grep -rhoE 'models\.[A-Z][A-Za-z]+' apps/server/internal/modules --include='*.go' \
  | sort | uniq -c | sort -rn | head -25

# models → internal/modules 反向依赖（应为空；TestModelsDoesNotImportModulePackages 同口径）
grep -rn '"servify/apps/server/internal/modules' apps/server/internal/models --include='*.go'
```
