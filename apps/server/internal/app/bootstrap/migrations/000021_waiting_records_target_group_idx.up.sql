-- 2026-10-10: waiting_records.target_group_id model-tag index (gorm:"index").
-- 000005 added the column but not the tag-derived index
-- idx_waiting_records_target_group_id, so fresh postgres versioned databases
-- (which never run AutoMigrate) lack it; AutoMigrate/sqlite paths have it.
CREATE INDEX IF NOT EXISTS "idx_waiting_records_target_group_id" ON "waiting_records" ("target_group_id");
