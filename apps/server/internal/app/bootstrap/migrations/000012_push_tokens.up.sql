-- 000012: 推送 token 注册表（M3 移动 SDK 配套 §10 #5——移动端 SDK 经免认证
-- 访客端点 POST /api/v1/push/register 上报 FCM/APNs token，按 session 绑定
-- 租户 scope；同 (session_id, platform) 幂等，换 token 更新、重复注册保活）。
-- 与 models.PushToken 逐列对齐；sqlite 路径走 AutoMigrate 不经过本文件。

CREATE TABLE IF NOT EXISTS "push_tokens" ("id" bigserial,"tenant_id" text DEFAULT '',"workspace_id" text DEFAULT '',"session_id" text DEFAULT '',"platform" text,"token" text,"created_at" timestamptz,"updated_at" timestamptz,PRIMARY KEY ("id"));
CREATE INDEX IF NOT EXISTS "idx_push_tokens_scope" ON "push_tokens" ("tenant_id", "workspace_id");
CREATE INDEX IF NOT EXISTS "idx_push_tokens_session_platform" ON "push_tokens" ("session_id", "platform");
