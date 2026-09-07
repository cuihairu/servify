package services

import (
	"context"
	"testing"
	"time"

	"servify/apps/server/internal/models"

	"gorm.io/gorm"
)

type stubWorkspaceAgentReader struct {
	agents []*AgentInfo
}

func (r *stubWorkspaceAgentReader) GetOnlineAgents(ctx context.Context) []*AgentInfo {
	return r.agents
}

func newWorkspaceTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	return newServicesTestDB(t,
		&models.User{}, &models.Agent{}, &models.Customer{},
		&models.Session{}, &models.Ticket{}, &models.Message{},
	)
}

func TestWorkspaceService_GetOverview(t *testing.T) {
	db := newWorkspaceTestDB(t)
	reader := &stubWorkspaceAgentReader{agents: []*AgentInfo{
		{UserID: 1, Name: "Agent One"},
		{UserID: 2, Username: "agent-two"},
		nil,
	}}
	svc := NewWorkspaceService(db, reader)
	ctx := unitScopedContext("t1", "w1")

	user := &models.User{Username: "u", Email: "u@x.com", Role: "agent", Name: "User One"}
	if err := db.Create(user).Error; err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if err := db.Create(&models.Agent{UserID: user.ID, TenantID: "t1", WorkspaceID: "w1", Status: "online"}).Error; err != nil {
		t.Fatalf("seed agent: %v", err)
	}

	now := time.Now()
	customer := &models.User{Username: "c", Email: "c@x.com", Role: "customer", Name: "Customer One"}
	if err := db.Create(customer).Error; err != nil {
		t.Fatalf("seed customer: %v", err)
	}
	if err := db.Create(&models.Customer{UserID: customer.ID, TenantID: "t1", WorkspaceID: "w1"}).Error; err != nil {
		t.Fatalf("seed customer profile: %v", err)
	}
	ticket := &models.Ticket{Title: "T", CustomerID: customer.ID, TenantID: "t1", WorkspaceID: "w1", CreatedAt: now, UpdatedAt: now}
	if err := db.Create(ticket).Error; err != nil {
		t.Fatalf("seed ticket: %v", err)
	}
	agentID := user.ID
	if err := db.Create(&models.Session{
		ID: "sess-1", TenantID: "t1", WorkspaceID: "w1", Platform: "web",
		Status: "active", StartedAt: now, CreatedAt: now, UpdatedAt: now, AgentID: &agentID, TicketID: &ticket.ID,
	}).Error; err != nil {
		t.Fatalf("seed session: %v", err)
	}
	if err := db.Create(&models.Session{
		ID: "sess-2", TenantID: "t1", WorkspaceID: "w1", Platform: "telegram",
		Status: "active", StartedAt: now, CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatalf("seed waiting session: %v", err)
	}

	overview, err := svc.GetOverview(ctx, 0)
	if err != nil {
		t.Fatalf("GetOverview: %v", err)
	}
	if overview.TotalActiveSessions != 2 || overview.WaitingQueue != 1 {
		t.Fatalf("unexpected session counts: %+v", overview)
	}
	if overview.OnlineAgents != 1 {
		t.Fatalf("unexpected online agents: %d", overview.OnlineAgents)
	}
	if len(overview.Channels) != 2 {
		t.Fatalf("unexpected channels: %+v", overview.Channels)
	}
	if len(overview.RecentSessions) != 2 {
		t.Fatalf("unexpected recent sessions: %+v", overview.RecentSessions)
	}
	if overview.AgentStats == nil || len(overview.AgentStats.AvailableAgents) != 2 {
		t.Fatalf("unexpected agent stats: %+v", overview.AgentStats)
	}
	if overview.AgentStats.AvailableAgents[0].Name != "Agent One" || overview.AgentStats.AvailableAgents[1].Name != "agent-two" {
		t.Fatalf("unexpected available agents: %+v", overview.AgentStats.AvailableAgents)
	}

	// default limit branch and no online agents branch
	empty := NewWorkspaceService(db, &stubWorkspaceAgentReader{})
	if _, err := empty.GetOverview(ctx, 10); err != nil {
		t.Fatalf("GetOverview empty: %v", err)
	}
	if _, err := NewWorkspaceService(db, nil).GetOverview(ctx, 10); err != nil {
		t.Fatalf("GetOverview nil reader: %v", err)
	}
}

func TestWorkspaceService_GetOverviewErrors(t *testing.T) {
	db := newWorkspaceTestDB(t)
	svc := NewWorkspaceService(db, nil)
	ctx := context.Background()

	// drop tables to force query errors
	if err := db.Migrator().DropTable("sessions"); err != nil {
		t.Fatalf("drop sessions: %v", err)
	}
	if _, err := svc.GetOverview(ctx, 10); err == nil {
		t.Fatal("expected error when sessions table missing")
	}
}

func TestWorkspaceService_Helpers(t *testing.T) {
	db := newWorkspaceTestDB(t)
	svc := NewWorkspaceService(db, nil)
	if got := svc.getAvgResponseTime(context.Background()); got != 0 {
		t.Fatalf("expected zero avg, got %v", got)
	}
	if firstNonEmpty(" ", "b", "c") != "b" {
		t.Fatal("unexpected firstNonEmpty result")
	}
	if firstNonEmpty("", " ") != "" {
		t.Fatal("expected empty result")
	}
}
