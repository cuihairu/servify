-- 000009: RAGFlow 租户/工作区作用域配置（对齐 dify_json / we_knora_json 列形态）

ALTER TABLE "tenant_configs" ADD COLUMN IF NOT EXISTS "rag_flow_json" text DEFAULT '';
ALTER TABLE "workspace_configs" ADD COLUMN IF NOT EXISTS "rag_flow_json" text DEFAULT '';
