package outbox

// ClaimDue 的两条错误路径：候选查询失败与单行抢占更新失败都必须上抛，
// 不能静默返回空批（否则投递器会把库故障当成"无待办"）。

import (
	"context"
	"errors"
	"testing"
	"time"

	"servify/apps/server/internal/models"

	"gorm.io/gorm"
)

func closedOutboxDB(t *testing.T) *GormRepository {
	t.Helper()
	db := newOutboxTestDB(t)
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("db handle: %v", err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatalf("close db: %v", err)
	}
	return NewGormRepository(db)
}

func TestClaimDueCandidateQueryErrorSurfaces(t *testing.T) {
	repo := closedOutboxDB(t)
	claimed, err := repo.ClaimDue(context.Background(), time.Now(), 10)
	if err == nil {
		t.Fatalf("closed db must surface the candidate query error, got %+v", claimed)
	}
}

func TestClaimDueClaimUpdateErrorSurfaces(t *testing.T) {
	base := newOutboxTestDB(t)
	repo := NewGormRepository(base)
	ctx := context.Background()

	row := &models.TicketOutbox{EventID: "evt-claim", EventName: "ticket.created", AggregateID: "ticket:1", Payload: "{}"}
	if err := repo.Append(ctx, row); err != nil {
		t.Fatalf("append: %v", err)
	}

	// 只让 updates 语句失败（候选查询照常返回一行），验证单行抢占的错误分支。
	if err := base.Callback().Update().Before("gorm:update").Register("test:fail_claim", func(tx *gorm.DB) {
		tx.AddError(errors.New("claim update failed"))
	}); err != nil {
		t.Fatalf("register callback: %v", err)
	}

	claimed, err := repo.ClaimDue(ctx, time.Now().Add(time.Second), 10)
	if err == nil {
		t.Fatalf("claim update failure must surface, got %+v", claimed)
	}
}
