package orchestration

import (
	"context"
	"testing"

	"servify/apps/server/internal/models"
	pushcontract "servify/apps/server/internal/modules/push/contract"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func newPushOrchTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(uniqueMemDSN("file:orchpush_"+t.Name()+"")), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&models.Session{}))
	return db
}

func TestPrepareRegisterPushTokenNilRequest(t *testing.T) {
	o := NewPushOrchestrator(nil)
	_, err := o.PrepareRegisterPushToken(context.Background(), nil)
	require.ErrorContains(t, err, "request required")
}

func TestPrepareRegisterPushTokenRejectsUnknownPlatform(t *testing.T) {
	// platform 白名单在 session 查找之前（下发侧通道映射 ios=APNs /
	// android=FCM 依赖穷举，未知值必须写前拒绝）。
	o := NewPushOrchestrator(nil)
	_, err := o.PrepareRegisterPushToken(context.Background(), &pushcontract.RegisterPushTokenRequest{
		SessionID: "m-1",
		Platform:  "web",
		Token:     "tok",
	})
	require.ErrorContains(t, err, "unsupported platform")
}

func TestPrepareRegisterPushTokenSessionLookupFailure(t *testing.T) {
	db := newPushOrchTestDB(t)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	require.NoError(t, sqlDB.Close()) // 关闭连接模拟底层故障（非 NotFound）
	o := NewPushOrchestrator(db)

	_, err = o.PrepareRegisterPushToken(context.Background(), &pushcontract.RegisterPushTokenRequest{
		SessionID: "m-abc",
		Platform:  "ios",
		Token:     "tok",
	})
	require.ErrorContains(t, err, "session lookup failed")
}

func TestPrepareRegisterPushTokenRequiresExistingSession(t *testing.T) {
	db := newPushOrchTestDB(t)
	o := NewPushOrchestrator(db)

	_, err := o.PrepareRegisterPushToken(context.Background(), &pushcontract.RegisterPushTokenRequest{
		SessionID: "missing-session",
		Platform:  "android",
		Token:     "fcm-token",
	})
	require.ErrorContains(t, err, "session not found")
}

func TestPrepareRegisterPushTokenInheritsSessionScope(t *testing.T) {
	db := newPushOrchTestDB(t)
	require.NoError(t, db.Create(&models.Session{
		ID:          "m-abc123",
		TenantID:    "tenant-1",
		WorkspaceID: "ws-1",
		Status:      "active",
		Platform:    "chat",
	}).Error)
	o := NewPushOrchestrator(db)

	prepared, err := o.PrepareRegisterPushToken(context.Background(), &pushcontract.RegisterPushTokenRequest{
		SessionID: "m-abc123",
		Platform:  "ios",
		Token:     "apns-device-token",
	})
	require.NoError(t, err)

	// scope 从 session 行原样继承（与 realtime 消息持久化、访客工单同口径）。
	require.Equal(t, "m-abc123", prepared.SessionID)
	require.Equal(t, "ios", prepared.Platform)
	require.Equal(t, "apns-device-token", prepared.Token)
	require.Equal(t, "tenant-1", prepared.TenantID)
	require.Equal(t, "ws-1", prepared.WorkspaceID)
}
