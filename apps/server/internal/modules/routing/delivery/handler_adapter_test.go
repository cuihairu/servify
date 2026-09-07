package delivery

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"servify/apps/server/internal/models"
	agentdelivery "servify/apps/server/internal/modules/agent/delivery"
	conversationdelivery "servify/apps/server/internal/modules/conversation/delivery"
	routingcontract "servify/apps/server/internal/modules/routing/contract"

	"github.com/glebarez/sqlite"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

var handlerTestDBSeq atomic.Uint64

func newRoutingHandlerTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := "file:rt_handler_" + strings.ReplaceAll(t.Name(), "/", "_") + "_" + strconv.FormatUint(handlerTestDBSeq.Add(1), 10) + "?mode=memory&cache=shared"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("db handle: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)
	if err := db.AutoMigrate(&models.TransferRecord{}, &models.WaitingRecord{}, &models.Message{}); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	return db
}

func seedHandlerMessages(t *testing.T, db *gorm.DB, sessionID string, count int) {
	t.Helper()
	now := time.Now()
	for i := 0; i < count; i++ {
		msg := models.Message{
			SessionID: sessionID, UserID: 5,
			Content: fmt.Sprintf("msg-%d", i), Type: "text", Sender: "user",
			CreatedAt: now.Add(time.Duration(i) * time.Minute),
		}
		require.NoError(t, db.Create(&msg).Error)
	}
}

func seedHandlerWaiting(t *testing.T, db *gorm.DB, sessionID string) *models.WaitingRecord {
	t.Helper()
	record, err := newRoutingDeliveryAdapter(db).AddToWaitingQueue(
		context.Background(), nil, sessionID, "need_help", []string{"billing"}, "high", "",
	)
	require.NoError(t, err)
	return record
}

type handlerAIStub struct {
	transfer     bool
	query        string
	history      []models.Message
	summary      string
	summaryErr   error
	summaryCalls int
}

func (f *handlerAIStub) ShouldTransferToHuman(query string, sessionHistory []models.Message) bool {
	f.query = query
	f.history = sessionHistory
	return f.transfer
}

func (f *handlerAIStub) GetSessionSummary(messages []models.Message) (string, error) {
	f.summaryCalls++
	return f.summary, f.summaryErr
}

type handlerAgentsStub struct {
	findAgent    *agentdelivery.AgentInfo
	findErr      error
	findCalls    int
	lastSkills   []string
	lastPriority string

	online   *agentdelivery.AgentInfo
	onlineOK bool

	appliedSession string
	appliedFrom    *uint
	appliedTo      uint
}

func (f *handlerAgentsStub) FindAvailableAgent(ctx context.Context, skills []string, priority string) (*agentdelivery.AgentInfo, error) {
	f.findCalls++
	f.lastSkills = skills
	f.lastPriority = priority
	return f.findAgent, f.findErr
}

func (f *handlerAgentsStub) GetOnlineAgent(ctx context.Context, userID uint) (*agentdelivery.AgentInfo, bool) {
	return f.online, f.onlineOK
}

func (f *handlerAgentsStub) ApplySessionTransfer(ctx context.Context, sessionID string, fromAgentID *uint, toAgentID uint) {
	f.appliedSession = sessionID
	f.appliedFrom = fromAgentID
	f.appliedTo = toAgentID
}

type handlerNotifierStub struct {
	sent []Notification
}

func (f *handlerNotifierStub) SendToSession(sessionID string, message Notification) {
	f.sent = append(f.sent, message)
}

type handlerConvStub struct {
	session  *conversationdelivery.TransferSession
	sessions map[string]*conversationdelivery.TransferSession
	loadErr  error
	loadHits int

	syncErr   error
	syncCalls []string
	waitErr   error
	waitCalls []string

	appended  []string
	appendErr error
}

func (f *handlerConvStub) LoadTransferSession(ctx context.Context, sessionID string) (*conversationdelivery.TransferSession, error) {
	f.loadHits++
	if f.loadErr != nil {
		return nil, f.loadErr
	}
	if s, ok := f.sessions[sessionID]; ok {
		return s, nil
	}
	return f.session, nil
}

func (f *handlerConvStub) SyncTransferAssignment(ctx context.Context, tx *gorm.DB, sessionID string, customerID uint, agentID uint) error {
	f.syncCalls = append(f.syncCalls, sessionID)
	return f.syncErr
}

func (f *handlerConvStub) SyncWaitingAssignment(ctx context.Context, tx *gorm.DB, sessionID string, customerID uint) error {
	f.waitCalls = append(f.waitCalls, sessionID)
	return f.waitErr
}

func (f *handlerConvStub) AppendSystemMessage(ctx context.Context, tx *gorm.DB, sessionID string, content string, createdAt time.Time) error {
	f.appended = append(f.appended, content)
	return f.appendErr
}

type handlerTicketsStub struct {
	err   error
	calls []uint
}

func (f *handlerTicketsStub) SyncTransferAssignment(ctx context.Context, tx *gorm.DB, ticketID uint, agentID uint, actorID uint) error {
	f.calls = append(f.calls, ticketID)
	return f.err
}

type handlerLoadStub struct {
	err   error
	calls int
	from  *uint
	to    uint
}

func (f *handlerLoadStub) SyncTransferLoad(ctx context.Context, tx *gorm.DB, fromAgentID *uint, toAgentID uint) error {
	f.calls++
	f.from = fromAgentID
	f.to = toAgentID
	return f.err
}

