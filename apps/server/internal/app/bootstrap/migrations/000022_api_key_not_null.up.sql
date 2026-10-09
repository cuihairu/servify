-- 2026-10-10: api_keys prefix/key_hash NOT NULL (models/api_key.go gorm:"not null").
-- baeea93 (09-25) added the not null tags after the 000001 baseline capture
-- (same commit), so the baseline columns lack NOT NULL and no later migration
-- re-tightened them — fresh postgres versioned databases allow NULL where the
-- AutoMigrate/sqlite path enforces NOT NULL. SET NOT NULL is safe: business
-- writes always populate both (non-pointer string fields); a legacy NULL row
-- would fail loudly here rather than silently rewritten.
ALTER TABLE "api_keys" ALTER COLUMN "prefix" SET NOT NULL;
ALTER TABLE "api_keys" ALTER COLUMN "key_hash" SET NOT NULL;
