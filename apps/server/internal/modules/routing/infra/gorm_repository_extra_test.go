package infra

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"testing"
	"time"

	"servify/apps/server/internal/models"
	"servify/apps/server/internal/modules/routing/domain"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRoutingRepoRoutingAssignmentMarshalErrorBranches 覆盖评分审计落库的
// 两个 JSON 序列化错误分支：Factors 注入 NaN 走真实 json.Marshal 失败路径，
// Reasons（[]string 恒成功）仅经 marshalAssignmentJSON 注入点触发。
func TestRoutingRepoRoutingAssignmentMarshalErrorBranches(t *testing.T) {
	repo := NewGormRepository(newRoutingRepoCoverageDB(t))
	ctx := routingCovScope("t1", "w1")
	now := time.Now()

	err := repo.CreateRoutingAssignment(ctx, &domain.RoutingAssignment{
		SessionID: "sess-nan", ToAgentID: 9,
		Factors:    map[string]float64{"load": math.NaN()},
		Reasons:    []string{"负载过高"},
		AssignedAt: now,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "marshal factors")

	orig := marshalAssignmentJSON
	calls := 0
	marshalAssignmentJSON = func(v interface{}) ([]byte, error) {
		calls++
		if calls == 2 { // 第 1 次 Factors 成功，第 2 次 Reasons 失败
			return nil, errors.New("reasons boom")
		}
		return json.Marshal(v)
	}
	t.Cleanup(func() { marshalAssignmentJSON = orig })

	err = repo.CreateRoutingAssignment(ctx, &domain.RoutingAssignment{
		SessionID: "sess-reasons", ToAgentID: 9,
		Factors:    map[string]float64{"skill": 1},
		Reasons:    []string{"技能匹配"},
		AssignedAt: now,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "marshal reasons")
}

// TestRoutingRepoRoutingAssignmentLimitAndFailurePaths 覆盖评分审计读口的
// limit 归一化、Create/List 查询失败分支与 scope 辅助函数的 nil 入参防御。
func TestRoutingRepoRoutingAssignmentLimitAndFailurePaths(t *testing.T) {
	db := newRoutingRepoCoverageDB(t)
	require.NoError(t, db.AutoMigrate(&models.RoutingAssignment{}))
	repo := NewGormRepository(db)
	ctx := routingCovScope("t1", "w1")
	now := time.Now()

	require.NoError(t, repo.CreateRoutingAssignment(ctx, &domain.RoutingAssignment{
		SessionID: "sess-a", ToAgentID: 9, Factors: map[string]float64{"skill": 1},
		Reasons: []string{"ok"}, AssignedAt: now,
	}))

	// limit<=0 归一化为 50。
	items, err := repo.ListRoutingAssignments(ctx, "sess-a", 0)
	require.NoError(t, err)
	require.Len(t, items, 1)

	// 表不存在后 Create / List 均走查询失败分支。
	require.NoError(t, db.Migrator().DropTable(&models.RoutingAssignment{}))

	err = repo.CreateRoutingAssignment(ctx, &domain.RoutingAssignment{
		SessionID: "sess-b", ToAgentID: 9, Factors: map[string]float64{"skill": 1},
		Reasons: []string{"ok"}, AssignedAt: now,
	})
	require.Error(t, err)

	_, err = repo.ListRoutingAssignments(ctx, "sess-a", 10)
	require.Error(t, err)

	// scope 辅助函数 nil 入参直接返回。
	applyRoutingAssignmentScopeFields(context.Background(), nil)
	applyRoutingAssignmentScopeFields(routingCovScope("t9", "w9"), &models.RoutingAssignment{})
}
