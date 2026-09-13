package infra

import (
	"context"
	"testing"

	conversationapp "servify/apps/server/internal/modules/conversation/application"

	"github.com/stretchr/testify/require"
)

// ListSessions 的两条查询错误分支分别打在 Count 与 Find。
func TestGormRepositoryListSessionsQueryErrors(t *testing.T) {
	t.Run("count fails", func(t *testing.T) {
		db := newConversationUnitTestDB(t)
		repo := NewGormRepository(db)
		require.NoError(t, db.Migrator().DropTable("sessions"))

		_, _, err := repo.ListSessions(context.Background(), conversationapp.OpenSessionListQuery{Page: 1, PageSize: 20})
		require.Error(t, err)
	})

	t.Run("find fails", func(t *testing.T) {
		db := newConversationUnitTestDB(t)
		repo := NewGormRepository(db)
		// Count 只做 COUNT(*) 不引用列；Find 带 ORDER BY created_at，缺列即失败。
		require.NoError(t, db.Exec("ALTER TABLE sessions RENAME COLUMN created_at TO created_at_legacy").Error)

		_, _, err := repo.ListSessions(context.Background(), conversationapp.OpenSessionListQuery{Page: 1, PageSize: 20})
		require.ErrorContains(t, err, "created_at")
	})
}