type handlerRoutingStub struct {
	addToWaiting    func(ctx context.Context, tx *gorm.DB, sessionID string, reason string, targetSkills []string, priority string, notes string) (*models.WaitingRecord, error)
	assign          func(ctx context.Context, tx *gorm.DB, cmd AssignAgentCommand) (*models.TransferRecord, error)
	getHistory      func(ctx context.Context, sessionID string) ([]models.TransferRecord, error)
	listRecent      func(ctx context.Context, limit int) ([]models.TransferRecord, error)
	listWaiting     func(ctx context.Context, status string, limit int) ([]models.WaitingRecord, error)
	getWaiting      func(ctx context.Context, sessionID string) (*models.WaitingRecord, error)
	cancelWaiting   func(ctx context.Context, tx *gorm.DB, sessionID string, reason string) (*models.WaitingRecord, error)
	markTransferred func(ctx context.Context, tx *gorm.DB, sessionID string, agentID uint, assignedAt time.Time) (*models.WaitingRecord, error)
}

func (s *handlerRoutingStub) AddToWaitingQueue(ctx context.Context, tx *gorm.DB, sessionID string, reason string, targetSkills []string, priority string, notes string) (*models.WaitingRecord, error) {
	if s.addToWaiting == nil {
		return &models.WaitingRecord{SessionID: sessionID, Status: "waiting", QueuedAt: time.Now()}, nil
	}
	return s.addToWaiting(ctx, tx, sessionID, reason, targetSkills, priority, notes)
}

func (s *handlerRoutingStub) AssignAgent(ctx context.Context, tx *gorm.DB, cmd AssignAgentCommand) (*models.TransferRecord, error) {
	if s.assign == nil {
		return &models.TransferRecord{SessionID: cmd.SessionID, ToAgentID: &cmd.AgentID, TransferredAt: cmd.AssignedAt}, nil
	}
	return s.assign(ctx, tx, cmd)
}

func (s *handlerRoutingStub) GetTransferHistory(ctx context.Context, sessionID string) ([]models.TransferRecord, error) {
	if s.getHistory == nil {
		return []models.TransferRecord{}, nil
	}
	return s.getHistory(ctx, sessionID)
}

func (s *handlerRoutingStub) ListRecentTransferHistory(ctx context.Context, limit int) ([]models.TransferRecord, error) {
	if s.listRecent == nil {
		return []models.TransferRecord{}, nil
	}
	return s.listRecent(ctx, limit)
}

func (s *handlerRoutingStub) ListWaitingRecords(ctx context.Context, status string, limit int) ([]models.WaitingRecord, error) {
	if s.listWaiting == nil {
		return []models.WaitingRecord{}, nil
	}
	return s.listWaiting(ctx, status, limit)
}

func (s *handlerRoutingStub) GetWaitingRecord(ctx context.Context, sessionID string) (*models.WaitingRecord, error) {
	if s.getWaiting == nil {
		return nil, gorm.ErrRecordNotFound
	}
	return s.getWaiting(ctx, sessionID)
}

func (s *handlerRoutingStub) CancelWaiting(ctx context.Context, tx *gorm.DB, sessionID string, reason string) (*models.WaitingRecord, error) {
	if s.cancelWaiting == nil {
		return &models.WaitingRecord{SessionID: sessionID, Status: "cancelled"}, nil
	}
	return s.cancelWaiting(ctx, tx, sessionID, reason)
}

func (s *handlerRoutingStub) MarkWaitingTransferred(ctx context.Context, tx *gorm.DB, sessionID string, agentID uint, assignedAt time.Time) (*models.WaitingRecord, error) {
	if s.markTransferred == nil {
		return &models.WaitingRecord{SessionID: sessionID, Status: "transferred"}, nil
	}
	return s.markTransferred(ctx, tx, sessionID, agentID, assignedAt)
}

type handlerFixture struct {
	db       *gorm.DB
	svc      *HandlerServiceAdapter
	agents   *handlerAgentsStub
	conv     *handlerConvStub
	tickets  *handlerTicketsStub
	load     *handlerLoadStub
	notifier *handlerNotifierStub
	ai       *handlerAIStub
}

func newHandlerFixture(t *testing.T, session *conversationdelivery.TransferSession) *handlerFixture {
	t.Helper()
	db := newRoutingHandlerTestDB(t)
	fx := &handlerFixture{
		db:       db,
		agents:   &handlerAgentsStub{},
		conv:     &handlerConvStub{session: session},
		tickets:  &handlerTicketsStub{},
		load:     &handlerLoadStub{},
		notifier: &handlerNotifierStub{},
		ai:       &handlerAIStub{summary: "会话摘要"},
	}
	fx.svc = NewHandlerService(HandlerDependencies{
		DB:           db,
		Logger:       logrus.New(),
		AI:           fx.ai,
		Agents:       fx.agents,
		Notifier:     fx.notifier,
		Routing:      newRoutingDeliveryAdapter(db),
		Tickets:      fx.tickets,
		Conversation: fx.conv,
		AgentLoad:    fx.load,
	})
	return fx
}

