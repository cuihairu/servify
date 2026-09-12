-- 000002: 出站 Webhook 订阅端点与投递日志（P1 开放平台）
-- 与 models.WebhookEndpoint / models.WebhookDelivery 逐列对齐。

CREATE TABLE IF NOT EXISTS "webhook_endpoints" ("id" bigserial,"created_at" timestamptz,"updated_at" timestamptz,"deleted_at" timestamptz,"tenant_id" text,"workspace_id" text,"name" text NOT NULL,"url" text NOT NULL,"secret" text NOT NULL,"events" text,"description" text,"active" boolean DEFAULT true,PRIMARY KEY ("id"));
CREATE INDEX IF NOT EXISTS "idx_webhook_endpoints_deleted_at" ON "webhook_endpoints" ("deleted_at");

CREATE TABLE IF NOT EXISTS "webhook_deliveries" ("id" bigserial,"created_at" timestamptz,"updated_at" timestamptz,"endpoint_id" bigint,"event_name" text NOT NULL,"event_id" text,"aggregate_id" text,"status" text DEFAULT 'pending',"attempt" bigint DEFAULT 0,"http_status" bigint,"duration_ms" bigint,"last_error" text,"payload" text,"delivered_at" timestamptz,"next_retry_at" timestamptz,PRIMARY KEY ("id"));
CREATE INDEX IF NOT EXISTS "idx_webhook_deliveries_endpoint_id" ON "webhook_deliveries" ("endpoint_id");
CREATE INDEX IF NOT EXISTS "idx_webhook_deliveries_status_next_retry" ON "webhook_deliveries" ("status","next_retry_at");
