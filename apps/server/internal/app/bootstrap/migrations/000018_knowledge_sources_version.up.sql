-- 000018: Knowledge 产品化（V1.0 收敛 B3-1a，docs/v1-convergence-plan.md §8）
-- ① knowledge_sources 来源登记（markdown/website/pdf/faq/api 元数据，文档挂 source）；
-- ② knowledge_docs 补 source_id（挂源）与 version（版本号，内容变更自增，
--    存量行回填 1 = 初始版本语义，与 Go 侧建文档默认一致）；
-- ③ knowledge_index_jobs 补 document_version（任务执行时索引的文档版本，
--    0 = 未执行到回存）。
-- 幂等：IF NOT EXISTS / IF EXISTS 风格，空库与既有库两态可跑；与
-- AutoMigrate 路径（models.KnowledgeSource / KnowledgeDoc.SourceID /
-- Version / KnowledgeIndexJob.DocumentVersion）保持同语义。

CREATE TABLE IF NOT EXISTS "knowledge_sources" ("id" bigserial,"tenant_id" text,"workspace_id" text,"name" text,"type" text,"description" text,"created_at" timestamptz,"updated_at" timestamptz,PRIMARY KEY ("id"));
CREATE INDEX IF NOT EXISTS "idx_knowledge_sources_tenant_id" ON "knowledge_sources" ("tenant_id");
CREATE INDEX IF NOT EXISTS "idx_knowledge_sources_workspace_id" ON "knowledge_sources" ("workspace_id");
CREATE INDEX IF NOT EXISTS "idx_knowledge_sources_type" ON "knowledge_sources" ("type");

ALTER TABLE "knowledge_docs" ADD COLUMN IF NOT EXISTS "source_id" bigint DEFAULT 0;
ALTER TABLE "knowledge_docs" ADD COLUMN IF NOT EXISTS "version" bigint DEFAULT 1;
-- 既有行版本回填：DEFAULT 只影响新行，存量行 ADD COLUMN 后为 NULL → 归一为 1。
UPDATE "knowledge_docs" SET "version" = 1 WHERE "version" IS NULL OR "version" < 1;
CREATE INDEX IF NOT EXISTS "idx_knowledge_docs_source_id" ON "knowledge_docs" ("source_id");

ALTER TABLE "knowledge_index_jobs" ADD COLUMN IF NOT EXISTS "document_version" bigint DEFAULT 0;
UPDATE "knowledge_index_jobs" SET "document_version" = 0 WHERE "document_version" IS NULL;
