package infra

import (
	"context"
	"testing"
	"time"

	"servify/apps/server/internal/models"
	"servify/apps/server/internal/modules/routing/domain"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// B2-1 集成用例（计划书 §6.3）：评分审计落 routing_assignments 后 JSON
// 因子/理由可读回，tenant/workspace scope 字段落库口径与 transfer_records
// 一致；列表按会话过滤且最新在前。
func TestRoutingRepoScoringAuditRoundTrip(t *testing.T) {
	db := newRoutingRepoCoverageDB(t)
	require.NoError(t, db.AutoMigrate(&models.RoutingAssignment{}))
	repo := NewGormRepository(db)
	ctx := routingCovScope("t1", "w1")
	now := time.Now()

	from := uint(3)
	first := &domain.RoutingAssignment{
		SessionID:   "sess-score",
		FromAgentID: &from,
		ToAgentID:   9,
		TotalScore:  0.85,
		Factors: map[string]float64{
			"skill": 1, "language": 1, "availability": 1, "workload": 0.5,
			"priority": 1, "tier": 1, "channel": 1, "sla": 1,
		},
		Reasons:    []string{"技能匹配 2/2", "负载 2/5"},
		Strategy:   "default_weighted_v1",
		AssignedAt: now,
	}
	require.NoError(t, repo.CreateRoutingAssignment(ctx, first))

	later := &domain.RoutingAssignment{
		SessionID:  "sess-score",
		ToAgentID:  4,
		TotalScore: 0.72,
		Factors:    map[string]float64{"skill": 0.5},
		Reasons:    []string{"技能匹配 1/2"},
		Strategy:   "default_weighted_v1",
		AssignedAt: now.Add(time.Minute),
	}
	require.NoError(t, repo.CreateRoutingAssignment(ctx, later))

	items, err := repo.ListRoutingAssignments(ctx, "sess-score", 50)
	require.NoError(t, err)
	require.Len(t, items, 2)
	// 最新在前。
	assert.Equal(t, uint(4), items[0].ToAgentID)
	assert.Equal(t, 0.72, items[0].TotalScore)
	assert.Equal(t, uint(9), items[1].ToAgentID)
	assert.InDelta(t, 0.85, items[1].TotalScore, 1e-9)
	// JSON 往返。
	assert.Equal(t, 1.0, items[1].Factors["skill"])
	assert.Equal(t, 0.5, items[1].Factors["workload"])
	assert.Equal(t, []string{"技能匹配 2/2", "负载 2/5"}, items[1].Reasons)
	assert.Equal(t, "default_weighted_v1", items[1].Strategy)

	// scope 字段与 transfer_records 同口径。
	var stored models.RoutingAssignment
	require.NoError(t, db.Where("session_id = ? AND to_agent_id = ?", "sess-score", uint(9)).First(&stored).Error)
	assert.Equal(t, "t1", stored.TenantID)
	assert.Equal(t, "w1", stored.WorkspaceID)

	// 无租户上下文（如 worker 内部调用）时列表不过滤。
	noScope, err := repo.ListRoutingAssignments(context.Background(), "sess-score", 50)
	require.NoError(t, err)
	assert.Len(t, noScope, 2)

	// 空输入防御。
	require.Error(t, repo.CreateRoutingAssignment(ctx, nil))
}
