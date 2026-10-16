# Configuration Scopes

This document defines the scope boundaries, override order, and change constraints of Servify's current configuration, to advance `11 / T4 configuration-scopes`.

Goals:

- Distinguish which configurations can only be system-level
- Distinguish which configurations should sink down to tenant / workspace
- Avoid continuing to mix secrets, provider endpoints, business policy, and runtime switches in the same `config.yml`
- Provide unified rules for subsequent configuration persistence, audit, and rollback

## Current Configuration Sources

The repo currently has 4 kinds of configuration sources:

1. Code defaults
   - Entry: `apps/server/internal/config/config.go`
   - Purpose: guarantee local dev and default startup are runnable
   - Limitation: can only carry safe defaults, cannot carry production secrets

2. System startup config files
   - Entry: `config.yml`, `config.weknora.yml`
   - Load: `apps/server/internal/app/bootstrap/config.go`
   - Purpose: deployment-level infrastructure and system default policy; of these `config.weknora.yml` is mainly for WeKnora compatibility regression; the knowledge-base selection chain is ragflow → dify → weknora → pgvector direct (RAGFlow and WeKnora are the canonical knowledge-base dual paths, Dify retained as orchestration-platform dataset compatibility; the full selection landscape is in KNOWLEDGE_BASE_LANDSCAPE, the RAGFlow integration is in RAGFLOW_INTEGRATION)

3. Environment variable overrides
   - Entry: Viper `AutomaticEnv()`
   - Purpose: container deployment, CI, secret injection
   - Limitation: should only override system-level config, should not carry tenant / workspace business policy

4. Database-stored scoped configuration objects
   - Currently existing:
     - `SLAConfig`
     - `CustomField`
     - `AppIntegration`
   - Characteristic: already carry `tenant_id` / `workspace_id`, filterable by context

There is also one more kind of non-persistent runtime scope input:

- `tenant_id`, `workspace_id` in JWT / request context
- Temporary parameters in AI / knowledge requests, e.g. `knowledge_id`
- Temporary execution-state parameters in routing, transfer, session

These belong to runtime scope, not long-term configuration.

## Scope Model

### 1. System Scope

Definition:

- Shared by the entire deployment instance
- Only modifiable by ops, deployment processes, or system administrators
- Does not switch with tenant / workspace

Typical content:

- `server`
- `database`
- `redis`
- `jwt.secret`
- `log`
- `monitoring`
- tracing / exporter endpoint
- Global CORS baseline
- Global rate limiting baseline
- Upload root path, filesystem paths
- AI / knowledge provider base endpoints, client credentials, global timeouts

Storage principles:

- Prefer config file + environment variables
- Secrets are injected only via environment variables or an external secret manager
- Should not be stored in tenant config tables

### 2. Tenant Scope

Definition:

- Shared by one business tenant within the same deployment
- Takes effect for all workspaces under the tenant
- Allowed to be overridden at finer granularity by workspaces

Typical content:

- Tenant default AI provider selection
- Tenant default knowledge provider / knowledge base namespace
- Tenant default routing / SLA / automation policy
- Tenant default portal branding
- Tenant-level integration switches and capability policy
- Tenant-level security policy supplements, e.g. API surface allowlist, public knowledge base switches

Storage principles:

- Should go into database config objects or a dedicated tenant config model
- Should no longer be hard-coded in `config.yml`
- If credentials are involved, prefer storing a "secret reference" over plaintext

### 3. Workspace Scope

Definition:

- Business configuration of a specific workspace under a tenant
- Prioritizes service-team, business-line, and channel-face differences

Typical content:

- `CustomField`
- `SLAConfig`
- `AppIntegration`
- Workspace-level routing policy
- Workspace-level AI prompt/profile
- Workspace-level knowledge source selection
- Workspace-level portal appearance, default language, channel switches

Storage principles:

- Should be persisted to the database
- Must carry `tenant_id` + `workspace_id`
- Must go through audit

### 4. Runtime Scope

Definition:

- Temporarily takes effect during a single request, single task, single session execution
- Should not be persisted as long-term configuration

Typical content:

- `tenant_id` / `workspace_id` in the request context
- Whether retrieval / fallback is enabled for this AI request
- Priority, target skills, notes of this transfer
- Time range, filters, sort of this export

Storage principles:

- Exists only in request / job / event / session context
- Not persisted by default
- If it affects high-risk behavior, it should go into the audit log

## Configuration Matrix

### Infrastructure & Platform

- `server`: `system`
- `database`: `system`
- `redis`: `system`
- `webrtc.stun_server`: `system`
- `jwt.secret` / token lifecycle baseline: `system`
- `log` / `monitoring` / tracing exporter: `system`
- `upload.storage_path`: `system`

### Security & Access

- `security.cors`: `system`
- `security.rbac.roles`: `system`
- `security.rate_limiting` defaults: `system`
- `security.session_risk` baseline: `system`, allows `tenant` / `workspace` to refine risk thresholds within an authorized scope
- `security.session_risk_profiles.{environment}`: `system`, used to select the deployment-environment-level risk baseline by `server.environment`, then stacked with tenant / workspace override
- `security.session_ip_intelligence` provider endpoint / API key / timeout: `system`
- Whether a tenant / workspace allows public knowledge bases, Portal, specific integrations: `tenant` or `workspace`