func TestHandlerTransferToHumanSuccess(t *testing.T) {
	ticketID := uint(77)
	fx := newHandlerFixture(t, &conversationdelivery.TransferSession{
		ID: "sess-1", CustomerID: 5, Status: "active", UserUsername: "alice", TicketID: &ticketID,
	})
	fx.agents.findAgent = &agentdelivery.AgentInfo{UserID: 9, MaxConcurrent: 5, CurrentLoad: 1}
	seedHandlerMessages(t, fx.db, "sess-1", 3) // >=3 条消息 → AI 摘要

	result, err := fx.svc.TransferToHuman(context.Background(), &routingcontract.TransferRequest{
		SessionID: "sess-1", Reason: "need_help", Notes: "vip", TargetSkills: []string{"billing"}, Priority: "high",
	})
	require.NoError(t, err)
	assert.True(t, result.Success)
	assert.Equal(t, "sess-1", result.SessionID)
	assert.Equal(t, uint(9), result.NewAgentID)
	assert.Equal(t, "会话摘要", result.Summary)
	assert.False(t, result.IsWaiting)
	assert.False(t, result.TransferredAt.IsZero())
	assert.Equal(t, 1, fx.ai.summaryCalls)

	require.Len(t, fx.tickets.calls, 1)
	assert.Equal(t, uint(77), fx.tickets.calls[0])

	assert.Equal(t, 1, fx.load.calls)
	assert.Nil(t, fx.load.from)
	assert.Equal(t, uint(9), fx.load.to)

	assert.Equal(t, "sess-1", fx.agents.appliedSession)
	assert.Nil(t, fx.agents.appliedFrom)
	assert.Equal(t, uint(9), fx.agents.appliedTo)

	require.Len(t, fx.notifier.sent, 1)
	assert.Equal(t, "transfer_notification", fx.notifier.sent[0].Type)

	require.Len(t, fx.conv.syncCalls, 1)
	require.Len(t, fx.conv.appended, 1)
	assert.Contains(t, fx.conv.appended[0], "need_help")
	assert.Contains(t, fx.conv.appended[0], "vip")

	var records []models.TransferRecord
	require.NoError(t, fx.db.Find(&records, "session_id = ?", "sess-1").Error)
	require.Len(t, records, 1)
	assert.Equal(t, "need_help", records[0].Reason)
	assert.Equal(t, "会话摘要", records[0].SessionSummary)
	require.NotNil(t, records[0].ToAgentID)
	assert.Equal(t, uint(9), *records[0].ToAgentID)
}

func TestHandlerTransferToHumanWaitingQueue(t *testing.T) {
	fx := newHandlerFixture(t, &conversationdelivery.TransferSession{ID: "sess-w", CustomerID: 5, Status: "active"})
	fx.agents.findErr = errors.New("no agent online")

	result, err := fx.svc.TransferToHuman(context.Background(), &routingcontract.TransferRequest{
		SessionID: "sess-w", Reason: "need_help", TargetSkills: []string{"billing"}, Priority: "high",
	})
	require.NoError(t, err)
	assert.True(t, result.Success)
	assert.True(t, result.IsWaiting)
	assert.NotNil(t, result.QueuedAt)
	assert.Equal(t, "会话已加入等待队列", result.Summary)
	assert.Equal(t, []string{"billing"}, fx.agents.lastSkills)
	assert.Equal(t, "high", fx.agents.lastPriority)

	var waiting []models.WaitingRecord
	require.NoError(t, fx.db.Find(&waiting, "session_id = ?", "sess-w").Error)
	require.Len(t, waiting, 1)
	assert.Equal(t, "waiting", waiting[0].Status)

	require.Len(t, fx.conv.waitCalls, 1)
	require.Len(t, fx.conv.appended, 1)
	assert.Contains(t, fx.conv.appended[0], "等待队列")
	require.Len(t, fx.notifier.sent, 1)
	assert.Equal(t, "waiting_notification", fx.notifier.sent[0].Type)

	// 幂等：已有等待记录时直接返回
	second, err := fx.svc.TransferToHuman(context.Background(), &routingcontract.TransferRequest{SessionID: "sess-w"})
	require.NoError(t, err)
	assert.True(t, second.IsWaiting)
	assert.Equal(t, "会话已在等待队列中", second.Summary)

	var waitingAfter []models.WaitingRecord
	require.NoError(t, fx.db.Find(&waitingAfter, "session_id = ?", "sess-w").Error)
	assert.Len(t, waitingAfter, 1) // 未重复入队
	assert.Equal(t, 1, fx.agents.findCalls)
}

func TestHandlerTransferToHumanRejectsInvalidSessions(t *testing.T) {
	agentID := uint(3)
	cases := []struct {
		name    string
		session *conversationdelivery.TransferSession
		loadErr error
		wantErr string
	}{
		{name: "load failed", session: nil, loadErr: errors.New("boom"), wantErr: "session not found"},
		{name: "ended", session: &conversationdelivery.TransferSession{ID: "s", Status: "ended"}, wantErr: "session already ended"},
		{name: "assigned", session: &conversationdelivery.TransferSession{ID: "s", Status: "active", AgentID: &agentID}, wantErr: "session already assigned"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fx := newHandlerFixture(t, tc.session)
			fx.conv.loadErr = tc.loadErr
			_, err := fx.svc.TransferToHuman(context.Background(), &routingcontract.TransferRequest{SessionID: "s"})
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantErr)
		})
	}
}

func TestHandlerTransferToHumanSkipsNonWaitingRecord(t *testing.T) {
	fx := newHandlerFixture(t, &conversationdelivery.TransferSession{ID: "sess-c", CustomerID: 5, Status: "active"})
	fx.agents.findAgent = &agentdelivery.AgentInfo{UserID: 9}

	seedHandlerWaiting(t, fx.db, "sess-c")
	_, err := newRoutingDeliveryAdapter(fx.db).CancelWaiting(context.Background(), nil, "sess-c", "user_left")
	require.NoError(t, err)

	result, err := fx.svc.TransferToHuman(context.Background(), &routingcontract.TransferRequest{SessionID: "sess-c"})
	require.NoError(t, err)
	assert.True(t, result.Success)
	assert.Equal(t, uint(9), result.NewAgentID) // 已取消的等待记录不阻断转接

	var waiting []models.WaitingRecord
	require.NoError(t, fx.db.Find(&waiting, "session_id = ?", "sess-c").Error)
	require.Len(t, waiting, 1)
	// MarkWaitingTransferred 仅更新 status=waiting 的记录，已取消记录保持原状
	assert.Equal(t, "cancelled", waiting[0].Status)
}

