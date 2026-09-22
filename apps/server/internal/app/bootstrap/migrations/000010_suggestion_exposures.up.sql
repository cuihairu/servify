-- 000010: 客户侧推荐问题曝光/转化归因日志（P2-0 RQ-5，服务端归因口径）
-- 与 models.SuggestionExposureLog 逐列对齐；曝光 = initial/next 接口成功
-- 返回落一行，转化 = 同 session 后续客户消息与最近一次未转化曝光的
-- questions 规范化匹配命中（converted_question 非空即转化）。

CREATE TABLE IF NOT EXISTS "suggestion_exposure_logs" ("id" bigserial,"session_id" text DEFAULT '',"kind" text,"strategy" text,"questions" text,"converted_question" text DEFAULT '',"converted_at" timestamptz,"created_at" timestamptz,PRIMARY KEY ("id"));
CREATE INDEX IF NOT EXISTS "idx_suggestion_exposure_logs_session_id" ON "suggestion_exposure_logs" ("session_id");
