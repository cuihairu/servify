-- 000019: AI 首答持久化与反馈闭环（V1.0 收敛 B3-1b，docs/v1-convergence-plan.md
-- §3.1/§5.3/§8.3）
-- ① ai_answers：一次 AI 首答的查询/答案/置信/产生方式/引用来源快照——反馈
--    闭环与检索分析（top 问答/无命中率/低置信率）的锚表；
-- ② answer_feedback："是否有帮助"评价（helpful + 可选意见），对 answer_id。
-- 空库与既有库两态可跑；与 AutoMigrate 路径（models.AIAnswer /
-- models.AnswerFeedback）保持同语义。

CREATE TABLE IF NOT EXISTS "ai_answers" ("id" bigserial,"tenant_id" text,"workspace_id" text,"session_id" text,"query" text,"answer" text,"confidence" double precision,"strategy" text,"sources_json" text,"created_at" timestamptz,PRIMARY KEY ("id"));
CREATE INDEX IF NOT EXISTS "idx_ai_answers_tenant_id" ON "ai_answers" ("tenant_id");
CREATE INDEX IF NOT EXISTS "idx_ai_answers_workspace_id" ON "ai_answers" ("workspace_id");
CREATE INDEX IF NOT EXISTS "idx_ai_answers_session_id" ON "ai_answers" ("session_id");
CREATE INDEX IF NOT EXISTS "idx_ai_answers_strategy" ON "ai_answers" ("strategy");
CREATE INDEX IF NOT EXISTS "idx_ai_answers_created_at" ON "ai_answers" ("created_at");

CREATE TABLE IF NOT EXISTS "answer_feedback" ("id" bigserial,"tenant_id" text,"workspace_id" text,"answer_id" bigint,"helpful" boolean,"comment" text,"created_by" text,"created_at" timestamptz,PRIMARY KEY ("id"));
CREATE INDEX IF NOT EXISTS "idx_answer_feedback_tenant_id" ON "answer_feedback" ("tenant_id");
CREATE INDEX IF NOT EXISTS "idx_answer_feedback_workspace_id" ON "answer_feedback" ("workspace_id");
CREATE INDEX IF NOT EXISTS "idx_answer_feedback_answer_id" ON "answer_feedback" ("answer_id");
CREATE INDEX IF NOT EXISTS "idx_answer_feedback_created_at" ON "answer_feedback" ("created_at");
