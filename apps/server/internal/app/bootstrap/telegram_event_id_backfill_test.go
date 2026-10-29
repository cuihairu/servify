package bootstrap

// BackfillTelegramEventIDs（sqlite/AutoMigrate 路径）测试：与 postgres
// versioned 迁移 000031_...up.sql 的语义对账由
// TestTelegramEventIDBackfillSQLCoversSameLogic 把关（两条路径必须同语义，
// 惯例参照 TestScopeBackfillSQLCoversSameTables）。

import (
	"os"
	"strings"
	"testing"
	"time"

	"servify/apps/server/internal/platform/channel"

	"github.com/glebarez/sqlite"
	gormpostgres "gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func newTelegramBackfillDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("db handle: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)
	if err := db.AutoMigrate(&channel.ChatConnectorInbox{}); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	return db
}

// seedTelegramInbox 落一行 telegram message.created 行，event_id 可指定。
func seedTelegramInbox(t *testing.T, db *gorm.DB, tenant, workspace, inst, conv, eventID, key string) {
	t.Helper()
	now := time.Now()
	row := channel.ChatConnectorInbox{
		TenantID:               tenant,
		WorkspaceID:            workspace,
		IdempotencyKey:         key,
		Platform:               "telegram",
		InstallationID:         inst,
		EventID:                eventID,
		EventType:              string(channel.EventTypeMessageCreated),
		Status:                 channel.InboxStatusCommitted,
		ConversationPlatformID: conv,
		SenderPlatformID:       "u1",
		MessagePlatformID:      eventID,
		OccurredAt:             now,
		ReceivedAt:             now,
	}
	if err := db.Create(&row).Error; err != nil {
		t.Fatalf("seed inbox: %v", err)
	}
}

// 空库：天然 no-op，0 行改动、无错误。
func TestTelegramEventIDBackfillEmptyDatabase(t *testing.T) {
	db := newTelegramBackfillDB(t)
	touched, err := BackfillTelegramEventIDs(db)
	if err != nil {
		t.Fatalf("BackfillTelegramEventIDs() error = %v", err)
	}
	if touched != 0 {
		t.Fatalf("empty db touched %d rows", touched)
	}
}

// 旧格式裸 messageID 行重写为 chatID:messageID，event_id 与 idempotency_key
// 同步；非 telegram / 非 message.created / 已 chat 作用行不动。
func TestTelegramEventIDBackfillExistingRows(t *testing.T) {
	db := newTelegramBackfillDB(t)
	// 旧格式裸 message id 行（legacy key inst\x1f7）。
	seedTelegramInbox(t, db, "t1", "w1", "inst-1", "-100123", "7", "inst-1\x1f7")
	// 已 chat 作用行（不同 message，新格式，必须不动）。
	seedTelegramInbox(t, db, "t1", "w1", "inst-1", "-100123", "-100123:8", "inst-1\x1f-100123:8")
	// 非 telegram 平台（Memex，带冒号 event id，必须不动）。
	seedTelegramInbox(t, db, "t1", "w1", "inst-1", "conv-x", "m1", "inst-1\x1fm1")

	touched, err := BackfillTelegramEventIDs(db)
	if err != nil {
		t.Fatalf("BackfillTelegramEventIDs() error = %v", err)
	}
	if touched != 1 {
		t.Fatalf("touched = %d want 1", touched)
	}

	var legacy channel.ChatConnectorInbox
	if err := db.Where("id = ?", 1).First(&legacy).Error; err != nil {
		t.Fatalf("load legacy row: %v", err)
	}
	if legacy.EventID != "-100123:7" {
		t.Fatalf("legacy event_id = %q want -100123:7", legacy.EventID)
	}
	if legacy.IdempotencyKey != "inst-1\x1f-100123:7" {
		t.Fatalf("legacy key = %q want inst-1\\x1f-100123:7", legacy.IdempotencyKey)
	}

	// 幂等：重跑零改动。
	second, err := BackfillTelegramEventIDs(db)
	if err != nil {
		t.Fatalf("second BackfillTelegramEventIDs() error = %v", err)
	}
	if second != 0 {
		t.Fatalf("idempotent rerun touched %d rows", second)
	}
}