func TestHandlerTransferToAgentFlows(t *testing.T) {
	ctx := context.Background()
	agentID := uint(9)

	t.Run("session not found", func(t *testing.T) {
		fx := newHandlerFixture(t, nil)
		fx.conv.loadErr = errors.New("boom")
		_, err := fx.svc.TransferToAgent(ctx, "s", agentID, "r")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "session not found")
	})

	t.Run("agent offline", func(t *testing.T) {
		fx := newHandlerFixture(t, &conversationdelivery.TransferSession{ID: "s", Status: "active"})
		_, err := fx.svc.TransferToAgent(ctx, "s", agentID, "r")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "target agent is not online")
	})

	t.Run("agent at capacity", func(t *testing.T) {
		fx := newHandlerFixture(t, &conversationdelivery.TransferSession{ID: "s", Status: "active"})
		fx.agents.online = &agentdelivery.AgentInfo{UserID: agentID, CurrentLoad: 3, MaxConcurrent: 3}
		fx.agents.onlineOK = true
		_, err := fx.svc.TransferToAgent(ctx, "s", agentID, "r")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "target agent is at maximum capacity")
	})

	t.Run("ended session rejected in executeTransfer", func(t *testing.T) {
		fx := newHandlerFixture(t, &conversationdelivery.TransferSession{ID: "s", Status: "ended"})
		fx.agents.online = &agentdelivery.AgentInfo{UserID: agentID, CurrentLoad: 0, MaxConcurrent: 3}
		fx.agents.onlineOK = true
		_, err := fx.svc.TransferToAgent(ctx, "s", agentID, "r")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "session already ended")
	})

	t.Run("same agent idempotent", func(t *testing.T) {
		fx := newHandlerFixture(t, &conversationdelivery.TransferSession{ID: "s", Status: "active", AgentID: &agentID})
		fx.agents.online = &agentdelivery.AgentInfo{UserID: agentID, MaxConcurrent: 5}
		fx.agents.onlineOK = true
		result, err := fx.svc.TransferToAgent(ctx, "s", agentID, "r")
		require.NoError(t, err)
		assert.True(t, result.Success)
		assert.Equal(t, "会话已指派给目标客服", result.Summary)

		var records []models.TransferRecord
		require.NoError(t, fx.db.Find(&records).Error)
		assert.Empty(t, records) // 未产生新转接记录
	})

	t.Run("success with short summary by name", func(t *testing.T) {
		fx := newHandlerFixture(t, &conversationdelivery.TransferSession{ID: "s", CustomerID: 5, Status: "active", UserName: "Bob"})
		fx.agents.online = &agentdelivery.AgentInfo{UserID: agentID, MaxConcurrent: 5}
		fx.agents.onlineOK = true
		result, err := fx.svc.TransferToAgent(ctx, "s", agentID, "reason-1")
		require.NoError(t, err)
		assert.Equal(t, agentID, result.NewAgentID)
		assert.Equal(t, "用户Bob的简短会话，共0条消息", result.Summary)
	})

	t.Run("summary error falls back", func(t *testing.T) {
		fx := newHandlerFixture(t, &conversationdelivery.TransferSession{ID: "s", CustomerID: 5, Status: "active", UserUsername: "alice"})
		fx.ai.summaryErr = errors.New("ai down")
		fx.agents.online = &agentdelivery.AgentInfo{UserID: agentID, MaxConcurrent: 5}
		fx.agents.onlineOK = true
		seedHandlerMessages(t, fx.db, "s", 3)
		result, err := fx.svc.TransferToAgent(ctx, "s", agentID, "r")
		require.NoError(t, err)
		assert.Equal(t, "无法生成会话摘要", result.Summary)
	})
}

