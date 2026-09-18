# Servify 备份与恢复（P2-3）

> 最后更新：2026-09-17（P2-3 定稿）
> 维护规则：本文档声明数据资产的备份/恢复口径。**任何改变备份格式、
> 恢复语义或迁移回滚策略的代码变更必须同步更新本文档**；恢复链路由
> 演练锚定（见文末"演练与证据"）。

## 结论

Servify 的恢复口径按部署形态分两轨：

- **sqlite 单机形态**（默认/演示/小型部署）：数据库与上传资产经
  `cmd/dbrecovery` 工具做全量逻辑备份与逐字节校验恢复；对应实现为
  `internal/platform/recovery` 包。
- **postgres 生产形态**：一致性备份走 `pg_dump`/`pg_restore` SOP
  （本文"PostgreSQL 恢复步骤"）。`dbrecovery` 的 db 子命令**显式
  拒绝 postgres 方言**——把 sqlite JSONL 备份误灌生产库比没有备份
  更危险，工具在建立连接前就拦截。

**迁移回滚的唯一口径是"恢复备份"**：版本化迁移不提供 down 文件
（`apps/server/internal/app/bootstrap/migrate_runner.go` 的 embed
注释即该教义），失败迁移不走反向 SQL，而是把数据库恢复到迁移前
的备份点。该口径的 postgres 可执行证据在 CI Integration job 的
"Backup & restore drill (pg_dump/pg_restore)" 步骤：DROP 整库 →
重建 → `pg_restore` → 迁移水位 `8|f` 随备份回来。

## 备份对象与清单格式

### 数据库（sqlite）

`dbrecovery db-backup -out DIR`：

- 每张用户表一个 JSONL 文件（`<table>.jsonl`），值经统一编码
  （`[]byte`/`time.Time` → 字符串），schema 列清单记录在
  `manifest.json` 的每表条目（`name/columns/rows/file/sha256`）。
- `manifest.json` 记录方言、时间、逐表行数、每文件 sha256 与
  **自增序列位置**（`sqlite_sequence`）——恢复后重放序列，防止新
  插入行撞上已恢复主键（演练 checks 的 `db_sequence_no_collision`）。
- 恢复（`db-restore -backup DIR`）在**单连接 + 单事务**内执行：
  先整体校验每个 dump 文件 sha256，再 `PRAGMA foreign_keys=OFF` →
  逐表 DELETE+回灌 → 序列重放 → 提交。任何一步失败整体回滚，
  目标库保持原状。

### 上传文件（uploads）

`dbrecovery files-backup -dir UPLOADS -archive FILE`：

- 目录打成 tar.gz，逐文件记录 `path/size/mode/sha256` 到 manifest
  （默认路径 `<archive>.manifest.json`）。
- 恢复（`files-restore`）先校验归档内字节流的 sha256 与 manifest
  一致再落盘；恢复后用 `VerifyFiles` 对账——缺失/新增/篡改都会被
  点名（演练 checks 的 `files_verify_detects_tamper`）。

### 关键表

todo P2-3 圈定的恢复优先级表（演练逐表对账）：
`users`、`sessions`、`messages`、`tickets`、`audit_logs`。

### 知识文档资产（外部 provider 持有）

知识文档正文不在 servify 备份范围内：接入 Dify/WeKnora 后，文档
内容与其向量索引由 provider 侧持有并依赖 provider 自身的快照/导出
能力；servify 库内只保存映射与元数据（`servify_weknora_mappings`、
`knowledge_sync_logs` 等），随数据库备份一并恢复。**provider 凭据
与 KB ID 记录在配置中，恢复演练不覆盖 provider 侧数据**——这是
P2-3 的明确边界，不是遗漏。

## RPO / RTO

| 形态 | 备份手段 | 建议频率 | RPO | RTO |
| --- | --- | --- | --- | --- |
| sqlite 单机 | `db-backup` + `files-backup` | 每日一次 + 版本升级/迁移前必做 | ≤ 1 天（迁移窗口前为 0） | 分钟级（恢复为单事务 + 文件解包） |
| postgres | `pg_dump -Fc` | 每日一次 + 版本升级/迁移前必做 | ≤ 1 天 | 分钟级（restore 全量回灌） |

备份产物（备份目录、tar.gz 与 manifest）应落到**数据库所在主机之外**
的存储（对象存储/另一台主机）。本仓库只提供备份与恢复的执行工具与
校验语义，不做异地投递。

## sqlite 恢复步骤

```bash
# 备份（迁移/升级前必做）
go run ./cmd/dbrecovery db-backup -dsn /data/servify.db -out /backup/db-$(date +%F)
go run ./cmd/dbrecovery files-backup -dir /data/uploads -archive /backup/files-$(date +%F).tar.gz

# 恢复（停服后执行；工具会整体校验通过才落库）
go run ./cmd/dbrecovery db-restore -dsn /data/servify.db -backup /backup/db-2026-09-17
go run ./cmd/dbrecovery files-restore -archive /backup/files-2026-09-17.tar.gz \
  -manifest /backup/files-2026-09-17.tar.gz.manifest.json -dest /data/uploads
```

## PostgreSQL 恢复步骤

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

# 对账：迁移水位必须等于备份时刻（如 8|f）
docker compose -f infra/compose/docker-compose.yml exec -T postgres \
  psql -U postgres -d servify -tAc "SELECT version, dirty FROM schema_migrations"
```

迁移水位随备份恢复是**特性而非事故**：回滚迁移 = 回到旧 schema +
旧数据的一致快照，恢复后无需也不应再跑迁移补账。若恢复点是更老的
schema，服务启动会按迁移 Runner 正常补跑其后的版本——这同样以备份
恢复为起点，而不是手工 down。

## 失败迁移处置

1. 迁移中断且 `schema_migrations.dirty = true`：**不要**手工改水位，
   直接按上文恢复到迁移前备份。
2. 迁移前无备份：postgres 可尝试手工修复 dirty 事务内的部分变更，
   但该操作不属于受支持口径——唯一的受支持路径是"先有备份再迁移"。
   迁移窗口的操作顺序因此固定为：备份 → 迁移 → 验证
   （`SELECT version, dirty FROM schema_migrations`）。

## 演练与证据

- **sqlite 演练**：`make backup-restore-acceptance`（或直接
  `scripts/test-backup-restore.sh`）。驱动
  `TestBackupRestoreDrillSQLite`：播种关键表 → 备份 → 注入迁移窗口
  增量 + 损坏（篡改消息、清空审计日志、多插工单）→ 恢复 → 逐表
  行数/内容/自增序列对账；上传资产分支做篡改+删除+新增检测与
  字节级恢复。证据落 `scripts/test-results/backup-restore/`
  （summary.txt、db-manifest.json、files-manifest.json、manifest.json），
  manifest 由 `scripts/validate-acceptance-manifest.sh` 的
  `backup-restore` case 逐项校验并随仓库留档。
- **postgres 演练**：CI Integration job（pgvector/pgvector:pg15）
  播种 → `pg_dump -Fc` → 删光 users → `DROP DATABASE WITH (FORCE)`
  → 重建 → `pg_restore` → 校验 `schema_migrations = 8|f` 与行数
  回到备份时刻。
- 两轨任一失败 CI 即红：恢复链路不是文档承诺，是门禁约束。
