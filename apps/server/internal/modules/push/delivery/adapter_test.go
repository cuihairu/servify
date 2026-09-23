package delivery

import (
	"context"
	"testing"

	"servify/apps/server/internal/models"
	pushcontract "servify/apps/server/internal/modules/push/contract"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func newPushDeliveryDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(uniqueMemDSN("file:push_delivery_"+t.Name()+"")), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&models.Session{}, &models.PushToken{}))
	return db
}

func seedPushSession(t *testing.T, db *gorm.DB, id string) {
	t.Helper()
	require.NoError(t, db.Create(&models.Session{
		ID:          id,
		TenantID:    "tenant-1",
		WorkspaceID: "ws-1",
		Status:      "active",
		Platform:    "chat",
	}).Error)
}

func TestRegisterPushTokenCreatesWithInheritedScope(t *testing.T) {
	db := newPushDeliveryDB(t)
	seedPushSession(t, db, "m-1")
	adapter := NewPushRegistrationAdapter(db)

	reg, err := adapter.RegisterPushToken(context.Background(), &pushcontract.RegisterPushTokenRequest{
		SessionID: "m-1",
		Platform:  "ios",
		Token:     "apns-token-1",
	})
	require.NoError(t, err)
	require.NotZero(t, reg.ID)
	require.Equal(t, "m-1", reg.SessionID)
	require.Equal(t, "ios", reg.Platform)
	require.False(t, reg.UpdatedAt.IsZero())

	// 落库行：scope 继承自 session，token 持久化供下发侧消费。
	var row models.PushToken
	require.NoError(t, db.First(&row, reg.ID).Error)
	require.Equal(t, "tenant-1", row.TenantID)
	require.Equal(t, "ws-1", row.WorkspaceID)
	require.Equal(t, "apns-token-1", row.Token)
}

func TestRegisterPushTokenIsIdempotentPerSessionPlatform(t *testing.T) {
	// 同 (session_id, platform) 重复注册：保活同一行（不重复建行），token 刷新；
	// 同 session 换平台（ios→android）是另一条注册面（双端可并存）。
	db := newPushDeliveryDB(t)
	seedPushSession(t, db, "m-1")
	adapter := NewPushRegistrationAdapter(db)

	first, err := adapter.RegisterPushToken(context.Background(), &pushcontract.RegisterPushTokenRequest{
		SessionID: "m-1", Platform: "ios", Token: "tok-old",
	})
	require.NoError(t, err)

	second, err := adapter.RegisterPushToken(context.Background(), &pushcontract.RegisterPushTokenRequest{
		SessionID: "m-1", Platform: "ios", Token: "tok-new",
	})
	require.NoError(t, err)
	require.Equal(t, first.ID, second.ID, "同 session+platform 必须复用同一行")

	android, err := adapter.RegisterPushToken(context.Background(), &pushcontract.RegisterPushTokenRequest{
		SessionID: "m-1", Platform: "android", Token: "fcm-token",
	})
	require.NoError(t, err)
	require.NotEqual(t, first.ID, android.ID, "不同 platform 是独立注册面")

	var count int64
	require.NoError(t, db.Model(&models.PushToken{}).Count(&count).Error)
	require.Equal(t, int64(2), count)

	var iosRow models.PushToken
	require.NoError(t, db.Where("session_id = ? AND platform = ?", "m-1", "ios").First(&iosRow).Error)
	require.Equal(t, "tok-new", iosRow.Token)
}

func TestRegisterPushTokenRejectsMissingSession(t *testing.T) {
	db := newPushDeliveryDB(t)
	adapter := NewPushRegistrationAdapter(db)

	_, err := adapter.RegisterPushToken(context.Background(), &pushcontract.RegisterPushTokenRequest{
		SessionID: "m-gone", Platform: "ios", Token: "tok",
	})
	require.ErrorContains(t, err, "session not found")
}

func TestRegisterPushTokenRejectsUnknownPlatform(t *testing.T) {
	db := newPushDeliveryDB(t)
	seedPushSession(t, db, "m-1")
	adapter := NewPushRegistrationAdapter(db)

	_, err := adapter.RegisterPushToken(context.Background(), &pushcontract.RegisterPushTokenRequest{
		SessionID: "m-1", Platform: "web", Token: "tok",
	})
	require.ErrorContains(t, err, "unsupported platform")
	var count int64
	require.NoError(t, db.Model(&models.PushToken{}).Count(&count).Error)
	require.Equal(t, int64(0), count, "白名单拒绝不得落行")
}

func TestRegisterPushTokenPropagatesUpdateFailure(t *testing.T) {
	// 幂等保活分支（First 命中 → Updates）的写失败传播：sqlite 触发器让 UPDATE
	// 必败（session 查找正常——失败点精确落在 delivery 的更新语句上）。
	db := newPushDeliveryDB(t)
	seedPushSession(t, db, "m-1")
	adapter := NewPushRegistrationAdapter(db)
	_, err := adapter.RegisterPushToken(context.Background(), &pushcontract.RegisterPushTokenRequest{
		SessionID: "m-1", Platform: "ios", Token: "tok",
	})
	require.NoError(t, err)
	require.NoError(t, db.Exec(`CREATE TRIGGER push_tokens_block_update BEFORE UPDATE ON push_tokens
		BEGIN SELECT RAISE(ABORT, 'update blocked'); END;`).Error)

	_, err = adapter.RegisterPushToken(context.Background(), &pushcontract.RegisterPushTokenRequest{
		SessionID: "m-1", Platform: "ios", Token: "tok-2",
	})
	require.ErrorContains(t, err, "update blocked")
}

func TestRegisterPushTokenPropagatesCreateFailure(t *testing.T) {
	// 新建分支（First 未命中 → Create）的写失败传播。
	db := newPushDeliveryDB(t)
	seedPushSession(t, db, "m-1")
	adapter := NewPushRegistrationAdapter(db)
	require.NoError(t, db.Exec(`CREATE TRIGGER push_tokens_block_insert BEFORE INSERT ON push_tokens
		BEGIN SELECT RAISE(ABORT, 'insert blocked'); END;`).Error)

	_, err := adapter.RegisterPushToken(context.Background(), &pushcontract.RegisterPushTokenRequest{
		SessionID: "m-1", Platform: "ios", Token: "tok",
	})
	require.ErrorContains(t, err, "insert blocked")
}

func TestRegisterPushTokenPropagatesLookupFailure(t *testing.T) {
	// 幂等查行（First）非 NotFound 错误的传播分支：删表让查行必败——
	// 与"session 未找到"（orchestration 层 404 语义）不同源。
	db := newPushDeliveryDB(t)
	seedPushSession(t, db, "m-1")
	adapter := NewPushRegistrationAdapter(db)
	require.NoError(t, db.Exec("DROP TABLE push_tokens").Error)

	_, err := adapter.RegisterPushToken(context.Background(), &pushcontract.RegisterPushTokenRequest{
		SessionID: "m-1", Platform: "ios", Token: "tok",
	})
	require.ErrorContains(t, err, "no such table")
}