func TestHandlerExecuteTransferFailures(t *testing.T) {
	ctx := context.Background()
	newSession := func() *conversationdelivery.TransferSession {
		return &conversationdelivery.TransferSession{ID: "s", CustomerID: 5, Status: "active"}
	}

	t.Run("update session fails", func(t *testing.T) {
		fx := newHandlerFixture(t, newSession())
		fx.agents.findAgent = &agentdelivery.AgentInfo{UserID: 9}
		fx.conv.syncErr = errors.New("sync boom")
		_, err := fx.svc.TransferToHuman(ctx, &routingcontract.TransferRequest{SessionID: "s"})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "update session")
	})

	t.Run("update ticket fails", func(t *testing.T) {
		ticketID := uint(4)
		fx := newHandlerFixture(t, &conversationdelivery.TransferSession{ID: "s", CustomerID: 5, Status: "active", TicketID: &ticketID})
		fx.agents.findAgent = &agentdelivery.AgentInfo{UserID: 9}
		fx.tickets.err = errors.New("ticket boom")
		_, err := fx.svc.TransferToHuman(ctx, &routingcontract.TransferRequest{SessionID: "s"})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "update ticket")
	})

	t.Run("ticket not found tolerated", func(t *testing.T) {
		ticketID := uint(4)
		fx := newHandlerFixture(t, &conversationdelivery.TransferSession{ID: "s", CustomerID: 5, Status: "active", TicketID: &ticketID})
		fx.agents.findAgent = &agentdelivery.AgentInfo{UserID: 9}
		fx.tickets.err = gorm.ErrRecordNotFound
		result, err := fx.svc.TransferToHuman(ctx, &routingcontract.TransferRequest{SessionID: "s"})
		require.NoError(t, err)
		assert.True(t, result.Success)
	})

	t.Run("sync agent load fails", func(t *testing.T) {
		fx := newHandlerFixture(t, newSession())
		fx.agents.findAgent = &agentdelivery.AgentInfo{UserID: 9}
		fx.load.err = errors.New("load boom")
		_, err := fx.svc.TransferToHuman(ctx, &routingcontract.TransferRequest{SessionID: "s"})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "sync agent load")
	})

	t.Run("create transfer message fails", func(t *testing.T) {
		fx := newHandlerFixture(t, newSession())
		fx.agents.findAgent = &agentdelivery.AgentInfo{UserID: 9}
		fx.conv.appendErr = errors.New("append boom")
		_, err := fx.svc.TransferToHuman(ctx, &routingcontract.TransferRequest{SessionID: "s"})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "create transfer message")
	})

	t.Run("create transfer record fails", func(t *testing.T) {
		fx := newHandlerFixture(t, newSession())
		fx.agents.findAgent = &agentdelivery.AgentInfo{UserID: 9}
		require.NoError(t, fx.db.Migrator().DropTable(&models.TransferRecord{}))
		_, err := fx.svc.TransferToHuman(ctx, &routingcontract.TransferRequest{SessionID: "s"})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "create transfer record")
	})

	t.Run("sync waiting record fails", func(t *testing.T) {
		db := newRoutingHandlerTestDB(t)
		conv := &handlerConvStub{session: &conversationdelivery.TransferSession{ID: "s", CustomerID: 5, Status: "active"}}
		agents := &handlerAgentsStub{findAgent: &agentdelivery.AgentInfo{UserID: 9}}
		stub := &handlerRoutingStub{}
		stub.markTransferred = func(ctx context.Context, tx *gorm.DB, sessionID string, agentID uint, assignedAt time.Time) (*models.WaitingRecord, error) {
			return nil, errors.New("mark boom")
		}
		svc := NewHandlerService(HandlerDependencies{
			DB: db, Logger: logrus.New(), Agents: agents, Routing: stub,
			Conversation: conv, Tickets: &handlerTicketsStub{}, AgentLoad: &handlerLoadStub{},
		})
		_, err := svc.TransferToHuman(ctx, &routingcontract.TransferRequest{SessionID: "s"})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "sync waiting record")
	})

	t.Run("nil agent runtime tolerated", func(t *testing.T) {
		db := newRoutingHandlerTestDB(t)
		conv := &handlerConvStub{session: &conversationdelivery.TransferSession{ID: "s", CustomerID: 5, Status: "active", UserUsername: "alice"}}
		svc := &HandlerServiceAdapter{
			db: db, logger: logrus.New(), aiService: &handlerAIStub{summary: "s"},
			routing: newRoutingDeliveryAdapter(db), conversation: conv,
			tickets: &handlerTicketsStub{}, agents: &handlerLoadStub{},
		}
		result, err := svc.executeTransfer(ctx, conv.session, 9, "r", "")
		require.NoError(t, err)
		assert.True(t, result.Success)
		assert.Equal(t, uint(9), result.NewAgentID)
	})
}

func TestHandlerAddToWaitingQueueBranches(t *testing.T) {
	ctx := context.Background()

	t.Run("existing waiting record short-circuits", func(t *testing.T) {
		fx := newHandlerFixture(t, &conversationdelivery.TransferSession{ID: "sess-d", CustomerID: 5, Status: "active"})
		seedHandlerWaiting(t, fx.db, "sess-d")

		result, err := fx.svc.addToWaitingQueue(ctx, fx.conv.session, &routingcontract.TransferRequest{SessionID: "sess-d"})
		require.NoError(t, err)
		assert.True(t, result.IsWaiting)
		assert.Equal(t, "会话已在等待队列中", result.Summary)

		var waiting []models.WaitingRecord
		require.NoError(t, fx.db.Find(&waiting, "session_id = ?", "sess-d").Error)
		assert.Len(t, waiting, 1)
	})

	t.Run("ensure session state fails", func(t *testing.T) {
		fx := newHandlerFixture(t, &conversationdelivery.TransferSession{ID: "sess-e", CustomerID: 5, Status: "active"})
		fx.conv.waitErr = errors.New("wait boom")
		_, err := fx.svc.addToWaitingQueue(ctx, fx.conv.session, &routingcontract.TransferRequest{SessionID: "sess-e"})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to ensure session active")
	})

	t.Run("create waiting record fails", func(t *testing.T) {
		db := newRoutingHandlerTestDB(t)
		conv := &handlerConvStub{session: &conversationdelivery.TransferSession{ID: "sess-f", CustomerID: 5, Status: "active"}}
		stub := &handlerRoutingStub{}
		stub.addToWaiting = func(ctx context.Context, tx *gorm.DB, sessionID string, reason string, targetSkills []string, priority string, notes string) (*models.WaitingRecord, error) {
			return nil, errors.New("queue boom")
		}
		svc := NewHandlerService(HandlerDependencies{
			DB: db, Logger: logrus.New(), Agents: &handlerAgentsStub{}, Routing: stub,
			Conversation: conv, Tickets: &handlerTicketsStub{}, AgentLoad: &handlerLoadStub{},
		})
		_, err := svc.addToWaitingQueue(ctx, conv.session, &routingcontract.TransferRequest{SessionID: "sess-f"})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to create waiting record")
	})

	t.Run("append waiting message fails", func(t *testing.T) {
		fx := newHandlerFixture(t, &conversationdelivery.TransferSession{ID: "sess-g", CustomerID: 5, Status: "active"})
		fx.conv.appendErr = errors.New("append boom")
		_, err := fx.svc.addToWaitingQueue(ctx, fx.conv.session, &routingcontract.TransferRequest{SessionID: "sess-g"})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "create waiting message")
	})
}