### AI Provider

- Provider endpoint, API key, client secret, global timeout: `system`
- Default provider registry and fallback baseline: `system`
- Tenant default provider selection, default model, default prompt profile: `tenant`
- Workspace model selection, temperature, tool policy targeting a business line: `workspace`
- Whether a single request force-disables retrieval / fallback: `runtime`

Conclusions:

- Provider credentials should not become workspace configuration
- Workspace can only override "policy selection", cannot directly hold system-level infrastructure parameters

### Knowledge Provider

- Provider endpoint, global API key: `system`
- Tenant default `knowledge_base_id` / namespace mapping: `tenant`
- Workspace default knowledge base, retrieval threshold / topK / strategy: `workspace`
- Per-query temporary `knowledge_id`, temporary retrieval strategy: `runtime`

### Routing Policy

- Global fallback policy and implementation switches: `system`
- Tenant default queueing/assignment/load policy: `tenant`
- Workspace channel differences, skill priority, human-handoff threshold: `workspace`
- Per-session transfer reason / target skills / priority: `runtime`

### Portal / UX / Business Policy

- System default `portal` appearance and locale: `system`
- Tenant branding: `tenant`
- Workspace portal display, public help center, channel entries: `workspace`

## Override Order

The unified override order is defined as:

1. `runtime`
2. `workspace`
3. `tenant`
4. `system`
5. `code default`

Explanation:

- `code default` only handles fallback
- `system` handles deployment-level defaults
- `tenant` handles tenant-level business defaults
- `workspace` handles team/channel-level differences
- `runtime` only does temporary override for this execution

Constraints:

- Lower levels cannot override higher-level secrets
- `runtime` cannot break permission boundaries, can only narrow or temporarily select within an authorized scope
- With no explicit workspace config, fall back to tenant
- With no tenant config, fall back to system

## Current Recommended Landing Points

### Content that stays in the system config file

- `server`
- `database`
- `redis`
- `jwt`
- `log`
- `monitoring`
- Provider endpoint / credentials
- Upload root path

### Content that should gradually move out of the system config and into scoped config

- `portal.brand_name`, `logo_url`, colors, locale
- AI provider default model selection and prompt profile
- Knowledge base default mapping
- Routing policy
- Workspace-level integrations enablement

Current code skeleton:

- `internal/platform/configscope.Resolver` already supports `portal`, `OpenAI`, `Dify / RAGFlow / WeKnora knowledge provider` with the `tenant -> workspace -> runtime` provider override order
- `ai.provider` selection (`openai | anthropic | local`) is a single global decision, not going through scope override; the openai-family scope-resolution result (key/base_url/model etc.) is written into each outbound LLM call at the orchestration layer, anthropic only consumes the global `ai.anthropic` section, `local` is a zero-external-dependency extractive-QA baseline (see `config.yml` ai.provider comment) — switching vendors changes one place `ai.provider`, unified construction is collected at `internal/platform/llm/factory`
- `internal/platform/configscope.Resolver` now supports the `session_risk` `system -> tenant -> workspace -> runtime` risk-threshold resolution chain
- Database-persisted `TenantConfig` / `WorkspaceConfig` GORM providers have been added; the current public portal already integrates this read path
- The admin surface has the minimal read/write entry for tenant/workspace scoped config, currently covering `portal` / `OpenAI` / knowledge provider / `session_risk`
- The scoped config write API now records before/after snapshots; tenant/workspace-level `PUT` / `rollback` / `verify` also strictly require `change_ref` and `reason`, and write these change-control fields into audit request metadata
- For `PUT` / `rollback` hitting the high-risk config cluster, the current API not only requires `approval_ref`, but also requires `approval_ref + change_ref` to match a recorded `scoped_config.{scope}.approve` audit event, and the approver and the actual executor must be different people; the API still supports JSON body or `X-Approval-Ref` header; the current high-risk coverage mainly includes provider endpoint, knowledge mapping, session risk core thresholds, and the rollback operation itself
- Scoped config also provides tenant/workspace-level change history list metadata, field-path-level diff preview of a single history detail, current/snapshot value pairs with `added/removed/updated` types, and a rollback-by-audit-record entry requiring explicit confirmation
- The admin surface has gained a post-execution verification entry, able to write `passed` / `failed` verification results for a single update / rollback audit record; besides `status` / `notes` / `evidence`, verification submission also needs to carry `checks` aligned with `verification_template.checks`
- Verification is now collected into templated checks: `passed` must carry evidence, and all required check items in the template must be explicitly `passed`; `failed` must carry notes, and at least one template check item must be marked `failed`
- `verification_template` now returns root-level `changed_paths`, and breaks a single check item down by field risk into portal public surface / locale, OpenAI endpoint / model, knowledge provider endpoint / KB mapping / health check, session risk score / window / concurrency; each check also carries `risk_level` and the `changed_paths` it hit
- Write responses, history lists, details, and verify responses also return `change_control`, `change_risk`, and `approval_policy`; of these `change_control` carries `approval_ref`, `change_risk` gives `risk_level` / `risk_reasons` / `changed_paths`, and `approval_policy` now further distinguishes real approval states like providing a ticket number only, approval event persisted, and latest approver
- A unified `governance_status` / `governance_policy` is also returned, combining pre-approval and post-execution verification into a single state machine; the verify response additionally returns `source_governance_status` / `source_governance_policy`
- Verification now enforces a minimal two-person review constraint: the reviewer must carry `user_id` and must not be the same as the original change executor; history lists, details, and verify responses give `verification_status`, the latest verification record, verification history, `verification_template`, and `verification_policy` carrying `checks_required` / `required_check_ids` / `template_check_count`
- The `history` list also supports querying the governance queue directly by `governance_status` / `risk_level` / `approval_status` / `verification_status` / `needs_action`, and additionally returns `governance_summary` and `applied_filters`, so the admin console can directly render the pending-approval / pending-verification workbench

