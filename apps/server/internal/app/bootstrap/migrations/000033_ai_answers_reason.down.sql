-- 000033: reason 列为可逆增量；仅完整性/审计用途，回滚按教义仍应走备份。
DROP INDEX IF EXISTS "idx_ai_answers_reason";
ALTER TABLE "ai_answers" DROP COLUMN IF EXISTS "reason";
