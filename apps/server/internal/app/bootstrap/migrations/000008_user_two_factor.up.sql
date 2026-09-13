-- 000008: 用户 TOTP 两步验证（users 三列 + 恢复码独立表）
-- 与 models.User 新列 / models.UserRecoveryCode 逐列对齐；
-- totp_secret 只存 base32 密钥本身，恢复码只存 sha256 原文哈希（一次性原子核销）。

ALTER TABLE "users" ADD COLUMN IF NOT EXISTS "totp_secret" text DEFAULT '';
ALTER TABLE "users" ADD COLUMN IF NOT EXISTS "totp_enabled" boolean DEFAULT false;
ALTER TABLE "users" ADD COLUMN IF NOT EXISTS "totp_enabled_at" timestamptz;

CREATE TABLE IF NOT EXISTS "user_recovery_codes" ("id" bigserial,"user_id" bigint NOT NULL,"code_hash" text NOT NULL,"used_at" timestamptz,"created_at" timestamptz,PRIMARY KEY ("id"));
CREATE INDEX IF NOT EXISTS "idx_user_recovery_codes_user_id" ON "user_recovery_codes" ("user_id");
