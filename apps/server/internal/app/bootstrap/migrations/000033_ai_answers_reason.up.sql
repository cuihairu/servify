-- 2026-11-05: ai_answers.reason 列（AI 无依据回答策略审计：zero_hit /
-- low_confidence / knowledge_unavailable）。可空文本，direct 策略为空串。
-- 与 models.AIAnswer.Reason 逐列对齐；sqlite 路径走 AutoMigrate 不经过本文件。
ALTER TABLE "ai_answers" ADD COLUMN IF NOT EXISTS "reason" text;

CREATE INDEX IF NOT EXISTS "idx_ai_answers_reason" ON "ai_answers" ("reason");
