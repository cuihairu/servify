-- 000004: 质检记录（P2 质检全链路：规则违规 + LLM 打分 + 人工复核）
-- 与 models.QualityReview 逐列对齐。session_id 唯一是 worker 幂等闩；
-- violations/dimensions 是 JSON 文本列，解析在 Go 侧（sqlite/pg 双轨方言安全）。

CREATE TABLE IF NOT EXISTS "quality_reviews" ("id" bigserial,"created_at" timestamptz,"updated_at" timestamptz,"tenant_id" text,"workspace_id" text,"session_id" text NOT NULL,"customer_id" bigint,"agent_id" bigint,"status" text DEFAULT 'pending',"trigger" text DEFAULT 'worker',"message_count" integer,"duration_seconds" integer,"violations_json" text,"violation_count" integer,"max_severity" text,"dimensions_json" text,"llm_total_score" double precision,"llm_summary" text,"llm_provider" text,"llm_model" text,"attempt_count" integer DEFAULT 0,"next_retry_at" timestamptz,"last_error" text,"manual_score" double precision,"manual_result" text,"review_note" text,"reviewed_by" bigint,"reviewed_at" timestamptz,"scored_at" timestamptz,PRIMARY KEY ("id"));
CREATE UNIQUE INDEX IF NOT EXISTS "uniq_quality_reviews_session" ON "quality_reviews" ("session_id");
CREATE INDEX IF NOT EXISTS "idx_quality_reviews_scope" ON "quality_reviews" ("tenant_id","workspace_id");
CREATE INDEX IF NOT EXISTS "idx_quality_reviews_status" ON "quality_reviews" ("status");
CREATE INDEX IF NOT EXISTS "idx_quality_reviews_retry" ON "quality_reviews" ("next_retry_at");
CREATE INDEX IF NOT EXISTS "idx_quality_reviews_agent_id" ON "quality_reviews" ("agent_id");
CREATE INDEX IF NOT EXISTS "idx_quality_reviews_customer_id" ON "quality_reviews" ("customer_id");
CREATE INDEX IF NOT EXISTS "idx_quality_reviews_max_severity" ON "quality_reviews" ("max_severity");
