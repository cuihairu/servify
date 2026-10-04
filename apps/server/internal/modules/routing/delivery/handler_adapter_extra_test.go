package delivery

import (
	"context"
	"errors"
	"testing"

	"servify/apps/server/internal/models"
	agentdelivery "servify/apps/server/internal/modules/agent/delivery"
	conversationdelivery "servify/apps/server/internal/modules/conversation/delivery"
	routingapplication "servify/apps/server/internal/modules/routing/application"
	"servify/apps/server/internal/platform/eventbus"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// handlerScorerStub 是打分引擎桩：按注入的候选/错误驱动 scoreAssignment 分支。
type handlerScorerStub struct {
	strategy string
	scored   []routingapplication.ScoredCandidate
	err      error
}

func (f *handlerScorerStub) Strategy() string {
	if f.strategy == "" {
		return "handler_stub_v1"
	}
	return f.strategy
}

func (f *handlerScorerStub) Score(_ context.Context, _ routingapplication.ScoringInput) ([]routingapplication.ScoredCandidate, error) {
	return f.scored, f.err
}

// handlerPublisherStub 是事务提交后的 routing 事件发口桩：前 failAfter 次成功，
// 之后返回错误（驱动提交后发布的告警分支）。
type handlerPublisherStub struct {
	failAfter int
	published []eventbus.Event
}

func (f *handlerPublisherStub) Publish(_ context.Context, event eventbus.Event) error {
	f.published = append(f.published, event)
	if len(f.published) > f.failAfter {
		return errors.New("bus down")
	}
	return nil
}

func TestHandlerWithScorerReceiverBranches(t *testing.T) {
	var nilSvc *HandlerServiceAdapter
	require.Nil(t, nilSvc.WithScorer(&handlerScorerStub{})) // nil 接收者原样返回

	svc := &HandlerServiceAdapter{}
	scorer := &handlerScorerStub{}
	got := svc.WithScorer(scorer)
	assert.Same(t, svc, got)
	assert.Same(t, scorer, got.scorer)
}

func TestHandlerScoreAssignmentBranches(t *testing.T) {
	ctx := context.Background()
	session := &conversationdelivery.TransferSession{ID: "sess-score", CustomerID: 5, Status: "active", Platform: "web"}
	fx := newHandlerFixture(t, session)
	svc := fx.svc.WithScorer(&handlerScorerStub{})

	// 坐席离线（拿不到在线快照）时静默跳过。
	fx.agents.online, fx.agents.onlineOK = nil, false
	assert.Nil(t, svc.scoreAssignment(ctx, session, 9, []string{"billing"}, "high"))

	// 打分失败：记警告后跳过，不阻塞转接。
	fx.agents.online = &agentdelivery.AgentInfo{UserID: 9, Status: "online", MaxConcurrent: 5}
	fx.agents.onlineOK = true
	svc.scorer = &handlerScorerStub{err: errors.New("scorer down")}
	assert.Nil(t, svc.scoreAssignment(ctx, session, 9, nil, ""))

	// 打分成功：取 top 候选的总分/因子/理由与策略标识。
	svc.scorer = &handlerScorerStub{
		scored: []routingapplication.ScoredCandidate{{
			AgentCandidate: routingapplication.AgentCandidate{AgentID: 9, UserID: 9, Status: "online"},
			Total:          0.87,
			Factors:        map[string]float64{"skill": 1},
			Reasons:        []string{"技能匹配"},
		}},
		strategy: "handler_stub_v1",
	}
	detail := svc.scoreAssignment(ctx, session, 9, []string{"billing"}, "high")
	require.NotNil(t, detail)
	assert.Equal(t, 0.87, detail.TotalScore)
	assert.Equal(t, map[string]float64{"skill": 1}, detail.Factors)
	assert.Equal(t, []string{"技能匹配"}, detail.Reasons)
	assert.Equal(t, "handler_stub_v1", detail.Strategy)
}

func TestHandlerExecuteTransferPublishesRoutingEvents(t *testing.T) {
	ctx := context.Background()
	agentID := uint(9)
	fx := newHandlerFixture(t, &conversationdelivery.TransferSession{ID: "sess-pub", CustomerID: 5, Status: "active", UserName: "Bob"})
	pub := &handlerPublisherStub{failAfter: 1} // 首个事件成功，其后失败走告警分支
	fx.svc.publisher = pub
	fx.agents.online = &agentdelivery.AgentInfo{UserID: agentID, Status: "online", MaxConcurrent: 5}
	fx.agents.onlineOK = true

	result, err := fx.svc.TransferToAgent(ctx, "sess-pub", agentID, "vip_escalation")
	require.NoError(t, err)
	assert.True(t, result.Success)
	require.NotEmpty(t, pub.published)
	names := make([]string, 0, len(pub.published))
	for _, event := range pub.published {
		names = append(names, event.Name())
	}
	assert.Contains(t, names, "routing.agent_assigned")
	assert.Contains(t, names, "routing.transfer_completed")
}

func TestHandlerListRoutingAssignmentsPassthrough(t *testing.T) {
	fx := newHandlerFixture(t, &conversationdelivery.TransferSession{ID: "sess-audit", CustomerID: 5, Status: "active"})
	require.NoError(t, fx.db.AutoMigrate(&models.RoutingAssignment{}))

	items, err := fx.svc.ListRoutingAssignments(context.Background(), "sess-audit", 10)
	require.NoError(t, err)
	assert.Empty(t, items)
}
