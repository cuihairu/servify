package infra

import (
	"testing"

	"servify/apps/server/internal/models"
	customerapp "servify/apps/server/internal/modules/customer/application"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// closeExtraDB 关闭底层连接池：此后任何查询返回 «sql: database is closed»，
// 用于驱动仓储读口的通用错误路径（非 ErrRecordNotFound 分支）。
func closeExtraDB(t *testing.T, db *gorm.DB) {
	t.Helper()
	sqlDB, err := db.DB()
	require.NoError(t, err)
	require.NoError(t, sqlDB.Close())
}

// —— LoadCustomerWithUser 错误路径 ——

// 客户档案查询本身失败（非 NotFound）：错误按 «load customer:» 包装上抛。
func TestDataBoundaryExtraLoadCustomerError(t *testing.T) {
	db := newBoundaryDB(t)
	repo := NewGormDataBoundaryRepository(db)
	closeExtraDB(t, db)

	_, _, err := repo.LoadCustomerWithUser(boundaryScopeCtx(), 1)
	require.Error(t, err)
	require.ErrorContains(t, err, "load customer:")
	assert.NotErrorIs(t, err, customerapp.ErrCustomerNotFound)
}

// 客户档案存在但其 user 行丢失：读口以 ErrCustomerNotFound 兜底（404 语义）。
func TestDataBoundaryExtraLoadMissingUser(t *testing.T) {
	db := newBoundaryDB(t)
	repo := NewGormDataBoundaryRepository(db)
	ghost := &models.Customer{UserID: 424242, Company: "Ghost", TenantID: "t9", WorkspaceID: "w9"}
	require.NoError(t, db.Create(ghost).Error)

	_, _, err := repo.LoadCustomerWithUser(boundaryScopeCtx(), ghost.ID)
	require.ErrorIs(t, err, customerapp.ErrCustomerNotFound)
}

// user 行查询失败（非 NotFound）：错误按 «load customer user:» 包装上抛。
func TestDataBoundaryExtraLoadUserError(t *testing.T) {
	db := newBoundaryDB(t)
	repo := NewGormDataBoundaryRepository(db)
	_, customer := seedBoundaryData(t, db)
	require.NoError(t, db.Exec("DROP TABLE users").Error)

	_, _, err := repo.LoadCustomerWithUser(boundaryScopeCtx(), customer.ID)
	require.Error(t, err)
	require.ErrorContains(t, err, "load customer user:")
	assert.NotErrorIs(t, err, customerapp.ErrCustomerNotFound)
}

// —— 列表读口：空集合直返 nil，查询失败按语义包装 ——

func TestDataBoundaryExtraListEmptySets(t *testing.T) {
	db := newBoundaryDB(t)
	repo := NewGormDataBoundaryRepository(db)
	ctx := boundaryScopeCtx()

	messages, err := repo.ListMessagesBySessions(ctx, nil)
	require.NoError(t, err)
	assert.Empty(t, messages)

	comments, err := repo.ListCommentsByTickets(ctx, nil)
	require.NoError(t, err)
	assert.Empty(t, comments)

	files, err := repo.ListFilesByTickets(ctx, nil)
	require.NoError(t, err)
	assert.Empty(t, files)
}

// 连接关闭后各列表读口把底层错误按语义包装上抛。
func TestDataBoundaryExtraListErrorPaths(t *testing.T) {
	ctx := boundaryScopeCtx()

	t.Run("list sessions error", func(t *testing.T) {
		db := newBoundaryDB(t)
		repo := NewGormDataBoundaryRepository(db)
		closeExtraDB(t, db)
		_, err := repo.ListSessionsByUser(ctx, 1)
		require.ErrorContains(t, err, "list sessions:")
	})

	t.Run("list messages error", func(t *testing.T) {
		db := newBoundaryDB(t)
		repo := NewGormDataBoundaryRepository(db)
		closeExtraDB(t, db)
		_, err := repo.ListMessagesBySessions(ctx, []string{"sess-x"})
		require.ErrorContains(t, err, "list messages:")
	})

	t.Run("list tickets error", func(t *testing.T) {
		db := newBoundaryDB(t)
		repo := NewGormDataBoundaryRepository(db)
		closeExtraDB(t, db)
		_, err := repo.ListTicketsByUser(ctx, 1)
		require.ErrorContains(t, err, "list tickets:")
	})

	t.Run("list comments error", func(t *testing.T) {
		db := newBoundaryDB(t)
		repo := NewGormDataBoundaryRepository(db)
		closeExtraDB(t, db)
		_, err := repo.ListCommentsByTickets(ctx, []uint{1})
		require.ErrorContains(t, err, "list ticket comments:")
	})

	t.Run("list files error", func(t *testing.T) {
		db := newBoundaryDB(t)
		repo := NewGormDataBoundaryRepository(db)
		closeExtraDB(t, db)
		_, err := repo.ListFilesByTickets(ctx, []uint{1})
		require.ErrorContains(t, err, "list ticket files:")
	})
}

// —— EraseCustomerAndUser 事务内错误路径 ——

// customers 表 UPDATE 被 sqlite 触发器 RAISE(ABORT) 拒绝：
// 事务回滚，错误按 «erase customer:» 包装上抛。
func TestDataBoundaryExtraEraseCustomerError(t *testing.T) {
	db := newBoundaryDB(t)
	repo := NewGormDataBoundaryRepository(db)
	user, customer := seedBoundaryData(t, db)
	require.NoError(t, db.Exec("CREATE TRIGGER boom_customers BEFORE UPDATE ON customers BEGIN SELECT RAISE(ABORT, 'boom-customer-update'); END;").Error)

	err := repo.EraseCustomerAndUser(boundaryScopeCtx(), customer.ID, user.ID)
	require.ErrorContains(t, err, "erase customer:")

	// 回滚：档案保持原样。
	var kept models.Customer
	require.NoError(t, db.First(&kept, customer.ID).Error)
	assert.Equal(t, "Acme", kept.Company)
}

// 仅 users 表 UPDATE 被拒绝：客户档案更新先行成功，事务回滚，
// 错误按 «erase user:» 包装上抛。
func TestDataBoundaryExtraEraseUserError(t *testing.T) {
	db := newBoundaryDB(t)
	repo := NewGormDataBoundaryRepository(db)
	user, customer := seedBoundaryData(t, db)
	require.NoError(t, db.Exec("CREATE TRIGGER boom_users BEFORE UPDATE ON users BEGIN SELECT RAISE(ABORT, 'boom-user-update'); END;").Error)

	err := repo.EraseCustomerAndUser(boundaryScopeCtx(), customer.ID, user.ID)
	require.ErrorContains(t, err, "erase user:")

	// 回滚：身份保持原样。
	var kept models.User
	require.NoError(t, db.First(&kept, user.ID).Error)
	assert.Equal(t, "alice", kept.Username)
}
