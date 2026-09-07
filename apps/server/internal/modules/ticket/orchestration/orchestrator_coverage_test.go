package orchestration

import (
	"context"
	"errors"
	"testing"
	"time"

	"servify/apps/server/internal/models"
	ticketapp "servify/apps/server/internal/modules/ticket/application"
	ticketcontract "servify/apps/server/internal/modules/ticket/contract"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

func newOrchCovDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:orchcov_"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&models.User{}, &models.Agent{}, &models.Ticket{}, &models.TicketStatus{}, &models.TicketCustomFieldValue{}, &models.CustomField{}))
	return db
}

func TestOrchCovPrepareCreateTicketBranches(t *testing.T) {
	ctx := context.Background()

	// nil customerExists -> db lookup path (missing customer)
	db := newOrchCovDB(t)
	o := NewTicketOrchestrator(db, logrus.New(), nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	_, err := o.PrepareCreateTicket(ctx, &ticketcontract.CreateTicketRequest{Title: "t", CustomerID: 1})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "customer not found")

	// customer exists via db path
	require.NoError(t, db.Create(&models.User{ID: 1, Username: "u", Email: "u@e.com"}).Error)
	o = NewTicketOrchestrator(db, logrus.New(), nil, nil, nil, nil, nil,
		func(ctx context.Context, provided map[string]interface{}, ticketCtx map[string]interface{}, enforceRequired bool) ([]models.TicketCustomFieldValue, error) {
			return nil, nil
		}, nil, nil, nil, nil)
	prepared, err := o.PrepareCreateTicket(ctx, &ticketcontract.CreateTicketRequest{Title: "t", CustomerID: 1, Category: "billing", Priority: "high", Source: "chat", Tags: "vip"})
	require.NoError(t, err)
	assert.Equal(t, "billing", prepared.Ticket.Category)
	assert.Equal(t, "high", prepared.Ticket.Priority)
	assert.Equal(t, "chat", prepared.Ticket.Source)
	assert.Nil(t, prepared.Ticket.SessionID)

	// customerExists error path
	o = NewTicketOrchestrator(nil, logrus.New(), nil, nil, nil,
		func(ctx context.Context, id uint) (bool, error) { return false, errors.New("db down") },
		nil, nil, nil, nil, nil, nil)
	_, err = o.PrepareCreateTicket(ctx, &ticketcontract.CreateTicketRequest{Title: "t", CustomerID: 1})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "customer lookup failed")

	// customer missing path
	o = NewTicketOrchestrator(nil, logrus.New(), nil, nil, nil,
		func(ctx context.Context, id uint) (bool, error) { return false, nil },
		nil, nil, nil, nil, nil, nil)
	_, err = o.PrepareCreateTicket(ctx, &ticketcontract.CreateTicketRequest{Title: "t", CustomerID: 1})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "customer not found")

	// custom field build error path
	o = NewTicketOrchestrator(nil, logrus.New(), nil, nil, nil,
		func(ctx context.Context, id uint) (bool, error) { return true, nil },
		nil,
		func(ctx context.Context, provided map[string]interface{}, ticketCtx map[string]interface{}, enforceRequired bool) ([]models.TicketCustomFieldValue, error) {
			return nil, errors.New("build failed")
		}, nil, nil, nil, nil)
	_, err = o.PrepareCreateTicket(ctx, &ticketcontract.CreateTicketRequest{Title: "t", CustomerID: 1})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "build failed")
}