// 目标键已被 chat 作用行占用：旧行删除，存活行不变（该 update 在格式改动
// 前后各落过一次，存活的是进入正常流的那次）。
func TestTelegramEventIDBackfillOccupiedTargetDeleted(t *testing.T) {
	db := newTelegramBackfillDB(t)
	// 旧行（id=1）与新格式行（id=2）同 update 各落一次：新格式键已占用。
	seedTelegramInbox(t, db, "t1", "w1", "inst-1", "-100123", "7", "inst-1\x1f7")
	seedTelegramInbox(t, db, "t1", "w1", "inst-1", "-100123", "-100123:7", "inst-1\x1f-100123:7")

	touched, err := BackfillTelegramEventIDs(db)
	if err != nil {
		t.Fatalf("BackfillTelegramEventIDs() error = %v", err)
	}
	if touched != 1 {
		t.Fatalf("touched = %d want 1 (旧行删除)", touched)
	}
	var count int64
	if err := db.Model(&channel.ChatConnectorInbox{}).Count(&count).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Fatalf("rows = %d want 1 (旧行删除，新格式行存活)", count)
	}
	var survivor channel.ChatConnectorInbox
	if err := db.First(&survivor).Error; err != nil {
		t.Fatalf("load survivor: %v", err)
	}
	if survivor.EventID != "-100123:7" {
		t.Fatalf("survivor event_id = %q want -100123:7", survivor.EventID)
	}
}

// nil 防御：nil db 返回 0 不 panic。
func TestTelegramEventIDBackfillNilDB(t *testing.T) {
	touched, err := BackfillTelegramEventIDs(nil)
	if err != nil {
		t.Fatalf("BackfillTelegramEventIDs(nil) error = %v", err)
	}
	if touched != 0 {
		t.Fatalf("nil db touched %d rows", touched)
	}
}

// TestTelegramEventIDBackfillSQLCoversSameLogic 把关 versioned SQL 迁移
// （postgres 路径）与 Go 回填（sqlite 路径）同语义：SQL 必须提供旧行删除
// 与旧行重写两条语句，且都钉在 telegram/message.created/裸 message id 上。
func TestTelegramEventIDBackfillSQLCoversSameLogic(t *testing.T) {
	sqlBytes, err := migrationsFS.ReadFile("migrations/000031_chat_connector_inbox_telegram_event_id.up.sql")
	if err != nil {
		t.Fatalf("read telegram event id backfill migration: %v", err)
	}
	sqlText := string(sqlBytes)
	for _, want := range []string{
		"DELETE FROM \"chat_connector_inboxes\"",
		"UPDATE \"chat_connector_inboxes\"",
		"legacy.event_id ~ '^[0-9]+$'",
		"legacy.conversation_platform_id || ':' || legacy.event_id",
		"event_id = conversation_platform_id || ':' || event_id",
		"idempotency_key = installation_id || chr(31)",
	} {
		if !strings.Contains(sqlText, want) {
			t.Fatalf("migration 000031 missing %q（与 BackfillTelegramEventIDs 语义不同步）", want)
		}
	}
}

