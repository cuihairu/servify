# Tenant-Level Quota Rate-Limiting Policy (translation.quota unit/window)

> Status: **decided on the user's behalf pending confirmation** (decided by delegation
> during the 2026-10-15 inspection; the user may overturn it; overturning only
> requires editing this document, with no code coupling). Corresponds to the
> Phase 3 voice → tenant-level quota rate-limiting todo item's blocker
> "unit/window criteria not registered, pending product decision". This document
> provides a comparison table and a single clear recommendation;
> enforcement was landed per §4 (2026-10-15, default `enabled: false`);
> the tenant/workspace override limit chain was landed per §4.4 (2026-10-16,
> decided on behalf in the same round).
>
> **2026-10-30 已代决复核（docs/review/2026-10-30-decisions.md 第 4、7 项）**:
> 保留当前**软配额保护**口径——`limit + unit(requests|tokens) + window(hour|day|month)`、
> Redis 固定窗口、租户/工作区只覆盖 limit、默认关闭、计量故障放行;它作为**服务保护/软阈值,不是计费账本**,交付说明统一用「软配额保护」措辞,不作硬费用保证。无新需求时不重新设计、暂不统一开启生产配额;只有客户合同或明确支出风险要求硬封顶时,才单独设计(调用前原子预留/调用后结算/失败退款/崩溃恢复)并立项,立项触发时产品先给出计量对象/允许超额/故障时能否中断/并发规模/预算归属。

## 1. Current State (measured 2026-10-15)

- `translation.quota` **has only a direction registration, no schema at all**:
  a repo-wide grep for `Quota`
  yields zero hits in Go/YAML/SQL/TS; the only sources are
  `docs/realtime-translation-design.md` §3.2 ("per-tenant configuration switches and
  quotas
  (reusing the configscope tenant-override pattern)") and §4.6 ("per-tenant quota
  rate-limiting,
  429 when over quota").
- The repo already has two sets of "window" vocabulary, neither with a
  `unit`/`window` string enum:
  - Rate limiter: `RateLimitingConfig.RequestsPerMinute + Burst`
    (`apps/server/internal/config/config.go:474-495`), window implicitly 1 minute
    (`ratePerSec: rpm/60.0`), token bucket, keyed by socket peer IP,
    system scope.
  - Session risk window: `*_window_{minutes|hours|days}` int field suffix
    (`config.go:439-443`, 5 precedents), the unit is the field-name suffix.
- Measurement status: translation token usage already has an observation surface
  (`TranslateResult.TokenUsage` →
  `ai_llm_tokens_total{provider, token_type}`、
  `voice_translation_sentences_total{outcome}`), but **has no tenant label**,
  and Prometheus counters accumulate per process and cannot be aggregated
  across instances.
- No usage ledger table/migration exists (no quota/usage-related entries among the
  23 migrations).

## 2. Comparison Table

| Option | Form | Pros | Cons |
| --- | --- | --- | --- |
| A. Reuse the rate-limiter criteria (`requests_per_minute + burst`) | Same shape as the current `RateLimitingConfig` | Zero new concepts, reuse the existing middleware | The token bucket is a smooth-rate semantics, not a "budget + periodic reset" quota ledger; only supports a minute window; no token-usage dimension; keyed by socket peer IP, not tenant |
| B. Unit-suffixed int fields (`limit` + `window_minutes/hours/days`) | Follow the session-risk naming scheme | 5 precedents in the repo | The measurement unit (count vs tokens) and the time window are orthogonal; the suffix scheme cannot express two dimensions; adding a window tier means adding a field, fields explode |
| C. Explicit `limit + unit + window` enums | `unit: requests\|tokens`，`window: hour\|day\|month`，fixed rolling window | Complete orthogonal expression of measurement unit × time window; enums are validatable and registrable (aligned with the config_template_lint gate); naturally attaches to configscope tenant override | New enums and validation needed; the window anchor requires shared storage (see §4) |

## 3. Recommendation (decided on behalf)

**Option C**. The criteria are defined as:

```yaml
translation:
  quota:
    enabled: false        # 默认关；开启即生效（§4 已落地）
    limit: 100000         # 每租户每窗口配额量
    unit: tokens          # requests | tokens（tokens 取 LLM TokenUsage 累计）
    window: day           # hour | day | month，固定滚动窗口，超配额 429
```

- `unit` and `window` are orthogonal: `requests` counts
  translation call count, `tokens` counts the
  `TokenUsage` accumulation; the window uses fixed rolling semantics (no token-bucket
  smoothing).
- **Token limits are soft thresholds, not hard spend caps.** Usage is recorded after
  provider success, so concurrent calls and a single call's usage can cross `limit`;
  missing provider usage and meter failures also cannot be charged. Non-empty batch
  translation returns `ErrTranslationQuotaUsageUnavailable` before provider execution
  while the result contract has no token-usage surface. This is an abuse/cost
  guardrail, not billing or a strict budget.
- Default `enabled: false`: the criteria are registered first, and no behavior
  occurs before the enforcement gap is filled.
- Aligned with `docs/realtime-translation-design.md` §3.2/§4.6: configscope
  tenant override (tenant/workspace layer overriding the system value, §4.4
  landed), 429 when over quota.

**Measurement storage choice (decided on behalf 2026-10-15)**: Redis fixed window, no
DB usage table.
Rationale: this repo's production config already pins redis (config.yml startup
validation rejects inmemory),
no migration, no column guard; window state is naturally transient, and the
consequence of a redis restart resetting the window counter is only allowing one
extra window of requests (the 429 protection surface, not a billing surface), causing
no revenue loss. The window anchor goes into the key name
(`servify:quota:translation:{window}:{anchor}:{tenant}`), with TTL
fallback self-cleanup. On meter failure, fail-open (allow the current request and
keep the translation main path available — the protection surface
must not kill the main surface in turn).