func TestOrchCovApplyCreateTicketSideEffects(t *testing.T) {
	ctx := context.Background()

	// nil ticket
	o := NewTicketOrchestrator(nil, logrus.New(), nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	_, err := o.ApplyCreateTicketSideEffects(ctx, nil)
	require.Error(t, err)

	// load ticket error
	o = NewTicketOrchestrator(nil, logrus.New(), nil, nil, nil, nil, nil, nil, nil,
		func(ctx context.Context, id uint) (*models.Ticket, error) { return nil, errors.New("load failed") },
		nil, nil)
	_, err = o.ApplyCreateTicketSideEffects(ctx, &models.Ticket{ID: 1, CustomerID: 2})
	require.Error(t, err)

	// success path with bus + sla
	ticket := &models.Ticket{ID: 5, CustomerID: 2, Status: "open"}
	sla := &stubSLAService{}
	bus := &stubBus{}
	o = NewTicketOrchestrator(nil, logrus.New(), sla, nil, bus, nil,
		func(ctx context.Context) (*models.Agent, error) { return nil, errors.New("no agent") },
		nil, nil,
		func(ctx context.Context, id uint) (*models.Ticket, error) { return ticket, nil },
		nil, nil)
	created, err := o.ApplyCreateTicketSideEffects(ctx, &models.Ticket{ID: 5, CustomerID: 2})
	require.NoError(t, err)
	assert.Equal(t, uint(5), created.ID)
	require.Len(t, bus.events, 1)
	assert.Equal(t, "ticket.created", bus.events[0].Name())
	assert.Len(t, sla.checkCalls, 1)
}

func TestOrchCovPrepareUpdateTicketBranches(t *testing.T) {
	ctx := context.Background()
	now := time.Now()
	old := &models.Ticket{ID: 3, Title: "Old", CustomerID: 7, Category: "billing", Priority: "normal", Source: "web", Status: "open", CreatedAt: now, UpdatedAt: now}

	// load error
	o := NewTicketOrchestrator(nil, logrus.New(), nil, nil, nil, nil, nil, nil, nil,
		func(ctx context.Context, id uint) (*models.Ticket, error) { return nil, errors.New("load failed") },
		nil, nil)
	_, err := o.PrepareUpdateTicket(ctx, 3, &ticketcontract.UpdateTicketRequest{}, 1)
	require.Error(t, err)

	// description + due date + agent change to zero + tags + mutation error
	o = NewTicketOrchestrator(nil, logrus.New(), nil, nil, nil, nil, nil, nil, nil,
		func(ctx context.Context, id uint) (*models.Ticket, error) { return old, nil },
		nil, nil)
	o.prepareCustomFieldUpdate = func(ctx context.Context, ticketID uint, provided map[string]interface{}, ticketCtx map[string]interface{}) (*ticketapp.CustomFieldMutation, error) {
		return nil, errors.New("mutation failed")
	}
	zeroAgent := uint(0)
	_, err = o.PrepareUpdateTicket(ctx, 3, &ticketcontract.UpdateTicketRequest{Description: strPtrHelper("d"), DueDate: &now, AgentID: &zeroAgent, Tags: strPtrHelper("x"), CustomFields: map[string]interface{}{"a": 1}}, 1)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "mutation failed")

	// agent changed (nil -> non-zero), closed status branch
	closed := "closed"
	o2 := NewTicketOrchestrator(nil, logrus.New(), nil, nil, nil, nil, nil, nil, nil,
		func(ctx context.Context, id uint) (*models.Ticket, error) { return old, nil },
		nil, nil)
	prepared, err := o2.PrepareUpdateTicket(ctx, 3, &ticketcontract.UpdateTicketRequest{AgentID: uintPtrHelper(9), Status: &closed}, 4)
	require.NoError(t, err)
	assert.True(t, prepared.AgentChanged)
	assert.True(t, prepared.StatusChanged)
	assert.NotNil(t, prepared.Updates["closed_at"])
	assert.Nil(t, prepared.Updates["resolved_at"])
	assert.NotNil(t, prepared.StatusChange)

	// no-op update with all nil fields
	o3 := NewTicketOrchestrator(nil, logrus.New(), nil, nil, nil, nil, nil, nil, nil,
		func(ctx context.Context, id uint) (*models.Ticket, error) { return old, nil },
		nil, nil)
	prepared, err = o3.PrepareUpdateTicket(ctx, 3, &ticketcontract.UpdateTicketRequest{}, 4)
	require.NoError(t, err)
	assert.False(t, prepared.StatusChanged)
	assert.False(t, prepared.AgentChanged)
	assert.Nil(t, prepared.StatusChange)
	assert.NotContains(t, prepared.Updates, "status")

	// same agent id -> agent unchanged
	assigned := &models.Ticket{ID: 4, Title: "A", CustomerID: 7, Status: "assigned", AgentID: uintPtrHelper(9)}
	o4 := NewTicketOrchestrator(nil, logrus.New(), nil, nil, nil, nil, nil, nil, nil,
		func(ctx context.Context, id uint) (*models.Ticket, error) { return assigned, nil },
		nil, nil)
	prepared, err = o4.PrepareUpdateTicket(ctx, 4, &ticketcontract.UpdateTicketRequest{AgentID: uintPtrHelper(9)}, 4)
	require.NoError(t, err)
	assert.False(t, prepared.AgentChanged)
}

