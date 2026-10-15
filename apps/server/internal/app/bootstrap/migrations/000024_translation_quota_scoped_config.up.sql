-- 000024: 租户/工作区翻译配额覆盖值列（docs/design/tenant-quota-policy.md
-- §4 configscope 租户覆盖层，对齐 rag_flow_json / session_risk_json 列形态）。
-- YAML 文本列，仅存 {limit} 覆盖值（<=0 视为未覆盖，回落
-- translation.quota.limit 系统默认值）；与
-- models.TenantConfig.TranslationQuotaJSON /
-- models.WorkspaceConfig.TranslationQuotaJSON 逐列对齐，sqlite 路径走
-- AutoMigrate 不经过本文件。
ALTER TABLE "tenant_configs" ADD COLUMN IF NOT EXISTS "translation_quota_json" text DEFAULT '';
ALTER TABLE "workspace_configs" ADD COLUMN IF NOT EXISTS "translation_quota_json" text DEFAULT '';
