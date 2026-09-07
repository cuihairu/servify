package delivery

import (
	"context"
	"testing"

	"servify/apps/server/internal/models"
	platformauth "servify/apps/server/internal/platform/auth"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func newTransferAdapterTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:agent_delivery_" + t.Name() + "?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.Agent{}))
	return db
}

func loadOf(t *testing.T, db *gorm.DB, userID uint) int {
	t.Helper()
	var agent models.Agent
	require.NoError(t, db.First(&agent, "user_id = ?", userID).Error)
	return agent.CurrentLoad
}

func TestNewTransferRuntimeAdapter(t *testing.T) {
	adapter := NewTransferRuntimeAdapter()
	require.NotNil(t, adapter)
	var _ RuntimeService = adapter
}

func TestTransferRuntimeAdapterSyncTransferLoadWithoutFromAgent(t *testing.T) {
	db := newTransferAdapterTestDB(t)
	require.NoError(t, db.Create(&models.Agent{UserID: 1, CurrentLoad: 2}).Error)

	adapter := NewTransferRuntimeAdapter()
	require.NoError(t, adapter.SyncTransferLoad(context.Background(), db, nil, 1))
	require.Equal(t, 3, loadOf(t, db, 1))
}

func TestTransferRuntimeAdapterSyncTransferLoadBetweenAgents(t *testing.T) {
	db := newTransferAdapterTestDB(t)
	require.NoError(t, db.Create(&[]models.Agent{
		{UserID: 1, CurrentLoad: 0},
		{UserID: 2, CurrentLoad: 2},
	}).Error)

	adapter := NewTransferRuntimeAdapter()
	from := uint(2)
	require.NoError(t, adapter.SyncTransferLoad(context.Background(), db, &from, 1))
	require.Equal(t, 1, loadOf(t, db, 1))
	require.Equal(t, 1, loadOf(t, db, 2))
}

func TestTransferRuntimeAdapterSyncTransferLoadSameAgent(t *testing.T) {
	db := newTransferAdapterTestDB(t)
	require.NoError(t, db.Create(&models.Agent{UserID: 1, CurrentLoad: 2}).Error)

	adapter := NewTransferRuntimeAdapter()
	from := uint(1)
	require.NoError(t, adapter.SyncTransferLoad(context.Background(), db, &from, 1))
	require.Equal(t, 3, loadOf(t, db, 1))
}

func TestTransferRuntimeAdapterSyncTransferLoadDoesNotGoNegative(t *testing.T) {
	db := newTransferAdapterTestDB(t)
	require.NoError(t, db.Create(&[]models.Agent{
		{UserID: 1, CurrentLoad: 0},
		{UserID: 2, CurrentLoad: 0},
	}).Error)

	adapter := NewTransferRuntimeAdapter()
	from := uint(2)
	require.NoError(t, adapter.SyncTransferLoad(context.Background(), db, &from, 1))
	require.Equal(t, 1, loadOf(t, db, 1))
	require.Equal(t, 0, loadOf(t, db, 2))
}

func TestTransferRuntimeAdapterSyncTransferLoadFromUpdateFailure(t *testing.T) {
	db := newTransferAdapterTestDB(t)
	require.NoError(t, db.Create(&models.Agent{UserID: 1, CurrentLoad: 0}).Error)
	require.NoError(t, db.Migrator().DropTable("agents"))

	adapter := NewTransferRuntimeAdapter()
	from := uint(2)
	err := adapter.SyncTransferLoad(context.Background(), db, &from, 1)
	require.Error(t, err)
}

func TestTransferRuntimeAdapterSyncTransferLoadScopedToTenant(t *testing.T) {
	db := newTransferAdapterTestDB(t)
	require.NoError(t, db.Create(&[]models.Agent{
		{UserID: 1, TenantID: "tenant-a", WorkspaceID: "ws-a", CurrentLoad: 0},
		{UserID: 1, TenantID: "tenant-b", WorkspaceID: "ws-b", CurrentLoad: 5},
	}).Error)

	adapter := NewTransferRuntimeAdapter()
	ctx := platformauth.ContextWithScope(context.Background(), "tenant-a", "ws-a")
	require.NoError(t, adapter.SyncTransferLoad(ctx, db, nil, 1))

	var scoped, other models.Agent
	require.NoError(t, db.First(&scoped, "tenant_id = ?", "tenant-a").Error)
	require.NoError(t, db.First(&other, "tenant_id = ?", "tenant-b").Error)
	require.Equal(t, 1, scoped.CurrentLoad)
	require.Equal(t, 5, other.CurrentLoad)
}
