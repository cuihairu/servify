-- 000019: AI 首答持久化与反馈闭环（V1.0 收敛 B3-1b，docs/v1-convergence-plan.md
-- §3.1/§5.3/§8.3）
-- ① ai_answers：一次 AI 首答的查询/答案/置信/产生方式/引用来源快照——反馈
--    闭环与检索分析（top 问答/无命中率/低置信率）的锚表；
-- ② answer_feedbacks："是否有帮助"评价（helpful + 可选意见），对 answer_id。
-- 空库与既有库两态可跑；与 AutoMigrate 路径（models.AIAnswer /
-- models.AnswerFeedback）保持同语义。
--
-- 2026-10-07 表名纠偏：反馈表真名是 answer_feedbacks（复数）——GORM 默认
-- 命名策略对 AnswerFeedback 无 uncountable 豁免，运行时 GORM 仓储与
-- cmd/gen-baseline 空库捕获均落 answer_feedbacks；首版迁移误写单数
-- answer_feedback，postgres 版本化路径上反馈读写必 42P01（sqlite/AutoMigrate
-- 测试路径用真名，绿测掩盖）。单数表只可能来自首版迁移且恒为空表（应用从未
-- 写成功过），DROP 清理；列形（answer_id NOT NULL / confidence decimal）按
-- gen-baseline 捕获对齐。

CREATE TABLE IF NOT EXISTS "ai_answers" ("id" bigserial,"tenant_id" text,"workspace_id" text,"session_id" text,"query" text,"answer" text,"confidence" decimal,"strategy" text,"sources_json" text,"created_at" timestamptz,PRIMARY KEY ("id"));
CREATE INDEX IF NOT EXISTS "idx_ai_answers_tenant_id" ON "ai_answers" ("tenant_id");
CREATE INDEX IF NOT EXISTS "idx_ai_answers_workspace_id" ON "ai_answers" ("workspace_id");
CREATE INDEX IF NOT EXISTS "idx_ai_answers_session_id" ON "ai_answers" ("session_id");
CREATE INDEX IF NOT EXISTS "idx_ai_answers_strategy" ON "ai_answers" ("strategy");
CREATE INDEX IF NOT EXISTS "idx_ai_answers_created_at" ON "ai_answers" ("created_at");

DROP TABLE IF EXISTS "answer_feedback";
CREATE TABLE IF NOT EXISTS "answer_feedbacks" ("id" bigserial,"tenant_id" text,"workspace_id" text,"answer_id" bigint NOT NULL,"helpful" boolean,"comment" text,"created_by" text,"created_at" timestamptz,PRIMARY KEY ("id"));
CREATE INDEX IF NOT EXISTS "idx_answer_feedbacks_tenant_id" ON "answer_feedbacks" ("tenant_id");
CREATE INDEX IF NOT EXISTS "idx_answer_feedbacks_workspace_id" ON "answer_feedbacks" ("workspace_id");
CREATE INDEX IF NOT EXISTS "idx_answer_feedbacks_answer_id" ON "answer_feedbacks" ("answer_id");
CREATE INDEX IF NOT EXISTS "idx_answer_feedbacks_created_at" ON "answer_feedbacks" ("created_at");
