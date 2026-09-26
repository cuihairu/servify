-- 000016: 翻译偏好加 viewer 角色维度（docs/realtime-translation-design.md
-- Phase 1 刀三）。同一会话两个读向各自一条偏好：agent 角色 = 坐席读译文的
-- 目标语言（访客 → 坐席方向，刀一/刀二语义），visitor 角色 = 访客读译文的
-- 目标语言（坐席 → 访客方向，刀三新增）。存量行全部归属 agent 角色；
-- 唯一约束从 conversation_session_id 单列改为 (conversation_session_id,
-- viewer_role) 组合。与 models.TranslationLanguagePreference 逐列对齐；
-- sqlite 路径走 AutoMigrate 不经过本文件（存量 dev 库如带 000015 的旧单列
-- 唯一索引，需手动删除后由 AutoMigrate 重建组合索引）。

ALTER TABLE "translation_language_preferences" ADD COLUMN IF NOT EXISTS "viewer_role" text NOT NULL DEFAULT 'agent';

DROP INDEX IF EXISTS "idx_translation_language_preferences_conversation_session_id";
CREATE UNIQUE INDEX IF NOT EXISTS "idx_translation_prefs_session_viewer" ON "translation_language_preferences" ("conversation_session_id", "viewer_role");
