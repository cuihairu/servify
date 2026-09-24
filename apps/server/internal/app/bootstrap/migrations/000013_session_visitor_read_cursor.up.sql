-- 000013: 会话访客已读游标列（M3 移动 SDK 配套 §10 #3——服务端未读数/
-- 已读游标增强，与 §10 #1 消息 ID 单调游标同源）。游标只前进不后退，
-- 未读数 = agent/system 消息中 ID 大于该游标的条数（与 SDK D7 客户端
-- 推导口径一致）。与 models.Session.VisitorReadMessageID 对齐；sqlite
-- 路径走 AutoMigrate 不经过本文件。

ALTER TABLE "sessions" ADD COLUMN "visitor_read_message_id" bigint DEFAULT 0;
