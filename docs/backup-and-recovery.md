# Servify Backup and Recovery (P2-3)

> Last updated: 2026-09-17 (P2-3 finalized)
> Maintenance rule: this document states the backup/recovery convention for data assets. **Any code change that alters the backup format,
> recovery semantics, or migration rollback strategy must update this document accordingly**; the recovery path is
> anchored by drills (see "Drills and Evidence" at the end).

## Conclusion

Servify's recovery convention splits into two tracks by deployment form:

- **sqlite single-node form** (default/demo/small deployments): the database and uploaded assets undergo a full logical backup and byte-verified restore via the `cmd/dbrecovery` tool; the corresponding implementation is the `internal/platform/recovery` package.
- **postgres production form**: consistent backups go through the `pg_dump`/`pg_restore` SOP (this document's "PostgreSQL restore steps"). The db subcommand of `dbrecovery` **explicitly rejects the postgres dialect** — pouring a sqlite JSONL backup into the production database is more dangerous than having no backup at all, so the tool blocks it before the connection is established.

**The only convention for rolling back a migration is "restore the backup"**: versioned migrations ship no down files (the embed comment in `apps/server/internal/app/bootstrap/migrate_runner.go` is that doctrine), and a failed migration does not run reverse SQL but instead restores the database to the pre-migration backup point. The executable postgres evidence for this convention lives in the CI Integration job's "Backup & restore drill (pg_dump/pg_restore)" step: DROP the whole database → recreate → `pg_restore` → the water level of the migration chain head version (currently `23|f`; CI reconciles the chain head dynamically rather than hardcoding it) comes back with the backup.

## Backup Objects and Manifest Format

### Database (sqlite)

`dbrecovery db-backup -out DIR`:

- One JSONL file per user table (`<table>.jsonl`), values uniformly encoded (`[]byte`/`time.Time` → strings), with the schema column list recorded in each table's entry in `manifest.json` (`name/columns/rows/file/sha256`).
- `manifest.json` records the dialect, timestamp, per-table row counts, per-file sha256, and the **auto-increment sequence position** (`sqlite_sequence`) — the sequence is replayed after restore so that newly inserted rows cannot collide with restored primary keys (the drill check `db_sequence_no_collision`).
- Restore (`db-restore -backup DIR`) runs inside a **single connection + single transaction**: it verifies every dump file's sha256 in full first, then `PRAGMA foreign_keys=OFF` → per-table DELETE + backfill → sequence replay → commit. Any failure rolls the whole thing back, leaving the target database as it was.

### Uploaded files (uploads)

`dbrecovery files-backup -dir UPLOADS -archive FILE`:

- The directory is packed into tar.gz, with `path/size/mode/sha256` recorded per file in the manifest (default path `<archive>.manifest.json`).
- Restore (`files-restore`) first verifies that the sha256 of the byte streams inside the archive match the manifest before writing to disk; after restore, `VerifyFiles` reconciles — missing / added / tampered files are all called out by name (the drill check `files_verify_detects_tamper`).

### Critical tables

The restore-priority tables scoped by todo P2-3 (the drill reconciles them per table):
`users`, `sessions`, `messages`, `tickets`, `audit_logs`.

### Knowledge document assets (held by external providers)

Knowledge document bodies are outside the scope of the servify backup: once Dify/WeKnora is integrated, the document content and its vector index are held on the provider side and rely on the provider's own snapshot/export capability; the servify database keeps only the mappings and metadata (`servify_weknora_mappings`, `knowledge_sync_logs`, etc.), which are restored along with the database backup. **Provider credentials and KB IDs are recorded in configuration, and the restore drill does not cover provider-side data** — this is an explicit boundary of P2-3, not an omission.

## RPO / RTO

| Form | Backup means | Suggested frequency | RPO | RTO |
| --- | --- | --- | --- | --- |
| sqlite single-node | `db-backup` + `files-backup` | Once daily + mandatory before any version upgrade/migration | ≤ 1 day (0 just before the migration window) | Minutes (restore is a single transaction + file unpack) |
| postgres | `pg_dump -Fc` | Once daily + mandatory before any version upgrade/migration | ≤ 1 day | Minutes (restore does a full backfill) |

Backup artifacts (the backup directory, the tar.gz, and the manifest) should land on storage **off the host that holds the database** (object storage / another machine). This repository only provides the tools that execute backup and restore along with the verification semantics; it does not do off-site transfer.

## sqlite Restore Steps

```bash
# 备份（迁移/升级前必做；go.mod 在 apps/server/，统一用 -C 指定模块目录）
go -C apps/server run ./cmd/dbrecovery db-backup -dsn /data/servify.db -out /backup/db-$(date +%F)
go -C apps/server run ./cmd/dbrecovery files-backup -dir /data/uploads -archive /backup/files-$(date +%F).tar.gz

# 恢复（停服后执行；工具会整体校验通过才落库）
go -C apps/server run ./cmd/dbrecovery db-restore -dsn /data/servify.db -backup /backup/db-2026-09-17
go -C apps/server run ./cmd/dbrecovery files-restore -archive /backup/files-2026-09-17.tar.gz \
  -manifest /backup/files-2026-09-17.tar.gz.manifest.json -dest /data/uploads
```

## PostgreSQL Restore Steps

```bash
# 备份
docker compose -f infra/compose/docker-compose.yml exec -T postgres \
  pg_dump -U postgres -Fc servify > backup/servify-$(date +%F).dump

# 恢复（停服；服务占着连接时用 WITH (FORCE)，pg 13+）
docker compose -f infra/compose/docker-compose.yml exec -T postgres \
  psql -U postgres -d postgres -c "DROP DATABASE servify WITH (FORCE)"
docker compose -f infra/compose/docker-compose.yml exec -T postgres \
  psql -U postgres -d postgres -c "CREATE DATABASE servify"
docker compose -f infra/compose/docker-compose.yml exec -T postgres \
  pg_restore -U postgres -d servify --no-owner backup/servify.dump

# 对账：迁移水位必须等于备份时刻（= 备份时的迁移链头部版本，如 19|f）
docker compose -f infra/compose/docker-compose.yml exec -T postgres \
  psql -U postgres -d servify -tAc "SELECT version, dirty FROM schema_migrations"
```

The migration water level returning with the backup is a **feature, not an incident**: rolling back a migration means going back to a consistent snapshot of the old schema plus the old data, so no further migration should or needs to be run to make up the accounts after restore. If the restore point is an older schema, the service startup will run the later versions as normal through the migration Runner — this too starts from a backup restore rather than a manual down.

## Handling a Failed Migration

1. The migration was interrupted and `schema_migrations.dirty = true`: **do not** edit the water level by hand — restore from the pre-migration backup as described above.
2. No backup before the migration: postgres can attempt a manual repair of part of the changes inside the dirty transaction, but that operation is not part of the supported convention — the only supported path is "have a backup first, then migrate". The operation order inside a migration window is therefore fixed: backup → migrate → verify (`SELECT version, dirty FROM schema_migrations`).

## Drills and Evidence

- **sqlite drill**: `make backup-restore-acceptance` (or `scripts/test-backup-restore.sh` directly). Drives `TestBackupRestoreDrillSQLite`: seed the critical tables → backup → inject migration-window deltas + corruption (tamper with messages, clear the audit log, insert extra tickets) → restore → reconcile per-table row counts / content / auto-increment sequence; the uploaded-assets branch performs tamper + delete + add detection and byte-level restore. Evidence lands in `scripts/test-results/backup-restore/` (summary.txt, db-manifest.json, files-manifest.json, manifest.json), and the manifest is validated item by item by the `backup-restore` case in `scripts/validate-acceptance-manifest.sh` and kept in the repository.
- **postgres drill**: the CI Integration job (pgvector/pgvector:pg15) seeds → `pg_dump -Fc` → deletes all users → `DROP DATABASE WITH (FORCE)` → recreates → `pg_restore` → verifies that `schema_migrations` equals the migration chain head version (currently `23|f`; CI takes the chain head dynamically for reconciliation, nothing hardcoded) and that row counts return to the backup point.
- If either track fails, CI goes red: the recovery path is not a promise in documentation, it is a gate constraint.
