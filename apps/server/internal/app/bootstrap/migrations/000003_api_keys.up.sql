-- 000003: 开放平台 API Key（service principal 凭据，P1 开放平台）
-- 与 models.APIKey 逐列对齐。库内只存 sha256 哈希（高熵免时序侧通道）。

CREATE TABLE IF NOT EXISTS "api_keys" ("id" bigserial,"created_at" timestamptz,"updated_at" timestamptz,"name" text NOT NULL,"prefix" text,"key_hash" text,"tenant_id" text,"workspace_id" text,"scopes" text,"created_by" text,"last_used_at" timestamptz,"expires_at" timestamptz,"revoked_at" timestamptz,PRIMARY KEY ("id"));
CREATE INDEX IF NOT EXISTS "idx_api_keys_prefix" ON "api_keys" ("prefix");
CREATE UNIQUE INDEX IF NOT EXISTS "idx_api_keys_key_hash" ON "api_keys" ("key_hash");
CREATE INDEX IF NOT EXISTS "idx_api_keys_tenant_id" ON "api_keys" ("tenant_id");