## 4. Enforcement (landed 2026-10-15)

Three gaps were filled per the §3 decision (default `enabled: false`; enabling
requires redis availability in sync):

1. **Tenant-dimension usage ledger** → `internal/platform/quotameter`: a Redis
   fixed-window
   meter (no DB usage table, §3 measurement storage choice decision); `Usage` reads the
   current window
   accumulation, `Add` atomically accumulates (IncrBy + ExpireNX fallback TTL); the
   window reset-and-reaccumulate
   tolerance boundary is in the §3 decision paragraph.
2. **Tenant-keyed checkpoint** → `translation/delivery` `QuotaGate` decorator: the
   assembly layer
   wraps it around the translation facade (REST translate / real-time auto
   translation / historical batch annotation /
   the voice channel all pass through the gate uniformly), with the tenant
   extracted from the ctx authentication info (`platformauth`),
   silently skipped when no tenant; `ErrTranslationQuotaExceeded` → HTTP 429. The
   `requests`
   unit is incremented +1 before comparing against the limit (rejected calls also
   occupy the window, avoiding check-then-act races); the `tokens`
   unit is rejected upfront on `Usage ≥ limit` (not consuming the meter), and after
   success is recorded by
    `TokenUsage.TotalTokens` (not recorded when the provider returns no usage); this
    is a **soft threshold**, because usage is only known after the provider call and
    concurrent calls can overshoot it. For non-empty batch annotation, `tokens` mode
    returns `ErrTranslationQuotaUsageUnavailable` before invoking the provider because
    the batch result has no usage surface. Empty batches pass
    through without a provider call. This remains a guardrail, not a billing cap.
   Meter failures always fail-open (allow + Warn log).
3. **Shared window anchor** → the key name encodes the anchor
   `servify:quota:translation:{window}:{anchor}:{tenant}` (hour→top of the hour,
   day→current day, month→current month); multiple instances sharing the same redis
   key naturally align; TTL =
   remaining window + 10min grace period, old keys self-clean as a fallback.

