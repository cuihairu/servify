package bootstrap

// BackfillTelegramEventIDs（sqlite/AutoMigrate 路径）测试：与 postgres
// versioned 迁移 000031_...up.sql 的语义对账由
// TestTelegramEventIDBackfillSQLCoversSameLogic 把关（两条路径必须同语义，
// 惯例参照 TestScopeBackfillSQLCoversSameTables）。

import (
	"crypto/sha256"
	"fmt"
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

// seedTelegramInbox 落一行 inbox 行，platform/eventType/event_id 可指定。
func seedTelegramInbox(t *testing.T, db *gorm.DB, tenant, workspace, inst, conv, eventID, key, platform, eventType string) {
	t.Helper()
	now := time.Now()
	row := channel.ChatConnectorInbox{
		TenantID:               tenant,
		WorkspaceID:            workspace,
		IdempotencyKey:         key,
		Platform:               platform,
		InstallationID:         inst,
		EventID:                eventID,
		EventType:              eventType,
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
	seedTelegramInbox(t, db, "t1", "w1", "inst-1", "-100123", "7", "inst-1\x1f7", "telegram", "message.created")
	// 已 chat 作用行（不同 message，新格式，必须不动）。
	seedTelegramInbox(t, db, "t1", "w1", "inst-1", "-100123", "-100123:8", "inst-1\x1f-100123:8", "telegram", "message.created")
	// 非 telegram 平台（Memex，带冒号 event id，必须不动）。
	seedTelegramInbox(t, db, "t1", "w1", "inst-1", "conv-x", "m1", "inst-1\x1fm1", "memex", "message.created")
	// 非 message.created（telegram，裸 id，必须不动）。
	seedTelegramInbox(t, db, "t1", "w1", "inst-1", "-100123", "9", "inst-1\x1f9", "telegram", "message.deleted")

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

	// 非 telegram / 非 message.created 行均未被重写。
	var nonTelegram, nonCreated channel.ChatConnectorInbox
	if err := db.Where("id = ?", 3).First(&nonTelegram).Error; err != nil {
		t.Fatalf("load non-telegram row: %v", err)
	}
	if nonTelegram.EventID != "m1" {
		t.Fatalf("non-telegram event_id = %q want m1 (不动)", nonTelegram.EventID)
	}
	if err := db.Where("id = ?", 4).First(&nonCreated).Error; err != nil {
		t.Fatalf("load non-message.created row: %v", err)
	}
	if nonCreated.EventID != "9" {
		t.Fatalf("non-message.created event_id = %q want 9 (不动)", nonCreated.EventID)
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
	seedTelegramInbox(t, db, "t1", "w1", "inst-1", "-100123", "7", "inst-1\x1f7", "telegram", "message.created")
	seedTelegramInbox(t, db, "t1", "w1", "inst-1", "-100123", "-100123:7", "inst-1\x1f-100123:7", "telegram", "message.created")

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
	// 名用短 hash 前缀，避免 t.Name() 拼出的标识符逼近 postgres 63 字节
	// 截断线；SQL 中一律以双引号引用，杜绝未来子测试名带特殊字符拼出
	// 非法 DDL。
	sum := sha256.Sum256([]byte(t.Name()))
	schemaName := "teidbf_" + fmt.Sprintf("%x", sum[:8])
	quotedSchema := `"` + schemaName + `"`
	root, err := gorm.Open(gormpostgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	rootDB, err := root.DB()
	if err != nil {
		t.Fatalf("root db handle: %v", err)
	}
	defer rootDB.Close()
	// 单连接：SET search_path 只作用于当前会话，必须把连接池钉死为 1，
	// 否则后续 DDL/DML 可能落在其他未 SET 的连接上，测试会在错误 schema
	// 上跑出假绿（隔离失效）。
	rootDB.SetMaxOpenConns(1)
	if _, err := rootDB.Exec("CREATE SCHEMA " + quotedSchema); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	defer rootDB.Exec("DROP SCHEMA " + quotedSchema + " CASCADE")

	// 000027 建 chat_connector_inboxes（000031 前置表），置 search_path 到临时 schema。
	createSQL, err := migrationsFS.ReadFile("migrations/000027_chat_connector_inbox.up.sql")
	if err != nil {
		t.Fatalf("read 000027: %v", err)
	}
	setSchema := "SET search_path TO " + quotedSchema + ", public"
	if _, err := rootDB.Exec(setSchema); err != nil {
		t.Fatalf("set search_path: %v", err)
	}
	// search_path 生效对账：隔离失效会在此直接暴露，而非静默落到 public。
	var currentSchema string
	if err := rootDB.QueryRow("SELECT current_schema()").Scan(&currentSchema); err != nil {
		t.Fatalf("current_schema: %v", err)
	}
	if currentSchema != schemaName {
		t.Fatalf("current_schema() = %q, want %q（search_path 隔离失效）", currentSchema, schemaName)
	}
	if _, err := rootDB.Exec(string(createSQL)); err != nil {
		t.Fatalf("exec 000027: %v", err)
	}

	// 000031 的 DELETE/USING 依赖 uq_chat_connector_inboxes_scope_key 的防
	// 冲突语义。该索引由 000027 up.sql 定义，此处断言其存在，防止测试与
	// 生产索引形态漂移；不再手抄副本（漂移会让测试静默测错形态）。
	if !strings.Contains(string(createSQL), "uq_chat_connector_inboxes_scope_key") {
		t.Fatalf("000027 up.sql 未定义 uq_chat_connector_inboxes_scope_key（000031 依赖其防冲突语义）")
	}
	var idxCount int
	if err := rootDB.QueryRow(`SELECT count(*) FROM pg_indexes WHERE schemaname = $1 AND indexname = 'uq_chat_connector_inboxes_scope_key'`, schemaName).Scan(&idxCount); err != nil {
		t.Fatalf("count unique index: %v", err)
	}
	if idxCount != 1 {
		t.Fatalf("uq_chat_connector_inboxes_scope_key 索引缺失（want 1, got %d）", idxCount)
	}

	insert := func(tenant, workspace, inst, conv, eventID, key, platform, eventType string) {
		t.Helper()
		_, err := rootDB.Exec(`INSERT INTO chat_connector_inboxes
			(tenant_id, workspace_id, idempotency_key, platform, installation_id,
			 event_id, event_type, status, conversation_platform_id,
			 sender_platform_id, message_platform_id, occurred_at, received_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7,'committed',$8,'u1',$6,NOW(),NOW())`,
			tenant, workspace, key, platform, inst, eventID, eventType, conv)
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
	// 业务键（tenant+workspace+idempotency_key）定位行，不依赖 serial id 的
	// INSERT 顺序分配——断言与 id 绑定会让失败信息在种子顺序调整/批量
	// 插入时指向错误语义。
	eventIDByKey := func(tenant, workspace, key string) string {
		t.Helper()
		var v string
		if err := rootDB.QueryRow(`SELECT event_id FROM chat_connector_inboxes
			WHERE tenant_id = $1 AND workspace_id = $2 AND idempotency_key = $3`,
			tenant, workspace, key).Scan(&v); err != nil {
			t.Fatalf("load event_id: %v", err)
		}
		return v
	}
	keyByKey := func(tenant, workspace, key string) string {
		t.Helper()
		var v string
		if err := rootDB.QueryRow(`SELECT idempotency_key FROM chat_connector_inboxes
			WHERE tenant_id = $1 AND workspace_id = $2 AND idempotency_key = $3`,
			tenant, workspace, key).Scan(&v); err != nil {
			t.Fatalf("load key: %v", err)
		}
		return v
	}

	// 旧行（裸 messageID）与 chat 作用行同 update 各落一次：目标键已被占用，
	// 旧行应被删除、chat 作用行存活。
	insert("t1", "w1", "inst-1", "-100123", "7", "inst-1\x1f7", "telegram", "message.created")
	insert("t1", "w1", "inst-1", "-100123", "-100123:7", "inst-1\x1f-100123:7", "telegram", "message.created")
	// 已 chat 作用行（不同 message）→ 不动。
	insert("t1", "w1", "inst-1", "-100123", "-100123:8", "inst-1\x1f-100123:8", "telegram", "message.created")
	// 非 telegram 平台（Memex，裸 id）→ 不动。
	insert("t1", "w1", "inst-1", "conv-x", "m1", "inst-1\x1fm1", "memex", "message.created")
	// 非 message.created（telegram，裸 id）→ 不动。
	insert("t1", "w1", "inst-1", "-100123", "9", "inst-1\x1f9", "telegram", "message.deleted")
	// 不同安装同裸 messageID → 其键未被占用，应重写。
	insert("t1", "w1", "inst-2", "-200456", "7", "inst-2\x1f7", "telegram", "message.created")

	upSQL, err := migrationsFS.ReadFile("migrations/000031_chat_connector_inbox_telegram_event_id.up.sql")
	if err != nil {
		t.Fatalf("read 000031: %v", err)
	}
	if _, err := rootDB.Exec(string(upSQL)); err != nil {
		t.Fatalf("exec 000031: %v", err)
	}

	// 旧行被删（目标键被占用），chat 作用行存活；总行数 6-1=5。
	if got := rowCount(); got != 5 {
		t.Fatalf("rows = %d, want 5 (旧行删除)", got)
	}
	if got := eventIDByKey("t1", "w1", "inst-1\x1f-100123:7"); got != "-100123:7" {
		t.Fatalf("chat-scoped event_id = %q, want -100123:7", got)
	}
	// 已 chat 作用不动。
	if got := eventIDByKey("t1", "w1", "inst-1\x1f-100123:8"); got != "-100123:8" {
		t.Fatalf("chat-scoped event_id = %q, want -100123:8", got)
	}
	// 非 telegram 不动。
	if got := eventIDByKey("t1", "w1", "inst-1\x1fm1"); got != "m1" {
		t.Fatalf("non-telegram event_id = %q, want m1", got)
	}
	// 非 message.created 不动。
	if got := eventIDByKey("t1", "w1", "inst-1\x1f9"); got != "9" {
		t.Fatalf("non-message.created event_id = %q, want 9", got)
	}
	// 不同安装裸键 → 重写（重写后 idempotency_key 变为 chat 作用格式）。
	if got := eventIDByKey("t1", "w1", "inst-2\x1f-200456:7"); got != "-200456:7" {
		t.Fatalf("inst-2 event_id = %q, want -200456:7", got)
	}
	if got := keyByKey("t1", "w1", "inst-2\x1f-200456:7"); got != "inst-2\x1f-200456:7" {
		t.Fatalf("inst-2 key = %q, want inst-2\\x1f-200456:7", got)
	}

	// 幂等重跑：零改动。
	if _, err := rootDB.Exec(string(upSQL)); err != nil {
		t.Fatalf("exec 000031 idempotent rerun: %v", err)
	}
	if got := rowCount(); got != 5 {
		t.Fatalf("idempotent rerun rows = %d, want 5", got)
	}
}
