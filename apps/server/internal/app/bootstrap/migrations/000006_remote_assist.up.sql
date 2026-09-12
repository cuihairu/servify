-- 000006: 远程协助会话 + 标注（P2 远程协助录制/标注）
-- 与 models.RemoteAssistSession / models.RemoteAssistAnnotation 逐列对齐；
-- 录制文件本体走既有 upload 存储，这里只落 key 与展示元数据。

CREATE TABLE IF NOT EXISTS "remote_assist_sessions" ("id" bigserial,"created_at" timestamptz,"updated_at" timestamptz,"tenant_id" text,"workspace_id" text,"conversation_session_id" text,"agent_user_id" bigint,"status" text DEFAULT 'active',"started_at" timestamptz,"ended_at" timestamptz,"recording_key" text,"recording_mime" text,"recording_duration_ms" bigint DEFAULT 0,"recording_size" bigint DEFAULT 0,PRIMARY KEY ("id"));
CREATE INDEX IF NOT EXISTS "idx_remote_assist_sessions_conversation_session_id" ON "remote_assist_sessions" ("conversation_session_id");
CREATE INDEX IF NOT EXISTS "idx_remote_assist_sessions_status" ON "remote_assist_sessions" ("status");

CREATE TABLE IF NOT EXISTS "remote_assist_annotations" ("id" bigserial,"created_at" timestamptz,"assist_session_id" bigint NOT NULL,"timestamp_ms" bigint DEFAULT 0,"shape" text,"payload" text,"created_by" bigint,PRIMARY KEY ("id"));
CREATE INDEX IF NOT EXISTS "idx_assist_annotations_session" ON "remote_assist_annotations" ("assist_session_id");
