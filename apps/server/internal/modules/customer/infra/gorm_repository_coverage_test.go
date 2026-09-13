package infra

import (
	"errors"
	"testing"
	"time"

	customerapp "servify/apps/server/internal/modules/customer/application"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// 覆盖 AddNote 中「UPDATE 成功执行但 RowsAffected==0」的分支：
// 用 SQLite RAISE(IGNORE) 触发器让行更新被静默跳过（无 error、0 行受影响）。
func TestCustomerRepoAddNoteUpdateAffectsZeroRows(t *testing.T) {
	db := newCustomerRepoTestDB(t)
	repo := NewGormRepository(db)
	now := time.Now()
	seedCustomerRow(t, db, 1, "alice", "", "", now)

	require.NoError(t, db.Exec("CREATE TRIGGER trg_ignore_note_update BEFORE UPDATE OF notes ON customers BEGIN SELECT RAISE(IGNORE); END;").Error)

	err := repo.AddNote(t.Context(), 1, customerapp.CustomerNoteDTO{AuthorID: 7, Content: "ignored", CreatedAt: now})
	require.Error(t, err)
	require.True(t, errors.Is(err, gorm.ErrRecordNotFound), "expected gorm.ErrRecordNotFound wrapper, got %v", err)
}
