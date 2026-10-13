-- 2026-10-13: 会话消息 metadata 列（历史译文持久化：跨页缓存按语言键
-- 存储，docs/realtime-translation-design.md §1.5「存储列与跨页缓存留后续刀」
-- 的收口）。JSON 文本列；保留键 translations（值为按语言译文的 JSON 串）
-- 编解码口径见 conversation application 的 Decode/MergeMessageTranslations。
-- 与 models.Message.Metadata 逐列对齐；sqlite 路径走 AutoMigrate 不经过本文件。
ALTER TABLE "messages" ADD COLUMN IF NOT EXISTS "metadata" text;
