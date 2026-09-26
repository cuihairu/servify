-- 000014: 远程协助"对方同意"状态机（RA-4）
-- consent_status: pending（发起后待访客表态）/ granted / declined；
-- 存量行为空串 = 未走同意流程（兼容期，不回填）。
-- consent_at: 表态时间。declined 后协助以 ended 收口且录制回写被拒。

ALTER TABLE "remote_assist_sessions" ADD COLUMN IF NOT EXISTS "consent_status" text NOT NULL DEFAULT '';
ALTER TABLE "remote_assist_sessions" ADD COLUMN IF NOT EXISTS "consent_at" timestamptz;