**Assembly validation** (`buildTranslationQuotaGate`): when enabled=true, validate
unit/window/limit and redis availability, fail-fast on illegality. The four
config templates are synced (schema registration passed the config_template_lint gate).

### 4.4 configscope tenant override (landed by delegation 2026-10-16)

The alignment commitment for the "configscope tenant override" in `§3` and
realtime-translation-design §3.2 is filled
(the original gap was recorded as "pending real-requirement scheduling", and this
round it is landed directly under delegation authority; the user may
overturn — overturning only changes this document and this section's implementation):

1. **Storage**: migration `000024_translation_quota_scoped_config` adds one column
   each to
   `tenant_configs` / `workspace_configs`:
   `translation_quota_json` text (aligned with the `rag_flow_json` /
   `session_risk_json` column shapes, sqlite goes through AutoMigrate), aligned
   column-by-column with
   `models.TenantConfig.TranslationQuotaJSON` /
   `models.WorkspaceConfig.TranslationQuotaJSON` (the column-level reconciliation
   guard `TestMigrationChainCoversAllModelColumns` passes). The column only stores
   `{limit: N}` (`config.TranslationQuotaOverrideConfig`).
2. **Resolution**: `configscope.Resolver.ResolveTranslationQuotaLimit(ctx)` three-layer
   merge — system default (`translation.quota.limit`) + tenant override + workspace
   override (workspace first, consistent with the portal/openai/session_risk layering
   criteria); an
   override value `<= 0` is treated as unset and falls back to the previous layer, as
   does a provider error or no override row
   (the protection surface must not wrongly deny service due to override read
   failures). Tenant and workspace identity comes from the
   `platformauth` ctx; the provider shape reuses the existing gorm provider.
3. **Checkpoint**: `QuotaGate` gains a `QuotaLimitSource func(ctx) int64` port,
   resolving the per-request limit on each request
   (`NewQuotaGate` keeps the static-limit semantics,
   `NewQuotaGateWithLimitSource` takes the resolver); both the `Translate` and
   `BatchTranslate` sides go through `limitFor(ctx)` uniformly. The assembly layer
   `buildTranslationQuotaGate` constructs the resolver injection when a primary
   database exists, and passes nil for no-primary-database deployment forms
   (guarding only by the system default limit, zero behavioral difference).
4. **Only override `limit`**: `enabled` / `unit` / `window` stay system-level —
   `window` is already in the Redis meter key name, and per-tenant window switching
   would require another set of meter keys and key-space governance, with benefit
   not worth the complexity; `unit` is bound to metering semantics (requests
   pre-occupy the window /
   tokens are recorded post-hoc), and per-tenant mixing would create criteria
   ambiguity.

**Explicitly not done this round** (remaining gaps):

- **Admin-plane write surface**: the REST write path
  (`PUT /security/config/{tenant,workspace}`) already persists
  `translation_quota`; since 2026-10-18 (delegated decision, recorded here) it
  also carries the section through the governance machinery — payload merge for
  risk templates, history diff keys, and a section-aware verification check
  (`translation_quota_verified` / `translation_quota_override_verified`,
  medium risk, approval not required since only `limit` is overridden while
  enabled/unit/window stay system-level). What still does not exist anywhere is
  the admin frontend itself: the admin has no scoped-config form, field
  validation, or audit view for any section, so that first write surface stays a
  product-surface project and is initiated separately.
- **Tenant plumbing for the real-time/historical paths**: when these paths' ctx
  does not yet carry a tenant, they are skipped as no-tenant
  (passing through the gate without metering); the REST translate endpoint already has
  an authenticated tenant and takes full effect.

## 5. How to Overturn

Overturning the criteria = editing this document + editing the corresponding note in
`todo.md` + setting `translation.quota.enabled`
back to false (default off, no data rollback cost; meter keys self-clean via TTL).
Overriding the limit
chain (§4.4) = clearing the `translation_quota_json` override rows returns to the
system single-value criteria
(the column can stay, no override row = no override; the migration is a pure
add-column, no rollback needed).