### Recommended positioning of the current database config objects

- `SLAConfig`: `workspace`
- `CustomField`: `workspace`
- `AppIntegration`: `workspace`
- To be added later:
  - `TenantConfig`
  - `WorkspaceConfig`
  - Or finer-grained `AIConfigOverride` / `RoutingPolicyConfig`

## Audit and Rollback Constraints

### System-level config

- Modification entries should be treated as ops changes
- Must leave traces via deployment records, Git changes, or config-center version records
- Direct plaintext modification of system secrets in admin APIs is forbidden

### Tenant / Workspace config

- Must go through the admin write API
- Must record audit logs
- Must support reading the most recent effective version
- Before/after snapshots should be retained around modifications

### Runtime config

- Not persisted by default
- If it is a high-risk action, e.g. forcing a provider switch, bypassing fallback, switching knowledge source, it should at least record audit or structured logs

### Rollback

- system-level config is handled via config version / release rollback
- tenant / workspace config is handled via database version snapshots or "restore previous version"
- runtime has no general rollback, only re-initiating execution

## Current Gaps

- `TenantConfig` / `WorkspaceConfig` already exist (`internal/models/models.go`), and the public portal read path is integrated (see above); later, finer-grained `AIConfigOverride` / `RoutingPolicyConfig` can be extended on this basis
- Viper + `config.yml` still carries some business defaults that should later sink to tenant/workspace
- `portal` is still system-level config, not yet tenant/workspace-ified
- AI / knowledge / routing tenant/workspace overrides currently have rules defined, but not yet unified implementation
- `DailyStats` is still a system-level global aggregation, without tenant/workspace dimensions

## Template Governance and Environment Strictness (P2-2)

**Template drift gate**: `apps/server/internal/config/config_template_lint_test.go`
aligns the repo's four templates (`config.yml`, `config.staging.example.yml`,
`config.production.secure.example.yml`, `config.weknora.yml`) with the
`Config` structure's yaml schema:

1. **Unknown keys fail directly** — viper silently ignores keys outside the schema, a typo silently
   falls back to the default; this gate makes it fail explicitly at CI time. When adding config items, change the structure,
   change the templates, the test covers it automatically.
2. **The `database` section and `jwt.secret` of the staging / production templates must be explicitly
   present and use `${ENV}` placeholders** — template absence falls back to code default dev values and gets
   rejected by the startup validation, the template loses usability; plaintext credentials are not allowed in any template.
3. Template-intentionally-omitted non-sensitive keys (going through `GetDefaultConfig` defaults) are allowed.

**Environment strictness**: `server.environment` supports `dev/development`,
`stage/staging`, `prod/production` and case aliases, normalized at load time
(`canonicalEnvironment`), preventing spelling from bypassing strict validation.

- `development`: known-unsafe defaults (dev JWT secret, dev database password,
  WeKnora default key) only produce warnings, runnable locally.
- `staging` and `production`: any known-unsafe default rejects startup — placeholder
  credentials (`${ENV}` expanding to empty when not injected) are also rejected, this is expected behavior:
  pre-production and production do not allow starting services with placeholder credentials.

**Credential injection path**: secrets in templates are always `${ENV_VAR}` placeholders, the load chain is
`expandEnvVarsInConfig` (expansion before read) → viper parse → `normalizeConfig`
(including environment normalization) → `Validate` strictness tiering → `applyConfigEnvOverrides`
(OPENAI_API_KEY and other explicit environment overrides). Environment variable overrides are only used for system-level config,
do not carry tenant/workspace policy (see "Scope Model").

## Subsequent Implementation Order

1. Add `TenantConfig` / `WorkspaceConfig` documented data models
2. Gradually migrate `portal`, AI provider default, knowledge default, routing policy out of `config.yml`
3. Build a unified resolver for config reads: `runtime -> workspace -> tenant -> system -> default`
4. Add audit snapshots and restore entries for config changes