func TestHandlerProcessWaitingQueue(t *testing.T) {
	ctx := context.Background()

	t.Run("list failure propagates", func(t *testing.T) {
		db := newRoutingHandlerTestDB(t)
		stub := &handlerRoutingStub{}
		stub.listWaiting = func(ctx context.Context, status string, limit int) ([]models.WaitingRecord, error) {
			return nil, errors.New("list boom")
		}
		svc := NewHandlerService(HandlerDependencies{DB: db, Logger: logrus.New(), Routing: stub})
		err := svc.ProcessWaitingQueue(ctx)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to list waiting records")
	})

	t.Run("skips when agent unavailable", func(t *testing.T) {
		fx := newHandlerFixture(t, &conversationdelivery.TransferSession{ID: "sess-p1", CustomerID: 5, Status: "active"})
		seedHandlerWaiting(t, fx.db, "sess-p1")
		fx.agents.findErr = errors.New("no agent")

		require.NoError(t, fx.svc.ProcessWaitingQueue(ctx))
		var records []models.TransferRecord
		require.NoError(t, fx.db.Find(&records).Error)
		assert.Empty(t, records)
	})

	t.Run("skips when session missing", func(t *testing.T) {
		fx := newHandlerFixture(t, nil)
		fx.conv.loadErr = errors.New("gone")
		seedHandlerWaiting(t, fx.db, "sess-p2")
		fx.agents.findAgent = &agentdelivery.AgentInfo{UserID: 9}

		require.NoError(t, fx.svc.ProcessWaitingQueue(ctx))
		var records []models.TransferRecord
		require.NoError(t, fx.db.Find(&records).Error)
		assert.Empty(t, records)
	})

	t.Run("skips when transfer fails", func(t *testing.T) {
		fx := newHandlerFixture(t, &conversationdelivery.TransferSession{ID: "sess-p3", CustomerID: 5, Status: "active"})
		seedHandlerWaiting(t, fx.db, "sess-p3")
		fx.agents.findAgent = &agentdelivery.AgentInfo{UserID: 9}
		fx.conv.syncErr = errors.New("sync boom")

		require.NoError(t, fx.svc.ProcessWaitingQueue(ctx))
		var records []models.TransferRecord
		require.NoError(t, fx.db.Find(&records).Error)
		assert.Empty(t, records)
	})

	t.Run("transfers waiting sessions", func(t *testing.T) {
		fx := newHandlerFixture(t, nil)
		fx.conv.sessions = map[string]*conversationdelivery.TransferSession{
			"sess-p4": {ID: "sess-p4", CustomerID: 5, Status: "active"},
			"sess-p5": {ID: "sess-p5", CustomerID: 6, Status: "active"},
		}
		adapter := newRoutingDeliveryAdapter(fx.db)
		_, err := adapter.AddToWaitingQueue(ctx, nil, "sess-p4", "need_help", []string{"billing", "vip"}, "high", "queued note")
		require.NoError(t, err)
		_, err = adapter.AddToWaitingQueue(ctx, nil, "sess-p5", "need_help", nil, "low", "")
		require.NoError(t, err)
		fx.agents.findAgent = &agentdelivery.AgentInfo{UserID: 9}

		require.NoError(t, fx.svc.ProcessWaitingQueue(ctx))

		var waiting []models.WaitingRecord
		require.NoError(t, fx.db.Find(&waiting).Error)
		require.Len(t, waiting, 2)
		for _, w := range waiting {
			assert.Equal(t, "transferred", w.Status)
			require.NotNil(t, w.AssignedTo)
			assert.Equal(t, uint(9), *w.AssignedTo)
		}

		var records []models.TransferRecord
		require.NoError(t, fx.db.Find(&records).Error)
		require.Len(t, records, 2)

		// 第二次处理时队列已空
		fx.agents.findCalls = 0
		require.NoError(t, fx.svc.ProcessWaitingQueue(ctx))
		assert.Zero(t, fx.agents.findCalls)
	})
}

func TestHandlerHistoryAndWaitingQueries(t *testing.T) {
	ctx := context.Background()
	fx := newHandlerFixture(t, nil)
	adapter := newRoutingDeliveryAdapter(fx.db)
	now := time.Now().UTC().Truncate(time.Second)
	_, err := adapter.AssignAgent(ctx, nil, AssignAgentCommand{SessionID: "sess-q", AgentID: 9, Reason: "r1", AssignedAt: now})
	require.NoError(t, err)
	_, err = adapter.AddToWaitingQueue(ctx, nil, "sess-q2", "need_help", []string{"billing"}, "high", "")
	require.NoError(t, err)

	history, err := fx.svc.GetTransferHistory(ctx, "sess-q")
	require.NoError(t, err)
	require.Len(t, history, 1)
	assert.Equal(t, "r1", history[0].Reason)

	recent, err := fx.svc.ListRecentTransferHistory(ctx, 10)
	require.NoError(t, err)
	require.Len(t, recent, 1)
	assert.Equal(t, "sess-q", recent[0].SessionID)

	// status/limit 归一化：空 status → waiting，limit<=0 → 50
	waiting, err := fx.svc.ListWaitingRecords(ctx, "", 0)
	require.NoError(t, err)
	require.Len(t, waiting, 1)
	assert.Equal(t, "sess-q2", waiting[0].SessionID)
	assert.Equal(t, "billing", waiting[0].TargetSkills)

	stub := &handlerRoutingStub{}
	stub.getHistory = func(ctx context.Context, sessionID string) ([]models.TransferRecord, error) {
		return nil, errors.New("hist boom")
	}
	stub.listRecent = func(ctx context.Context, limit int) ([]models.TransferRecord, error) {
		return nil, errors.New("recent boom")
	}
	stub.listWaiting = func(ctx context.Context, status string, limit int) ([]models.WaitingRecord, error) {
		return nil, errors.New("waitlist boom")
	}
	svc := NewHandlerService(HandlerDependencies{DB: fx.db, Logger: logrus.New(), Routing: stub})

	_, err = svc.GetTransferHistory(ctx, "s")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get transfer history")

	_, err = svc.ListRecentTransferHistory(ctx, 10)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to list recent transfer history")

	_, err = svc.ListWaitingRecords(ctx, "waiting", 10)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to list waiting records")
}

