-- 2026-10-10: audit_logs hash chain columns (R2, models.go AuditLog.PrevHash/EntryHash).
-- 70ba271 added the columns to the model but the follow-up migration was never
-- written: sqlite AutoMigrate masked the gap, postgres versioned path misses them.
-- gorm:"size:64;index" -> idx_audit_logs_prev_hash / idx_audit_logs_entry_hash
-- (same convention as the other ;index columns in the 000001 baseline).
ALTER TABLE "audit_logs" ADD COLUMN IF NOT EXISTS "prev_hash" text;
ALTER TABLE "audit_logs" ADD COLUMN IF NOT EXISTS "entry_hash" text;
CREATE INDEX IF NOT EXISTS "idx_audit_logs_prev_hash" ON "audit_logs" ("prev_hash");
CREATE INDEX IF NOT EXISTS "idx_audit_logs_entry_hash" ON "audit_logs" ("entry_hash");
