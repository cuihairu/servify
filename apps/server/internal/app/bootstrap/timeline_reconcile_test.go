package bootstrap

// ReconcileConversationTimeline（todo 197）测试：业务写入与事件发布之间无
// 事务边界，写库后、发布前崩溃的时间线「会话创建」行靠启动期对账回填。
// 谓词是 (conversation_id, event_type) 不存在，与 event_id 是否为 NULL
// 无关——旧格式 NULL event_id 行同样视为已覆盖。

import (
	"testing"
	"time"

	"servify/apps/server/internal/models"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func newTimelineReconcileDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&models.Session{}, &models.ConversationEvent{}))
	return db
}

func seedReconcileSession(t *testing.T, db *gorm.DB, id string, startedAt time.Time) {
	t.Helper()
	require.NoError(t, db.Create(&models.Session{
		ID:          id,
		Status:      "closed",
		Platform:    "web",
		TenantID:    "tenant-a",
		WorkspaceID: "ws-1",
		StartedAt:   startedAt,
		CreatedAt:   startedAt,
		UpdatedAt:   startedAt,
	}).Error)
}

func countReconcileRows(t *testing.T, db *gorm.DB, sessionID string) []models.ConversationEvent {
	t.Helper()
	var rows []models.ConversationEvent
	require.NoError(t, db.Where("conversation_id = ?", sessionID).
		Order("id ASC").Find(&rows).Error)
	return rows
}

func TestReconcileConversationTimelineBackfillsMissingCreatedRows(t *testing.T) {
	db := newTimelineReconcileDB(t)
	started := time.Date(2026, 11, 9, 8, 30, 0, 0, time.UTC)
	seedReconcileSession(t, db, "conv-normal", started)
	seedReconcileSession(t, db, "conv-gap1", started)
	seedReconcileSession(t, db, "conv-gap2", started)

	// conv-normal 已有正常投影行（真实事件 id）；conv-legacy 是旧格式
	// NULL event_id 行——两者都不该被重复回填。
	require.NoError(t, db.Create(&models.ConversationEvent{
		EventID:        ptrString("evt-real-1"),
		TenantID:       "tenant-a",
		WorkspaceID:    "ws-1",
		ConversationID: "conv-normal",
		EventType:      "conversation.created",
		ActorType:      "system",
		Summary:        "会话创建",
		OccurredAt:     started,
	}).Error)
	require.NoError(t, db.Create(&models.ConversationEvent{
		TenantID:       "tenant-a",
		WorkspaceID:    "ws-1",
		ConversationID: "conv-legacy",
		EventType:      "conversation.created",
		ActorType:      "system",
		Summary:        "会话创建",
		OccurredAt:     started,
	}).Error)
	seedReconcileSession(t, db, "conv-legacy", started)

	touched, err := ReconcileConversationTimeline(db)
	require.NoError(t, err)
	require.EqualValues(t, 2, touched, "only sessions without a created row get backfilled")

	// 回填行内容与投影注册表口径一致（actor=system、summary=会话创建、
	// occurred_at=started_at、确定性 event_id）。
	for _, id := range []string{"conv-gap1", "conv-gap2"} {
		rows := countReconcileRows(t, db, id)
		require.Len(t, rows, 1, id)
		require.NotNil(t, rows[0].EventID)
		require.Equal(t, "reconcile:conversation-created:"+id, *rows[0].EventID)
		require.Equal(t, "conversation.created", rows[0].EventType)
		require.Equal(t, "system", rows[0].ActorType)
		require.Equal(t, "会话创建", rows[0].Summary)
		require.Equal(t, "tenant-a", rows[0].TenantID)
		require.Equal(t, "ws-1", rows[0].WorkspaceID)
		require.True(t, rows[0].OccurredAt.Equal(started), "occurred_at should copy started_at")
	}

	// 已有行的会话不动：conv-normal 仍 1 行且 event_id 原值；conv-legacy
	// 仍 1 行（NULL event_id 行视为已覆盖，不得插出第二条）。
	normal := countReconcileRows(t, db, "conv-normal")
	require.Len(t, normal, 1)
	require.NotNil(t, normal[0].EventID)
	require.Equal(t, "evt-real-1", *normal[0].EventID)
	require.Len(t, countReconcileRows(t, db, "conv-legacy"), 1)

	// 幂等：第二遍 0 行。
	touched, err = ReconcileConversationTimeline(db)
	require.NoError(t, err)
	require.EqualValues(t, 0, touched, "second run must be a no-op")
}

func TestReconcileConversationTimelineEmptySchemaIsNoop(t *testing.T) {
	// skip 模式自管库/全新库无表：静默 0 行，不报错（启动编排不因此中断）。
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	touched, err := ReconcileConversationTimeline(db)
	require.NoError(t, err)
	require.EqualValues(t, 0, touched)
}

func ptrString(v string) *string { return &v }
