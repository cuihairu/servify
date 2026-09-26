-- 000015: 会话翻译语言偏好（docs/realtime-translation-design.md Phase 1 刀一）
-- 每个客服会话至多一行（conversation_session_id 唯一）；target_lang 为该
-- 会话消息自动翻译的目标语言。hub 消息落库后按该偏好异步翻译并广播
-- message-translated 帧。与 models.TranslationLanguagePreference 逐列对齐；
-- sqlite 路径走 AutoMigrate 不经过本文件。

CREATE TABLE IF NOT EXISTS "translation_language_preferences" ("id" bigserial,"tenant_id" text DEFAULT '',"workspace_id" text DEFAULT '',"conversation_session_id" text,"target_lang" text,"created_at" timestamptz,"updated_at" timestamptz,PRIMARY KEY ("id"));
CREATE UNIQUE INDEX IF NOT EXISTS "idx_translation_language_preferences_conversation_session_id" ON "translation_language_preferences" ("conversation_session_id");
CREATE INDEX IF NOT EXISTS "idx_translation_prefs_scope" ON "translation_language_preferences" ("tenant_id", "workspace_id");
