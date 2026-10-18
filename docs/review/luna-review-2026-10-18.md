[0m
[0m$ [0mpwd && rg --files -g '!node_modules' -g '!vendor' | head -250
/home/cui/workspaces/servify
README_KNOWLEDGE.md
todo.md
go.mod
ARCHITECTURE.md
go.work
config.yml
apps/website/assets/img/logo.svg
apps/website/assets/img/rtc-ill.svg
apps/website/assets/img/hero-ill.svg
apps/website/assets/img/ai-ill.svg
apps/website/assets/js/main.js
apps/website/assets/css/style.css
apps/website/404.html
apps/website/wrangler.jsonc
apps/website/index.html
apps/website/robots.txt
apps/website/README.md
apps/website/sitemap.xml
apps/server/go.mod
infra/compose/weknora-mock/Dockerfile
infra/compose/weknora-mock/main_test.go
infra/compose/weknora-mock/main.go
apps/server/cmd/server/server_seams_test.go
apps/server/cmd/server/server_extra_test.go
infra/compose/dify-mock/Dockerfile
TASKS.md
apps/server/cmd/server/main_test.go
apps/server/cmd/server/main.go
infra/compose/docker-compose.coturn.yml
infra/compose/docker-compose.dify.yml
infra/compose/docker-compose.observability.yml
infra/compose/docker-compose.weknora.yml
infra/compose/docker-compose.yml
config.weknora.yml
LICENSE
config.production.secure.example.yml
Dockerfile
go.sum
infra/observability/otel-collector-config.yaml
apps/server/cmd/cli/run.go
apps/server/cmd/cli/cli_shutdown_test.go
apps/server/cmd/cli/cli_run_smoke_test.go
apps/server/cmd/cli/check_security_baseline.go
apps/server/cmd/cli/cli_run_gaps_test.go
apps/server/cmd/cli/version.go
apps/server/cmd/cli/check_security_baseline_test.go
apps/server/cmd/cli/token.go
apps/server/cmd/cli/check_observability_baseline_test.go
README.md
apps/server/cmd/cli/token_decode.go
apps/server/cmd/cli/check_observability_baseline.go
Makefile
apps/server/cmd/cli/cli_extra_test.go
apps/server/cmd/cli/root.go
apps/server/cmd/cli/cli_coverage_test.go
apps/server/cmd/gen-baseline/main_coverage_test.go
apps/server/cmd/gen-baseline/main_test.go
apps/server/cmd/gen-baseline/main.go
generated-assets.manifest
go.work.sum
apps/server/cmd/schema-parity/main_coverage_test.go
apps/server/cmd/schema-parity/main_test.go
apps/server/cmd/schema-parity/main.go
apps/server/cmd/migrate/migrate_backfill_extra_test.go
apps/server/cmd/migrate/migrate_seams_test.go
apps/server/cmd/migrate/main_test.go
apps/server/cmd/migrate/main.go
apps/server/cmd/migrate/migrate_extra_test.go
build.sh
apps/server/cmd/dbrecovery/main_test.go
apps/server/cmd/dbrecovery/main.go
apps/server/cmd/main_test.go
apps/server/cmd/main.go
apps/demo/index.html
apps/demo/README.md
apps/server/internal/version/version.go
apps/server/internal/middleware/wrappers_test.go
apps/server/pkg/dify/client_coverage_test.go
apps/server/internal/middleware/security_headers_test.go
apps/server/pkg/dify/types.go
apps/server/internal/middleware/ratelimit.go
apps/server/pkg/dify/client_gap_test.go
apps/server/internal/middleware/permissions.go
apps/server/pkg/dify/client.go
apps/server/internal/middleware/ratelimit_test.go
apps/server/pkg/dify/client_test.go
apps/server/internal/middleware/auth.go
apps/server/internal/middleware/security_headers.go
apps/server/internal/middleware/ratelimit_unmatched_test.go
apps/server/internal/middleware/audit.go
apps/server/internal/middleware/permissions_test.go
apps/server/internal/middleware/auth_dbresolver_test.go
apps/server/internal/middleware/authorize.go
apps/server/internal/middleware/auth_extended_test.go
apps/server/internal/middleware/auth_rbac_test.go
apps/server/internal/middleware/memdsn_test.go
apps/server/pkg/weknora/client_extra_test.go
deploy/observability/dashboards/servify-service.json
apps/server/pkg/weknora/client_coverage_test.go
deploy/observability/dashboards/servify-business.json
apps/server/pkg/weknora/client_more_test.go
apps/server/pkg/weknora/types.go
apps/server/pkg/weknora/client.go
apps/server/pkg/weknora/client_test.go
deploy/observability/known-gaps.md
deploy/observability/alerts/rules.yaml
deploy/observability/runbook/operational-runbook.md
apps/server/pkg/ragflow/client_coverage_test.go
deploy/observability/slo.md
apps/server/pkg/ragflow/client_test.go
apps/server/internal/app/worker/periodic.go
apps/server/pkg/ragflow/types.go
apps/server/internal/app/worker/jobs.go
apps/server/internal/app/worker/email_poll_worker.go
apps/server/pkg/ragflow/client.go
apps/server/internal/app/worker/quality_review_worker.go
apps/server/internal/app/worker/automation_timer_worker.go
apps/server/internal/app/worker/email_poll_worker_test.go
apps/server/internal/app/worker/webhook_delivery_worker.go
apps/server/internal/app/worker/quality_review_worker_test.go
apps/server/internal/app/worker/doc.go
apps/server/internal/app/worker/workers_extra_test.go
apps/server/internal/app/worker/jobs_coverage_test.go
apps/server/internal/app/worker/jobs_retention_extra_test.go
apps/server/internal/app/worker/jobs_test.go
apps/server/internal/app/worker/survey_email_worker.go
apps/server/internal/app/worker/waiting_queue_worker.go
apps/server/internal/platform/sipws/types.go
apps/server/internal/platform/sipws/adapter_test.go
apps/server/internal/platform/sipws/doc.go
apps/server/internal/platform/sipws/adapter.go
apps/server/pkg/utils/utils_test.go
apps/server/pkg/utils/utils.go
apps/demo-sdk/examples/page-dark.html
apps/demo-sdk/examples/theme.json
apps/demo-sdk/examples/page-light.html
apps/demo-sdk/widget.js
apps/demo-sdk/widget.css
apps/demo-sdk/servify-sdk.esm.js
apps/demo-sdk/README.md
apps/demo-sdk/servify-sdk.umd.js
apps/demo-sdk/index.d.ts
apps/demo-sdk/widget.test.mjs
apps/server/internal/metrics/metrics_test.go
apps/server/internal/metrics/metrics.go
apps/server/internal/metrics/ratelimit_reset_test.go
apps/server/internal/modules/quality/delivery/handler_contract_test.go
apps/server/internal/modules/quality/delivery/handler_contract.go
apps/server/internal/modules/sla/delivery/types.go
apps/server/internal/platform/sip/types.go
apps/server/internal/platform/sip/doc.go
apps/server/internal/platform/sip/voiceprotocol_adapter.go
apps/server/internal/platform/sip/voiceprotocol_adapter_test.go
apps/server/internal/modules/quality/domain/models.go
apps/server/internal/modules/quality/domain/doc.go
apps/server/internal/modules/sla/delivery/contract.go
apps/server/internal/platform/llm/retry.go
apps/server/internal/app/server/security_surface.go
apps/server/internal/app/server/router_public.go
apps/server/internal/app/server/voice_runtime_test.go
apps/server/internal/app/server/db_stats_provider.go
apps/server/internal/app/server/static.go
apps/server/internal/app/server/agent_registry_test.go
apps/server/internal/app/server/runtime_assembly.go
apps/server/internal/app/server/push_outbound_test.go
apps/server/internal/app/server/runtime_extra_test.go
apps/server/internal/platform/llm/local/provider.go
apps/server/internal/app/server/router_management.go
apps/server/internal/platform/llm/local/provider_test.go
apps/server/internal/app/server/ai_scoped_handler_test.go
apps/server/internal/app/server/translation_quota_test.go
apps/server/internal/app/server/llm_runtime_extra_test.go
apps/server/internal/app/server/voice_route_wiring_test.go
apps/server/internal/app/server/ai_scoped_handler.go
apps/server/internal/app/server/ai_scoped_runtime.go
apps/server/internal/app/server/voice_runtime.go
apps/server/internal/app/server/ai_runtime_test.go
apps/server/internal/app/server/ai_runtime.go
apps/server/internal/app/server/server_extra_test.go
apps/server/internal/app/server/sdk_route_contract_test.go
apps/server/internal/app/server/push_outbound.go
apps/server/internal/app/server/survey_mailer.go
apps/server/internal/app/server/health.go
apps/server/internal/app/server/router_realtime.go
apps/server/internal/app/server/middleware_p2_5_test.go
apps/server/internal/app/server/router.go
apps/server/internal/app/server/security_surface_test.go
apps/server/internal/app/server/router_metrics_test.go
apps/server/internal/app/server/translation_preference_route_test.go
apps/server/internal/app/server/router_upload_test.go
apps/server/internal/app/server/static_test.go
apps/server/internal/app/server/session_history_wiring_test.go
apps/server/internal/app/server/routing_transfer_notifier.go
apps/server/internal/app/server/channel_outbound.go
apps/server/internal/app/server/visitor_translation_route_test.go
apps/server/internal/app/server/quality_runtime.go
apps/server/internal/app/server/doc.go
apps/server/internal/app/server/router_auth.go
apps/server/internal/app/server/ai_scoped_extra_test.go
apps/server/internal/app/server/llm_runtime.go
apps/server/internal/app/server/ai_scoped_runtime_test.go
apps/server/internal/app/server/ai_runtime_local_test.go
apps/server/internal/app/server/sdk_route_drift_test.go
apps/server/internal/app/server/router_gap2_test.go
apps/server/internal/app/server/runtime.go
apps/server/internal/app/server/router_auth_test.go
apps/server/internal/app/server/channel_outbound_test.go
apps/server/internal/app/server/translation_quota.go
apps/server/internal/app/server/knowledge_source_test.go
apps/server/internal/app/server/guest_token_wiring_test.go
apps/server/internal/app/server/server_gaps_test.go
apps/server/internal/app/server/knowledge_source.go
apps/server/internal/app/server/middleware.go
apps/server/internal/app/server/memdsn_test.go
apps/server/internal/app/server/llm_runtime_test.go
apps/server/internal/app/server/server_extra2_test.go
apps/server/internal/modules/quality/application/score_provider.go
apps/server/internal/modules/quality/application/rules_test.go
apps/server/internal/modules/quality/application/service_seam_test.go
apps/server/internal/modules/quality/application/rules.go
apps/server/internal/modules/quality/application/service.go
apps/server/internal/modules/quality/application/service_test.go
apps/server/internal/modules/quality/application/doc.go
apps/server/internal/modules/quality/application/service_coverage2_test.go
apps/server/internal/modules/quality/application/repositories.go
apps/server/internal/modules/sla/application/scope.go
apps/server/internal/modules/sla/application/service.go
apps/server/internal/modules/sla/application/sla_helpers_test.go
apps/server/internal/modules/sla/application/repositories.go
apps/server/internal/modules/sla/application/errors.go
apps/server/internal/modules/sla/application/sla_service_test.go
apps/server/internal/platform/llm/mock/provider.go
apps/server/internal/platform/llm/mock/provider_test.go
apps/server/internal/platform/llm/retry_test.go
apps/server/internal/platform/llm/provider.go
apps/server/internal/platform/llm/types.go
apps/server/internal/modules/suggestion/delivery/handler_adapter.go
apps/server/internal/modules/suggestion/delivery/doc.go
apps/server/internal/modules/suggestion/delivery/handler_contract.go
apps/server/internal/modules/suggestion/delivery/handler_adapter_unit_test.go
apps/server/internal/modules/suggestion/delivery/handler_service_integration_test.go
apps/server/internal/modules/suggestion/delivery/memdsn_test.go
apps/admin/tsconfig.json
apps/admin/pnpm-lock.yaml
apps/server/internal/app/bootstrap/app.go
apps/server/internal/app/bootstrap/migrations_parity_test.go
apps/server/internal/app/bootstrap/bootstrap_extra_test.go
sdk/examples/vue/vite.config.ts
apps/server/internal/platform/llm/anthropic/provider.go
apps/server/internal/platform/llm/anthropic/provider_test.go
[0m→ [0mRead todo.md 
[0m✱ [0mGlob "**/*.md" [90min . · 207 matches[0m
[0m✱ [0mGlob "apps/server/**/*.go" [90min . · 300 matches[0m
[0m$ [0mgit status --short && wc -l todo.md README.md ARCHITECTURE.md TASKS.md README_KNOWLEDGE.md
102 todo.md
  564 README.md
  845 ARCHITECTURE.md
   67 TASKS.md
  309 README_KNOWLEDGE.md
 1887 total
[0m→ [0mRead docs/审计-文档一致性.md 
[0m→ [0mRead docs/remote-assistance-current-state.md 
[0m→ [0mRead docs/current-architecture.md 
[0m→ [0mRead docs/v1-product-scope.md 
[0m→ [0mRead ARCHITECTURE.md [90m[limit=260][0m
[0m→ [0mRead docs/tenant-workspace-boundaries.md 
[0m✱ [0mGrep "TODO|FIXME|XXX|未实现|not implemented" [90min apps/server · 39 matches[0m
[0m✱ [0mGrep "TODO|未完成|not implemented|still missing|not yet" [90min docs · 66 matches[0m
[0m✱ [0mGrep "translation_quota|QuotaGate|fail-open|failOpen" [90min apps/server/internal · 100 matches[0m
[0m✱ [0mGrep "http://|localhost|0\.0\.0\.0|InsecureSkipVerify|AllowAll|AllowOrigins|origin" [90min apps/server/internal · 120 matches[0m
[0m$ [0mfind . -type f \( -name '*.go' -o -name '*.ts' -o -name '*.tsx' -o -name '*.swift' -o -name '*.kt' -o -name '*.js' \) -not -path '*/node_modules/*' | wc -l
2630
[0m→ [0mRead docs/configuration-scopes.md [90m[offset=260, limit=70][0m
[0m→ [0mRead apps/server/internal/modules/translation/delivery/quota_gate.go 
[0m→ [0mRead docs/design/tenant-quota-policy.md [90m[offset=1, limit=220][0m
[0m→ [0mRead apps/server/internal/handlers/scoped_config_handler.go [90m[offset=1450, limit=220][0m
[0m→ [0mRead apps/server/internal/platform/configscope/resolver.go 
[0m→ [0mRead apps/server/internal/app/server/translation_quota.go 
[0m✗ [0mRead apps/server/internal/platform/quotameter/meter.go failed
[91m[1mError: [0mFile not found: /home/cui/workspaces/servify/apps/server/internal/platform/quotameter/meter.go

Did you mean one of these?
/home/cui/workspaces/servify/apps/server/internal/platform/quotameter/quotameter.go
[0m→ [0mRead sdk/PROTOCOL.md [90m[offset=1, limit=130][0m
[0m→ [0mRead docs/remote-assistance-status.md [90m[offset=1, limit=190][0m
[0m→ [0mRead sdk/packages/core/src/bindings/mobile.ts 
[0m→ [0mRead apps/server/internal/modules/translation/delivery/quota_gate_test.go [90m[limit=230][0m
[0m→ [0mRead apps/server/internal/modules/ai/delivery/enhanced_service.go [90m[offset=120, limit=100][0m
[0m→ [0mRead apps/server/internal/handlers/upload_handler.go 
[0m→ [0mRead apps/server/internal/middleware/ratelimit.go 
[0m→ [0mRead apps/server/internal/platform/quotameter/quotameter.go 
[0m→ [0mRead apps/server/internal/app/server/router_public.go [90m[limit=180][0m
[0m→ [0mRead apps/server/internal/app/server/router_auth.go [90m[offset=1, limit=190][0m
[0m→ [0mRead apps/server/internal/platform/realtime/webrtc_service.go [90m[offset=1, limit=180][0m
[0m✱ [0mGrep "KeyHeader:|key_header:|key_header" [90min . · 8 matches[0m
[0m✱ [0mGrep "SetTrustedProxies|TrustedProxies|ForwardedByClientIP|RemoteIPHeaders" [90min apps/server · 0 matches[0m
[0m→ [0mRead apps/server/internal/config/config.go [90m[offset=400, limit=115][0m
[0m→ [0mRead docs/security-baseline-operations.md [90m[offset=1, limit=210][0m
[0m→ [0mRead docs/zh/tenant-workspace-boundaries.md [90m[offset=165, limit=50][0m
[0m→ [0mRead docs/zh/configuration-scopes.md [90m[offset=255, limit=55][0m
[0m→ [0mRead config.yml [90m[offset=210, limit=50][0m
[0m→ [0mRead config.production.secure.example.yml [90m[offset=100, limit=30][0m
[0m→ [0mRead apps/server/internal/config/config.go [90m[offset=1020, limit=55][0m
[0m→ [0mRead apps/server/internal/platform/configscope/gorm_store.go [90m[offset=1, limit=170][0m
[0m✱ [0mGrep "TranslationQuota|translation_quota" [90min apps/admin · 0 matches[0m
[0m→ [0mRead apps/server/internal/modules/translation/delivery/quota_gate.go [90m[offset=91, limit=70][0m
[0m→ [0mRead docs/implementation/13-ai-agent-loop.md 
[0m✱ [0mGrep "func \(.*\) ResolveTranslationQuotaLimit|func .*LoadTranslationQuotaConfig" [90min apps/server/internal/platform/configscope · 4 matches[0m
[0m✱ [0mGrep "func local-check|local-check:|release-check" [90min Makefile · 4 matches[0m
[0m✱ [0mGrep "func .*Authenticate|func AuthMiddleware|func RequirePrincipalKinds|func EnforceRequestScope" [90min apps/server/internal · 6 matches[0m
[0m✱ [0mGrep "func \(.*\) Get|func \(.*\) List|tenant_id|workspace_id" [90min apps/server/internal/modules/translation/infra · 3 matches[0m
[0m$ [0mrg --files | wc -l && find apps/admin/src -type f \( -name '*.ts' -o -name '*.tsx' \) | wc -l && find sdk -type f \( -name '*.ts' -o -name '*.tsx' -o -name '*.swift' -o -name '*.kt' \) | wc -l
2256
117
845
[0m→ [0mRead apps/server/internal/middleware/auth.go 
[0m→ [0mRead Makefile [90m[offset=265, limit=45][0m
[0m→ [0mRead apps/server/internal/platform/auth/scope_middleware.go 
[0m→ [0mRead docs/README.md [90m[limit=150][0m
[0m→ [0mRead apps/server/internal/platform/configscope/gorm_provider.go [90m[offset=60, limit=130][0m
[0m→ [0mRead docs/zh/index.md [90m[limit=100][0m
[0m$ [0mgo test ./internal/middleware ./internal/modules/translation/delivery ./internal/platform/configscope ./internal/platform/quotameter
ok  	servify/apps/server/internal/middleware	0.182s
ok  	servify/apps/server/internal/modules/translation/delivery	0.049s
ok  	servify/apps/server/internal/platform/configscope	0.105s
ok  	servify/apps/server/internal/platform/quotameter	4.175s
[0m$ [0mfind apps/server/internal -maxdepth 1 -type d -name services -print; rg --files apps/server/internal | rg '/services/' | head -20; rg -n 'internal/services|services/\*|旧 `services' docs/zh/tenant-workspace-boundaries.md docs/current-architecture.md
docs/current-architecture.md:5:> Snapshot verification: 2026-10-04 (V1.0 convergence refactor B0 batch filed; re-verified and updated after the `v1.0.0` release, aligning statistics closeout status, dependency graph, customer module layering with the day's code). The previous version's statement "the services directory still exists" is outdated: the old `internal/services` was removed entirely in P3-2 with references zeroed, business logic either sinks down to `modules/*/application` with the migration or exists as thin-shell modules (no `domain` layer).
docs/current-architecture.md:96:Target dependency direction (`internal/services` removed entirely in P3-2, the diagram no longer has a services layer):
docs/zh/tenant-workspace-boundaries.md:176:  - 仍保留在旧 `services/*` 中、未完全迁移到 modules 的少量兼容层入口，但当前已覆盖到的 legacy 聚合查询都已有 scoped 回归测试，不再是本轮阻塞
[0m✱ [0mGrep "access_token|visitor token|VisitorToken|ValidateVisitor" [90min apps/server/internal/platform/realtime · 8 matches[0m
[0m→ [0mRead todo.md [90m[offset=75, limit=9][0m
[0m→ [0mRead sdk/PROTOCOL.md [90m[offset=7, limit=15][0m
[0m✱ [0mGrep "access_token|visitor token|visitorToken" [90min apps/server/internal/handlers · 5 matches[0m
[0m✱ [0mGrep "VisitorToken|visitor_token|visitorToken" [90min apps/server/internal · 0 matches[0m
[0m→ [0mRead apps/server/internal/platform/realtime/websocket_hub.go [90m[offset=95, limit=20][0m
[0m→ [0mRead apps/server/internal/platform/realtime/websocket_hub.go [90m[offset=295, limit=25][0m
[0m→ [0mRead docs/zh/mobile-sdk-design.md [90m[offset=290, limit=35][0m
[0m→ [0mRead apps/server/internal/handlers/visitor_guest_session_handler.go [90m[limit=80][0m
[0m✱ [0mGrep "VisitorToken|GuestToken|Validate.*Token|Set.*TokenValidator|With.*Validator" [90min apps/server/internal/app/server · 18 matches[0m
[0m✱ [0mGrep "GuestToken:|guest_token:|Required:.*GuestToken|GuestTokenConfig" [90min apps/server/internal/config/config.go · 4 matches[0m
[0m→ [0mRead apps/server/internal/config/config.go [90m[offset=1228, limit=12][0m
[0m✱ [0mGrep "func Register.*Scoped|RegisterScoped|scoped-config|ScopedConfig" [90min apps/server/internal/handlers/scoped_config_handler.go · 40 matches[0m
[0m✱ [0mGlob "apps/server/internal/platform/realtime/*bridge*test.go" [90min . · 1 match[0m
[0m✱ [0mGrep "TestWebRTC_LoopbackMediaBridge" [90min apps/server/internal/platform/realtime/webrtc_bridge_test.go · 2 matches[0m
### 表一 缺陷

| 问题 | file:line | 为什么是问题 | 建议动作 |
|---|---|---|---|
| **P1：限流器的 key 可由请求方任意指定，且 bucket 永不回收** | `apps/server/internal/middleware/ratelimit.go:66-80,94-110,127-135`；`config.yml:218-225` | 配置 `KeyHeader: X-API-Key` 时，请求头值直接成为限流 key；任意改变该值即可拿到新 bucket。每个新 key 又永久留在 map 中，可绕过限流并持续占用内存。 | 用可信身份（已验证的 API key）或客户端 IP 限流；限制 key 数量并回收过期 bucket；避免把任意请求头当作身份凭据。 |
| **P2：`tokens` 配额不是硬上限，批量翻译甚至不累计用量** | `apps/server/internal/modules/translation/delivery/quota_gate.go:113-129,151-159`；`apps/server/internal/modules/translation/delivery/quota_gate_test.go:132-138,159-170` | 单条翻译先查余额、后调用 provider、再记账；并发请求可同时通过，且单次调用本身可使累计量超过 limit（现有测试明确验证 90 + 20 会得到 110，limit 为 100）。批量翻译只检查、不记账，余额未耗尽时可重复调用。 | 明确将其标注为软阈值，或实现并发预留与事后结算；批量路径增加可计量 usage，或在 `tokens` 模式下拒绝无法计量的批量调用。 |

### 表二 文档与实现不一致

| 文档说法 | 文档位置 | 代码实际 | 代码位置 |
|---|---|---|---|
| WS 握手的 `access_token` 服务端不读取；访客 token 落地后才会消费；Origin 是唯一门槛。 | `sdk/PROTOCOL.md:12-17` | 访客 token 已可在 `security.guest_token.required` 开启时校验；缺失、无效或会话不匹配会返回 401。该校验默认关闭，因此文档应说明是可选配置，而非尚未实现。 | `apps/server/internal/platform/realtime/websocket_hub.go:302-313`；`apps/server/internal/app/server/runtime_assembly.go:226-230`；默认值见 `apps/server/internal/config/config.go:1232-1235` |
| Portal 仍是 system 级配置，尚未 tenant/workspace 化。 | `docs/zh/configuration-scopes.md:297-303` | Portal 读取已通过 resolver 合并 system、tenant、workspace 配置；public route 已装配两类 scoped provider。 | `apps/server/internal/app/server/router_public.go:16-21`；`apps/server/internal/platform/configscope/resolver.go:154-174` |
| AI / knowledge / routing 的 tenant/workspace override 尚未统一实现。 | `docs/zh/configuration-scopes.md:301-303` | 这已不是“尚未实现”的整体状态：configscope 已有 OpenAI、Dify、WeKnora、RAGFlow 的 tenant/workspace provider 和 resolver；翻译配额也支持 tenant/workspace limit 解析。各能力覆盖范围仍不相同，文档宜标为“部分落地”，并列出尚缺项。 | `apps/server/internal/platform/configscope/gorm_provider.go:27-86,127-178`；`apps/server/internal/platform/configscope/resolver.go:221-302,331-350` |

### 表三 值得补的能力

| 能力 | 为什么现在没有 | 价值高低 |
|---|---|---|
| 真实 Dify / WeKnora / RAGFlow 服务验收 | 当前缺少真实远端端点与凭证；todo 记录 real 模式验收仍受此阻塞。 | 高：补齐生产 provider 的端到端证据。`todo.md:7` |
| 浏览器双端真实媒体验收 | 已有服务端双 PeerConnection / RTP loopback 测试，但浏览器级 `getDisplayMedia` 到管理端画面的验收脚本尚缺。 | 高：验证真实浏览器、设备与信令链路。`docs/remote-assistance-status.md:16,151`；loopback 测试见 `apps/server/internal/platform/realtime/webrtc_bridge_test.go:562-565` |
| Scoped config 管理端表单、校验与审计视图 | 服务端已有 scoped config 写入口，但文档明确管理端尚无对应前端写面。 | 高：减少直接调用 API 的运维风险。`docs/design/tenant-quota-policy.md:166-178`；写入口见 `apps/server/internal/handlers/scoped_config_handler.go:58-68,167-178` |
| 移动端语音上行与字幕消费 | 当前 mobile capability 将 voice 置为 disabled/reserved，协议也明确移动端 V1 不发送、不消费语音帧；待产品决策翻转范围。 | 中：扩展移动端场景，但受产品范围约束。`sdk/packages/core/src/bindings/mobile.ts:3-14`；`sdk/PROTOCOL.md:35-37`；`todo.md:9` |
| AI agent-loop 的 CustomerLookup、TicketLookup、Handoff 实际接线 | 工具已注册，但生产 assembly 传入 nil ports；todo 记录当前按决策保持不可用。 | 中：可启用真实上下文查询与工具调用；接线前需确认权限和副作用语义。`apps/server/internal/modules/ai/delivery/enhanced_service.go:176-182`；`docs/implementation/13-ai-agent-loop.md:9-10`；`todo.md:19` |
| 严格 token 预算与可配置的计量故障策略 | 当前按已产生 usage 事后计量，批量 token 路径不记账，meter 故障 fail-open；设计文档将其作为当前选择。 | 高：若配额用于成本封顶而非软保护，需要更强计量与超限语义。`apps/server/internal/modules/translation/delivery/quota_gate.go:91-97,113-129,133-159`；`docs/design/tenant-quota-policy.md:71-82,104-112` |
