package bootstrap

// BackfillTelegramEventIDs（sqlite/AutoMigrate 路径）测试：与 postgres
// versioned 迁移 000031_...up.sql 的语义对账由
// TestTelegramEventIDBackfillSQLCoversSameLogic 把关（两条路径必须同语义，
// 惯例参照 TestScopeBackfillSQLCoversSameTables）。

import (
	"strings"
	"testing"
	"time"

	"servify/apps/server/internal/platform/channel"

	"github.com/glebarez/sqlite"
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