func TestHandlerCancelWaitingRecord(t *testing.T) {
	ctx := context.Background()

	t.Run("empty session id rejected", func(t *testing.T) {
		fx := newHandlerFixture(t, nil)
		err := fx.svc.CancelWaitingRecord(ctx, "", 1, "r")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "session_id is required")
	})

	t.Run("cancels with explicit reason", func(t *testing.T) {
		fx := newHandlerFixture(t, &conversationdelivery.TransferSession{ID: "sess-x", CustomerID: 5, Status: "active"})
		seedHandlerWaiting(t, fx.db, "sess-x")

		require.NoError(t, fx.svc.CancelWaitingRecord(ctx, "sess-x", 7, "user_left"))

		var waiting models.WaitingRecord
		require.NoError(t, fx.db.First(&waiting, "session_id = ?", "sess-x").Error)
		assert.Equal(t, "cancelled", waiting.Status)
		assert.Equal(t, "user_left", waiting.Notes)
		require.Len(t, fx.conv.appended, 1)
		assert.Contains(t, fx.conv.appended[0], "user_left")
	})

	t.Run("default reason applied", func(t *testing.T) {
		fx := newHandlerFixture(t, &conversationdelivery.TransferSession{ID: "sess-y", CustomerID: 5, Status: "active"})
		seedHandlerWaiting(t, fx.db, "sess-y")

		require.NoError(t, fx.svc.CancelWaitingRecord(ctx, "sess-y", 7, ""))

		var waiting models.WaitingRecord
		require.NoError(t, fx.db.First(&waiting, "session_id = ?", "sess-y").Error)
		assert.Equal(t, "cancelled", waiting.Status)
		assert.Equal(t, "cancelled", waiting.Notes)
		require.Len(t, fx.conv.appended, 1)
		assert.Contains(t, fx.conv.appended[0], "cancelled")
	})

	t.Run("missing waiting record tolerated", func(t *testing.T) {
		fx := newHandlerFixture(t, &conversationdelivery.TransferSession{ID: "sess-z", CustomerID: 5, Status: "active"})
		require.NoError(t, fx.svc.CancelWaitingRecord(ctx, "sess-z", 7, "r"))
		require.Len(t, fx.conv.appended, 1)
	})

	t.Run("update failure propagates", func(t *testing.T) {
		db := newRoutingHandlerTestDB(t)
		stub := &handlerRoutingStub{}
		stub.cancelWaiting = func(ctx context.Context, tx *gorm.DB, sessionID string, reason string) (*models.WaitingRecord, error) {
			return nil, errors.New("cancel boom")
		}
		svc := NewHandlerService(HandlerDependencies{
			DB: db, Logger: logrus.New(), Routing: stub,
			Conversation: &handlerConvStub{},
		})
		err := svc.CancelWaitingRecord(ctx, "sess-bad", 1, "r")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "update waiting record")
	})
}

func TestHandlerAutoTransferCheck(t *testing.T) {
	ctx := context.Background()

	t.Run("nil ai service returns false", func(t *testing.T) {
		fx := newHandlerFixture(t, nil)
		svc := &HandlerServiceAdapter{db: fx.db, logger: logrus.New()}
		assert.False(t, svc.AutoTransferCheck(ctx, "s", []models.Message{{Sender: "user", Content: "help"}}))
	})

	t.Run("uses last five user messages", func(t *testing.T) {
		fx := newHandlerFixture(t, nil)
		messages := []models.Message{
			{Sender: "user", Content: "old1"},
			{Sender: "ai", Content: "skip-ai"},
			{Sender: "user", Content: "c2"},
			{Sender: "agent", Content: "skip-agent"},
			{Sender: "user", Content: "c4"},
			{Sender: "ai", Content: "skip-ai2"},
			{Sender: "user", Content: "c6"},
		}
		fx.ai.transfer = true
		assert.True(t, fx.svc.AutoTransferCheck(ctx, "s", messages))
		assert.Equal(t, "c2 c4 c6 ", fx.ai.query)
		assert.Len(t, fx.ai.history, 7)
	})

	t.Run("short history uses all messages", func(t *testing.T) {
		fx := newHandlerFixture(t, nil)
		fx.ai.transfer = false
		assert.False(t, fx.svc.AutoTransferCheck(ctx, "s", []models.Message{
			{Sender: "user", Content: "a"},
			{Sender: "user", Content: "b"},
		}))
		assert.Equal(t, "a b ", fx.ai.query)
	})
}

func TestHandlerGenerateSessionSummary(t *testing.T) {
	t.Run("query failure", func(t *testing.T) {
		fx := newHandlerFixture(t, nil)
		require.NoError(t, fx.db.Migrator().DropTable(&models.Message{}))
		_, err := fx.svc.generateSessionSummary(&conversationdelivery.TransferSession{ID: "s"})
		require.Error(t, err)
	})

	t.Run("prefers username then name then id", func(t *testing.T) {
		fx := newHandlerFixture(t, nil)
		summary, err := fx.svc.generateSessionSummary(&conversationdelivery.TransferSession{ID: "s", CustomerID: 5, UserUsername: "alice"})
		require.NoError(t, err)
		assert.Equal(t, "用户alice的简短会话，共0条消息", summary)

		summary, err = fx.svc.generateSessionSummary(&conversationdelivery.TransferSession{ID: "s", CustomerID: 5, UserName: "Bob"})
		require.NoError(t, err)
		assert.Equal(t, "用户Bob的简短会话，共0条消息", summary)

		summary, err = fx.svc.generateSessionSummary(&conversationdelivery.TransferSession{ID: "s", CustomerID: 5})
		require.NoError(t, err)
		assert.Equal(t, "用户ID=5的简短会话，共0条消息", summary)
	})

	t.Run("delegates to ai with enough messages", func(t *testing.T) {
		fx := newHandlerFixture(t, nil)
		seedHandlerMessages(t, fx.db, "s", 3)
		summary, err := fx.svc.generateSessionSummary(&conversationdelivery.TransferSession{ID: "s", CustomerID: 5})
		require.NoError(t, err)
		assert.Equal(t, "会话摘要", summary)
		assert.Equal(t, 1, fx.ai.summaryCalls)
	})
}