func TestOrchCovApplyUpdateTicketSideEffectsBranches(t *testing.T) {
	ctx := context.Background()

	// load error
	o := NewTicketOrchestrator(nil, logrus.New(), nil, nil, nil, nil, nil, nil, nil,
		func(ctx context.Context, id uint) (*models.Ticket, error) { return nil, errors.New("load failed") },
		nil, nil)
	_, err := o.ApplyUpdateTicketSideEffects(ctx, &TicketUpdatePreparation{}, 1)
	require.Error(t, err)

	// nil prepared
	ticket := &models.Ticket{ID: 2, Status: "open"}
	o = NewTicketOrchestrator(nil, logrus.New(), nil, nil, nil, nil, nil, nil, nil,
		func(ctx context.Context, id uint) (*models.Ticket, error) { return ticket, nil },
		nil, nil)
	got, err := o.ApplyUpdateTicketSideEffects(ctx, nil, 2)
	require.NoError(t, err)
	assert.Equal(t, ticket, got)

	// agent changed with nil bus (no publish) and no SLA
	o = NewTicketOrchestrator(nil, logrus.New(), nil, nil, nil, nil, nil, nil, nil,
		func(ctx context.Context, id uint) (*models.Ticket, error) { return ticket, nil },
		nil, nil)
	got, err = o.ApplyUpdateTicketSideEffects(ctx, &TicketUpdatePreparation{AgentChanged: true}, 2)
	require.NoError(t, err)
	assert.Equal(t, uint(2), got.ID)
}