// ---- 真实 postgres 执行 000031 up.sql ----
//
// TestTelegramEventIDBackfillPostgresExecutes 在真实 postgres 上执行
// 000031 迁移 SQL（经 TEST_POSTGRES_DSN 门控，CI Integration job 注入），
// 覆盖子串对账 TestTelegramEventIDBackfillSQLCoversSameLogic 看不见的 SQL
// 联接条件与语义：旧行重写、目标键占用删旧行、非 telegram/非
// message.created/已 chat 作用行不动、幂等重跑零改动、安装/作用域匹配。
// 惯例参照 CI Integration "Schema parity (migrated vs automigrate)" 对真
// postgres 收口 dialector 面。
func TestTelegramEventIDBackfillPostgresExecutes(t *testing.T) {
	dsn := os.Getenv("TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("TEST_POSTGRES_DSN unset; skipping real-postgres migration execution")
	}

	// 临时 schema 隔离：不污染 CI 的 servify 库与 schema_migrations。
	schemaName := "telegram_event_id_backfill_" + strings.ReplaceAll(t.Name(), "/", "_")
	root, err := gorm.Open(gormpostgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	rootDB, err := root.DB()
	if err != nil {
		t.Fatalf("root db handle: %v", err)
	}
	defer rootDB.Close()
	if _, err := rootDB.Exec("CREATE SCHEMA " + schemaName); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	defer rootDB.Exec("DROP SCHEMA " + schemaName + " CASCADE")

	// 000027 建 chat_connector_inboxes（000031 前置表），置 search_path 到临时 schema。
	createSQL, err := migrationsFS.ReadFile("migrations/000027_chat_connector_inbox.up.sql")
	if err != nil {
		t.Fatalf("read 000027: %v", err)
	}
	setSchema := "SET search_path TO " + schemaName + ", public"
	if _, err := rootDB.Exec(setSchema); err != nil {
		t.Fatalf("set search_path: %v", err)
	}
	if _, err := rootDB.Exec(string(createSQL)); err != nil {
		t.Fatalf("exec 000027: %v", err)
	}

	// 唯一索引须存在（000031 的 DELETE/USING 依赖其防冲突语义）。
	if _, err := rootDB.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS "uq_chat_connector_inboxes_scope_key" ON "chat_connector_inboxes" ("tenant_id", "workspace_id", "idempotency_key")`); err != nil {
		t.Fatalf("create unique index: %v", err)
	}

	insert := func(tenant, workspace, inst, conv, eventID, key string) {
		t.Helper()
		_, err := rootDB.Exec(`INSERT INTO chat_connector_inboxes
			(tenant_id, workspace_id, idempotency_key, platform, installation_id,
			 event_id, event_type, status, conversation_platform_id,
			 sender_platform_id, message_platform_id, occurred_at, received_at)
			VALUES ($1,$2,$3,'telegram',$4,$5,'message.created','committed',$6,'u1',$5,NOW(),NOW())`,
			tenant, workspace, key, inst, eventID, conv)
		if err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	rowCount := func() int {
		t.Helper()
		var n int
		if err := rootDB.QueryRow(`SELECT count(*) FROM chat_connector_inboxes`).Scan(&n); err != nil {
			t.Fatalf("count: %v", err)
		}
		return n
	}
	eventIDByID := func(id int) string {
		t.Helper()
		var v string
		if err := rootDB.QueryRow(`SELECT event_id FROM chat_connector_inboxes WHERE id = $1`, id).Scan(&v); err != nil {
			t.Fatalf("load event_id: %v", err)
		}
		return v
	}
	keyByID := func(id int) string {
		t.Helper()
		var v string
		if err := rootDB.QueryRow(`SELECT idempotency_key FROM chat_connector_inboxes WHERE id = $1`, id).Scan(&v); err != nil {
			t.Fatalf("load key: %v", err)
		}
		return v
	}

	// 旧行（id=1, 裸 messageID）与 chat 作用行（id=2）同 update 各落一次：
	// 目标键已被 id=2 占用，id=1 应被删除、id=2 存活。
	insert("t1", "w1", "inst-1", "-100123", "7", "inst-1\x1f7")
	insert("t1", "w1", "inst-1", "-100123", "-100123:7", "inst-1\x1f-100123:7")
	// 已 chat 作用行（不同 message，id=3）→ 不动。
	insert("t1", "w1", "inst-1", "-100123", "-100123:8", "inst-1\x1f-100123:8")
	// 非 telegram 平台（id=4，裸 id）→ 不动。
	insert("t1", "w1", "inst-1", "conv-x", "m1", "inst-1\x1fm1")
	// 不同安装/租户同裸 messageID（id=5）→ 其键 inst-2\x1f7 未被占用，应重写。
	insert("t1", "w1", "inst-2", "-200456", "7", "inst-2\x1f7")

	upSQL, err := migrationsFS.ReadFile("migrations/000031_chat_connector_inbox_telegram_event_id.up.sql")
	if err != nil {
		t.Fatalf("read 000031: %v", err)
	}
	if _, err := rootDB.Exec(string(upSQL)); err != nil {
		t.Fatalf("exec 000031: %v", err)
	}

	// id=1 被删（目标键被占用），id=2 存活；总行数 5-1=4。
	if got := rowCount(); got != 4 {
		t.Fatalf("rows = %d, want 4 (旧行删除)", got)
	}
	var legacyCount int
	if err := rootDB.QueryRow(`SELECT count(*) FROM chat_connector_inboxes WHERE event_id = '7'`).Scan(&legacyCount); err != nil {
		t.Fatalf("count legacy: %v", err)
	}
	if legacyCount != 0 {
		t.Fatalf("legacy rows = %d, want 0 (id=1 删除)", legacyCount)
	}
	if got := eventIDByID(2); got != "-100123:7" {
		t.Fatalf("id=2 event_id = %q, want -100123:7", got)
	}
	// id=3 已 chat 作用不动。
	if got := eventIDByID(3); got != "-100123:8" {
		t.Fatalf("id=3 event_id = %q, want -100123:8", got)
	}
	// id=4 非 telegram 不动。
	if got := eventIDByID(4); got != "m1" {
		t.Fatalf("id=4 event_id = %q, want m1", got)
	}
	// id=5 不同安装裸键 → 重写。
	if got := eventIDByID(5); got != "-200456:7" {
		t.Fatalf("id=5 event_id = %q, want -200456:7", got)
	}
	if got := keyByID(5); got != "inst-2\x1f-200456:7" {
		t.Fatalf("id=5 key = %q, want inst-2\\x1f-200456:7", got)
	}

	// 幂等重跑：零改动。
	if _, err := rootDB.Exec(string(upSQL)); err != nil {
		t.Fatalf("exec 000031 idempotent rerun: %v", err)
	}
	if got := rowCount(); got != 4 {
		t.Fatalf("idempotent rerun rows = %d, want 4", got)
	}
}
