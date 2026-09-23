-- 000011: 工单 AI 会话摘要列（M3 移动 SDK 配套 §10 #4——访客工单创建端点
-- 接受 ai_summary，坐席侧管理面列表/详情可见）。与 models.Ticket.AISummary
-- 对齐；sqlite 路径走 AutoMigrate 不经过本文件。

ALTER TABLE "tickets" ADD COLUMN "ai_summary" text;