func TestOrchCovApplyAssignTicketSideEffectsBranches(t *testing.T) {
	ctx := context.Background()
	sla := &stubSLAService{}
	o := NewTicketOrchestrator(nil, logrus.New(), sla, nil, nil, nil, nil, nil, nil, nil, nil, nil)

	// nil updated ticket -> early return
	o.ApplyAssignTicketSideEffects(ctx, &models.Ticket{ID: 1}, nil)
	assert.Empty(t, sla.checkCalls)

	// nil original ticket -> statusChanged false path
	o.ApplyAssignTicketSideEffects(ctx, nil, &models.Ticket{ID: 1, Status: "assigned", AgentID: uintPtrHelper(2)})
	assert.Len(t, sla.checkCalls, 1)

	// original with agent set -> no first_response resolve, status same
	sla2 := &stubSLAService{}
	o = NewTicketOrchestrator(nil, logrus.New(), sla2, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	o.ApplyAssignTicketSideEffects(ctx, &models.Ticket{ID: 1, Status: "assigned", AgentID: uintPtrHelper(3)}, &models.Ticket{ID: 1, Status: "assigned", AgentID: uintPtrHelper(4)})
	require.Len(t, sla2.resolveCalls, 1)
	assert.Equal(t, "first_response", sla2.resolveCalls[0].types[0])
	assert.Len(t, sla2.checkCalls, 1)
}

func TestOrchCovApplyCloseTicketSideEffectsErrorBranches(t *testing.T) {
	ctx := context.Background()
	sla := &stubSLAService{resolveErr: errors.New("resolve failed")}
	satisfaction := &stubSatisfactionService{err: errors.New("survey failed")}
	o := NewTicketOrchestrator(nil, logrus.New(), sla, satisfaction, nil, nil, nil, nil, nil,
		func(ctx context.Context, id uint) (*models.Ticket, error) { return &models.Ticket{ID: 8}, nil },
		nil,
		func(ctx context.Context, ticketID uint, userID uint, content string, commentType string) (*models.TicketComment, error) {
			return nil, errors.New("comment failed")
		})

	assert.NotPanics(t, func() {
		o.ApplyCloseTicketSideEffects(ctx, 8, 9, "done")
	})
}

func TestOrchCovAutoAssignAgentAssignError(t *testing.T) {
	o := NewTicketOrchestrator(nil, logrus.New(), nil, nil, nil, nil,
		func(ctx context.Context) (*models.Agent, error) { return &models.Agent{UserID: 3}, nil },
		nil, nil, nil,
		func(ctx context.Context, ticketID uint, agentID uint, assignerID uint) error {
			return errors.New("assign failed")
		},
		nil)

	assert.NotPanics(t, func() {
		o.autoAssignAgent(1)
	})
}

func TestOrchCovSelectAutoAssigneeDBPath(t *testing.T) {
	db := newOrchCovDB(t)
	o := NewTicketOrchestrator(db, logrus.New(), nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)

	_, err := o.selectAutoAssignee(context.Background())
	require.Error(t, err)

	require.NoError(t, db.Create(&models.Agent{UserID: 4, Status: "online", MaxConcurrent: 5, CurrentLoad: 1}).Error)
	agent, err := o.selectAutoAssignee(context.Background())
	require.NoError(t, err)
	assert.Equal(t, uint(4), agent.UserID)
}

func TestOrchCovPublishTicketModuleEventBranches(t *testing.T) {
	ctx := context.Background()
	ticket := &models.Ticket{ID: 1}

	// nil bus
	o := NewTicketOrchestrator(nil, logrus.New(), nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	assert.NotPanics(t, func() { o.publishTicketModuleEvent(ctx, "e", ticket) })

	// nil ticket
	bus := &stubBus{}
	o = NewTicketOrchestrator(nil, logrus.New(), nil, nil, bus, nil, nil, nil, nil, nil, nil, nil)
	assert.NotPanics(t, func() { o.publishTicketModuleEvent(ctx, "e", nil) })
	assert.Empty(t, bus.events)

	// publish error
	failing := &stubBus{err: errors.New("bus down")}
	o = NewTicketOrchestrator(nil, logrus.New(), nil, nil, failing, nil, nil, nil, nil, nil, nil, nil)
	assert.NotPanics(t, func() { o.publishTicketModuleEvent(ctx, "e", ticket) })
}

func TestOrchCovEvaluateTicketSLABranches(t *testing.T) {
	ctx := context.Background()

	// nil sla service
	o := NewTicketOrchestrator(nil, logrus.New(), nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	assert.NotPanics(t, func() { o.evaluateTicketSLA(ctx, &models.Ticket{ID: 1}, true, true) })

	// nil ticket
	sla := &stubSLAService{}
	o = NewTicketOrchestrator(nil, logrus.New(), sla, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	assert.NotPanics(t, func() { o.evaluateTicketSLA(ctx, nil, true, true) })
	assert.Empty(t, sla.checkCalls)

	// status resolved but not changed / agent nil: no resolve calls
	o.evaluateTicketSLA(ctx, &models.Ticket{ID: 1, Status: "resolved"}, false, true)
	assert.Empty(t, sla.resolveCalls)
	assert.Len(t, sla.checkCalls, 1)

	// status changed to non-terminal
	o.evaluateTicketSLA(ctx, &models.Ticket{ID: 1, Status: "assigned"}, true, false)
	assert.Empty(t, sla.resolveCalls)

	// agent changed but agent nil
	o.evaluateTicketSLA(ctx, &models.Ticket{ID: 1, Status: "assigned"}, false, true)
	assert.Empty(t, sla.resolveCalls)

	// resolve errors swallowed
	failing := &stubSLAService{resolveErr: errors.New("nope"), checkErr: errors.New("boom")}
	o = NewTicketOrchestrator(nil, logrus.New(), failing, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	assert.NotPanics(t, func() {
		o.evaluateTicketSLA(ctx, &models.Ticket{ID: 1, Status: "resolved", AgentID: uintPtrHelper(2)}, true, true)
	})
	assert.Len(t, failing.resolveCalls, 2)
}

func TestOrchCovResolveTicketSLAViolationsBranches(t *testing.T) {
	ctx := context.Background()

	o := NewTicketOrchestrator(nil, logrus.New(), nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	assert.NotPanics(t, func() { o.resolveTicketSLAViolations(ctx, 1, []string{"resolution"}) })

	failing := &stubSLAService{resolveErr: errors.New("failed")}
	o = NewTicketOrchestrator(nil, logrus.New(), failing, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	assert.NotPanics(t, func() { o.resolveTicketSLAViolations(ctx, 1, []string{"resolution"}) })
}

func strPtrHelper(v string) *string { return &v }

func uintPtrHelper(v uint) *uint { return &v }
