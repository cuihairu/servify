package delivery

import (
	"context"
	"errors"
	"testing"
	"time"

	"servify/apps/server/internal/models"
	agentdelivery "servify/apps/server/internal/modules/agent/delivery"
	conversationdelivery "servify/apps/server/internal/modules/conversation/delivery"
	routingapp "servify/apps/server/internal/modules/routing/application"
	routingcontract "servify/apps/server/internal/modules/routing/contract"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// groupCapturingAgentsStub 记录 SelectAgent 收到的 GroupID/Affinity，
// 覆盖 selectAgentForTransfer 中 targetGroupID != 0 的分支。
type groupCapturingAgentsStub struct {
	handlerAgentsStub
	lastGroupID  *uint
	lastAffinity *agentdelivery.AffinityHint
}

func (g *groupCapturingAgentsStub) SelectAgent(ctx context.Context, req agentdelivery.SelectionRequest) (*agentdelivery.SelectionResult, error) {
	g.lastGroupID = req.GroupID
	g.lastAffinity = req.Affinity
	return g.handlerAgentsStub.SelectAgent(ctx, req)
}

func TestHandlerTransferToHumanWithTargetGroup(t *testing.T) {
	db := newRoutingHandlerTestDB(t)
	agents := &groupCapturingAgentsStub{}
	agents.findAgent = &agentdelivery.AgentInfo{UserID: 9, MaxConcurrent: 5, CurrentLoad: 1}
	svc := NewHandlerService(HandlerDependencies{
		DB:      db,
		Logger:  logrus.New(),
		AI:      &handlerAIStub{summary: "会话摘要"},
		Agents:  agents,
		Routing: newRoutingDeliveryAdapter(db),
		Conversation: &handlerConvStub{session: &conversationdelivery.TransferSession{
			ID: "sess-group", CustomerID: 5, Status: "active",
		}},
		Notifier:  &handlerNotifierStub{},
		Tickets:   &handlerTicketsStub{},
		AgentLoad: &handlerLoadStub{},
	})

	result, err := svc.TransferToHuman(context.Background(), &routingcontract.TransferRequest{
		SessionID: "sess-group", Reason: "need_help", TargetGroupID: 7, Priority: "high",
	})
	require.NoError(t, err)
	assert.True(t, result.Success)
	assert.Equal(t, uint(9), result.NewAgentID)
	require.NotNil(t, agents.lastGroupID)
	assert.Equal(t, uint(7), *agents.lastGroupID)
	require.NotNil(t, agents.lastAffinity) // CustomerID != 0 -> 亲和提示
	assert.Equal(t, uint(5), agents.lastAffinity.CustomerUserID)
}

func TestHandlerReleaseClaimLogsWhenFails(t *testing.T) {
	db := newRoutingHandlerTestDB(t)
	stub := &handlerRoutingStub{}
	stub.claimWaiting = func(ctx context.Context, now time.Time, leaseBefore time.Time, limit int) ([]models.WaitingRecord, error) {
		return []models.WaitingRecord{{
			SessionID: "sess-rel", Status: "waiting", QueuedAt: time.Now(),
			TargetSkills: "billing", Priority: "high",
		}}, nil
	}
	stub.releaseClaim = func(ctx context.Context, sessionID string) error {
		return errors.New("release boom")
	}
	conv := &handlerConvStub{session: &conversationdelivery.TransferSession{
		ID: "sess-rel", CustomerID: 5, Status: "active",
	}}
	conv.syncErr = errors.New("sync boom")

	svc := NewHandlerService(HandlerDependencies{
		DB:           db,
		Logger:       logrus.New(),
		Agents:       &handlerAgentsStub{findAgent: &agentdelivery.AgentInfo{UserID: 9}},
		Routing:      stub,
		Conversation: conv,
		Tickets:      &handlerTicketsStub{},
		AgentLoad:    &handlerLoadStub{},
	})

	processed, err := svc.ProcessWaitingQueue(context.Background())
	require.NoError(t, err)
	assert.Zero(t, processed)
	assert.Equal(t, []string{"sess-rel"}, conv.syncCalls)
}

func TestHandlerDerefTargetGroupIDNonNil(t *testing.T) {
	id := uint(6)
	assert.Equal(t, uint(6), derefTargetGroupID(&id))
	assert.Equal(t, uint(0), derefTargetGroupID(nil))
}

func TestSessionTransferAdapterClaimWaitingRecordsError(t *testing.T) {
	db := newRoutingDeliveryTestDB(t)
	adapter := newRoutingDeliveryAdapter(db)
	require.NoError(t, db.Migrator().DropTable(&models.WaitingRecord{}))

	_, err := adapter.ClaimWaitingRecords(context.Background(), time.Now(), time.Now(), 5)
	require.Error(t, err)
}

func TestMapWaitingRecordNonZeroGroup(t *testing.T) {
	record := mapWaitingRecord(&routingapp.QueueEntryDTO{
		SessionID: "sess-map", Priority: "high", TargetGroupID: 5, TargetSkills: []string{"a"},
	})
	require.NotNil(t, record)
	require.NotNil(t, record.TargetGroupID)
	assert.Equal(t, uint(5), *record.TargetGroupID)
}
