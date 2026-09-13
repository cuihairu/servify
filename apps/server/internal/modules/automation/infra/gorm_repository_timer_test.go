package infra

import (
	"context"
	"testing"
	"time"

	"servify/apps/server/internal/models"
	automationapp "servify/apps/server/internal/modules/automation/application"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func newAutomationTimerUnitDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := newAutomationUnitTestDB(t)
	if err := db.AutoMigrate(&models.AutomationTimer{}); err != nil {
		t.Fatalf("automigrate timers: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Migrator().DropTable(&models.AutomationTimer{})
	})
	return db
}

// 覆盖 CreateTimer / ClaimDueTimers / CompleteTimer / UpdateTimerLastError 的完整生命周期。
func TestGormRepositoryTimerLifecycle(t *testing.T) {
	db := newAutomationTimerUnitDB(t)
	repo := NewGormRepository(db)
	ctx := context.Background()
	now := time.Now()

	timer := &models.AutomationTimer{
		TriggerID:   5,
		TicketID:    9,
		ActionsJSON: `[{"type":"add_tag","params":{"tag":"vip"}}]`,
		DueAt:       now.Add(-time.Minute),
		Status:      automationapp.TimerStatusPending,
		CreatedAt:   now,
	}
	require.NoError(t, repo.CreateTimer(ctx, timer))
	require.NotZero(t, timer.ID)

	// 未到期的不认领
	future := &models.AutomationTimer{
		TriggerID: 6, TicketID: 10, ActionsJSON: "[]",
		DueAt: now.Add(time.Hour), Status: automationapp.TimerStatusPending, CreatedAt: now,
	}
	require.NoError(t, repo.CreateTimer(ctx, future))

	due, err := repo.ClaimDueTimers(ctx, now, 10)
	require.NoError(t, err)
	require.Len(t, due, 1)
	assert.Equal(t, timer.ID, due[0].ID)
	assert.Equal(t, `[{"type":"add_tag","params":{"tag":"vip"}}]`, due[0].ActionsJSON)

	// 乐观抢占：pending -> done 只会发生一次
	assert.True(t, repo.CompleteTimer(ctx, timer.ID, now))
	assert.False(t, repo.CompleteTimer(ctx, timer.ID, now))

	var stored models.AutomationTimer
	require.NoError(t, db.First(&stored, "id = ?", timer.ID).Error)
	assert.Equal(t, automationapp.TimerStatusDone, stored.Status)
	require.NotNil(t, stored.ExecutedAt)

	// 记录失败信息
	require.NoError(t, repo.UpdateTimerLastError(ctx, timer.ID, "boom: action failed"))
	require.NoError(t, db.First(&stored, "id = ?", timer.ID).Error)
	assert.Equal(t, "boom: action failed", stored.LastError)

	// 查询失败分支：表被删
	require.NoError(t, db.Migrator().DropTable(&models.AutomationTimer{}))
	_, err = repo.ClaimDueTimers(ctx, now, 10)
	require.Error(t, err)
}

// ClaimDueTimers 尊重 limit 上限，按 due_at, id 排序。
func TestGormRepositoryClaimDueTimersLimitAndOrder(t *testing.T) {
	db := newAutomationTimerUnitDB(t)
	repo := NewGormRepository(db)
	ctx := context.Background()
	now := time.Now()

	for i := 3; i >= 1; i-- {
		timer := &models.AutomationTimer{
			TriggerID: uint(i), TicketID: uint(i * 10), ActionsJSON: "[]",
			DueAt:  now.Add(time.Duration(i) * -time.Minute),
			Status: automationapp.TimerStatusPending, CreatedAt: now,
		}
		require.NoError(t, repo.CreateTimer(ctx, timer))
	}

	due, err := repo.ClaimDueTimers(ctx, now, 2)
	require.NoError(t, err)
	require.Len(t, due, 2)
	// due_at 升序：最早到期的排最前
	assert.Equal(t, uint(3), due[0].TriggerID)
	assert.Equal(t, uint(2), due[1].TriggerID)
}