func TestHandlerSyncTransferTicketVariants(t *testing.T) {
	ctx := context.Background()
	fx := newHandlerFixture(t, nil)
	zero := uint(0)
	ticketID := uint(6)
	session := &conversationdelivery.TransferSession{ID: "s", TicketID: &ticketID}

	require.NoError(t, fx.svc.syncTransferTicket(ctx, fx.db, &conversationdelivery.TransferSession{ID: "s", TicketID: nil}, 9, time.Now()))
	require.NoError(t, fx.svc.syncTransferTicket(ctx, fx.db, &conversationdelivery.TransferSession{ID: "s", TicketID: &zero}, 9, time.Now()))
	assert.Empty(t, fx.tickets.calls)

	fx.tickets.err = gorm.ErrRecordNotFound
	require.NoError(t, fx.svc.syncTransferTicket(ctx, fx.db, session, 9, time.Now()))

	fx.tickets.err = errors.New("ticket boom")
	err := fx.svc.syncTransferTicket(ctx, fx.db, session, 9, time.Now())
	require.Error(t, err)
}

func TestHandlerNotifyWithoutNotifier(t *testing.T) {
	db := newRoutingHandlerTestDB(t)
	conv := &handlerConvStub{session: &conversationdelivery.TransferSession{ID: "s", CustomerID: 5, Status: "active"}}
	// Logger/Notifier 为 nil：不 panic
	svc := NewHandlerService(HandlerDependencies{
		DB: db, Agents: &handlerAgentsStub{findErr: errors.New("no agent")},
		Routing: newRoutingDeliveryAdapter(db), Conversation: conv,
		Tickets: &handlerTicketsStub{}, AgentLoad: &handlerLoadStub{},
	})
	result, err := svc.TransferToHuman(context.Background(), &routingcontract.TransferRequest{SessionID: "s"})
	require.NoError(t, err)
	assert.True(t, result.IsWaiting)

	assert.NotPanics(t, func() {
		svc.notifyTransfer("s", 1, "m")
		svc.notifyWaiting("s", "m")
	})
}

func TestSessionTransferAdapterErrorBranches(t *testing.T) {
	db := newRoutingHandlerTestDB(t)
	adapter := newRoutingDeliveryAdapter(db)
	ctx := context.Background()

	_, err := adapter.AddToWaitingQueue(ctx, nil, "", "r", nil, "", "")
	require.Error(t, err)

	_, err = adapter.GetTransferHistory(ctx, "")
	require.Error(t, err)

	require.NoError(t, db.Migrator().DropTable(&models.TransferRecord{}))
	_, err = adapter.ListRecentTransferHistory(ctx, 10)
	require.Error(t, err)

	require.NoError(t, db.Migrator().DropTable(&models.WaitingRecord{}))
	_, err = adapter.ListWaitingRecords(ctx, "waiting", 10)
	require.Error(t, err)

	assert.Nil(t, mapWaitingRecord(nil))
	assert.Empty(t, joinSkills(nil))
}

func TestHandlerPureHelpers(t *testing.T) {
	status, limit := normalizeWaitingRecordQuery("", 0)
	assert.Equal(t, "waiting", status)
	assert.Equal(t, 50, limit)

	_, limit = normalizeWaitingRecordQuery("x", -1)
	assert.Equal(t, 50, limit)

	_, limit = normalizeWaitingRecordQuery("x", 201)
	assert.Equal(t, 50, limit)

	status, limit = normalizeWaitingRecordQuery("transferred", 10)
	assert.Equal(t, "transferred", status)
	assert.Equal(t, 10, limit)

	assert.Equal(t, "您的会话已转接至人工客服。客服将很快为您提供帮助。", buildTransferMessage("", ""))
	assert.Contains(t, buildTransferMessage("busy", "vip"), "转接原因：busy")
	assert.Contains(t, buildTransferMessage("busy", "vip"), "备注：vip")

	updates, from, to := buildTransferTicketUpdate(9, "open")
	assert.Equal(t, map[string]interface{}{"agent_id": uint(9), "status": "assigned"}, updates)
	assert.Equal(t, "open", from)
	assert.Equal(t, "assigned", to)

	updates, from, to = buildTransferTicketUpdate(9, "")
	assert.Equal(t, "assigned", to)
	assert.Equal(t, map[string]interface{}{"agent_id": uint(9), "status": "assigned"}, updates)

	updates, from, to = buildTransferTicketUpdate(9, "resolved")
	assert.Equal(t, map[string]interface{}{"agent_id": uint(9)}, updates)
	assert.Equal(t, "resolved", from)
	assert.Equal(t, "resolved", to)

	assert.Nil(t, cloneTimePtr(nil))
	original := time.Now()
	cloned := cloneTimePtr(&original)
	require.NotNil(t, cloned)
	assert.Equal(t, original, *cloned)
	assert.NotSame(t, &original, cloned)
}
