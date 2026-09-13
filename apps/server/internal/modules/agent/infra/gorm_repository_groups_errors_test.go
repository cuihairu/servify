package infra

import (
	"context"
	"testing"
	"time"

	"servify/apps/server/internal/models"

	"github.com/stretchr/testify/require"
)

func TestGormRepositoryGetLastAgentForCustomerError(t *testing.T) {
	db := newAgentGroupTestDB(t)
	repo := NewGormRepository(db)
	require.NoError(t, db.Migrator().DropTable(&models.Session{}))

	_, err := repo.GetLastAgentForCustomer(context.Background(), 7, time.Now().Add(-time.Hour))
	require.ErrorContains(t, err, "failed to find last agent for customer")
}

func TestGormRepositoryListAgentGroupsError(t *testing.T) {
	db := newAgentGroupTestDB(t)
	repo := NewGormRepository(db)
	require.NoError(t, db.Migrator().DropTable(&models.AgentGroup{}))

	_, err := repo.ListAgentGroups(context.Background())
	require.ErrorContains(t, err, "failed to list agent groups")
}

func TestGormRepositoryUpdateAgentGroupError(t *testing.T) {
	db := newAgentGroupTestDB(t)
	repo := NewGormRepository(db)
	require.NoError(t, db.Migrator().DropTable(&models.AgentGroup{}))

	err := repo.UpdateAgentGroup(context.Background(), &models.AgentGroup{ID: 1, Name: "x"})
	require.ErrorContains(t, err, "failed to update agent group")
}

// DeleteAgentGroup 事务内两条 DELETE 的错误分支分别触发。
func TestGormRepositoryDeleteAgentGroupTransactionErrors(t *testing.T) {
	ctx := context.Background()

	t.Run("delete group fails", func(t *testing.T) {
		db := newAgentGroupTestDB(t)
		repo := NewGormRepository(db)
		require.NoError(t, db.Migrator().DropTable(&models.AgentGroup{}))

		err := repo.DeleteAgentGroup(ctx, 1)
		require.ErrorContains(t, err, "failed to delete agent group")
	})

	t.Run("clear members fails", func(t *testing.T) {
		db := newAgentGroupTestDB(t)
		repo := NewGormRepository(db)
		require.NoError(t, db.Migrator().DropTable(&models.AgentGroupMember{}))

		err := repo.DeleteAgentGroup(ctx, 1)
		require.ErrorContains(t, err, "failed to clear group members")
	})
}

// ReplaceGroupMembers 事务内先清空后插入：drop 表打在清空，rename 列打在插入。
func TestGormRepositoryReplaceGroupMembersTransactionErrors(t *testing.T) {
	ctx := context.Background()

	t.Run("clear members fails", func(t *testing.T) {
		db := newAgentGroupTestDB(t)
		repo := NewGormRepository(db)
		require.NoError(t, db.Migrator().DropTable(&models.AgentGroupMember{}))

		err := repo.ReplaceGroupMembers(ctx, 1, []uint{7})
		require.ErrorContains(t, err, "failed to clear group members")
	})

	t.Run("insert member fails", func(t *testing.T) {
		db := newAgentGroupTestDB(t)
		repo := NewGormRepository(db)
		// 删除语句不引用 agent_user_id 列，可成功；随后的显式列名 INSERT 报缺列。
		require.NoError(t, db.Exec("ALTER TABLE agent_group_members RENAME COLUMN agent_user_id TO agent_user_id_legacy").Error)

		err := repo.ReplaceGroupMembers(ctx, 1, []uint{7, 7, 0})
		require.ErrorContains(t, err, "failed to add group member 7")
	})
}

func TestGormRepositoryListEnabledGroupMemberIDsError(t *testing.T) {
	db := newAgentGroupTestDB(t)
	repo := NewGormRepository(db)
	require.NoError(t, db.Migrator().DropTable(&models.AgentGroup{}))

	_, err := repo.ListEnabledGroupMemberIDs(context.Background(), 1)
	require.ErrorContains(t, err, "failed to get agent group")
}

func TestGormRepositoryListGroupMemberIDsError(t *testing.T) {
	db := newAgentGroupTestDB(t)
	repo := NewGormRepository(db)
	require.NoError(t, db.Migrator().DropTable(&models.AgentGroupMember{}))

	_, err := repo.ListGroupMemberIDs(context.Background(), 1)
	require.ErrorContains(t, err, "failed to list group members")
}
