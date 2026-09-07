//go:build integration
// +build integration

package delivery_test

import (
	"context"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"servify/apps/server/internal/models"
	gamificationdelivery "servify/apps/server/internal/modules/gamification/delivery"
	platformauth "servify/apps/server/internal/platform/auth"
)

func newGamificationDeliveryTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(
		&models.Agent{},
		&models.User{},
		&models.Ticket{},
		&models.CustomerSatisfaction{},
	); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}
	return db
}

func gamificationUintPtr(i uint) *uint { return &i }

func createDeliveryTestAgent(db *gorm.DB, userID uint, name, department string, avgResponseTimeSec int) error {
	user := &models.User{
		ID:       userID,
		Username: "user_" + name,
		Email:    name + "@test.com",
		Name:     name,
		Role:     "agent",
	}
	if err := db.Create(user).Error; err != nil {
		return err
	}
	agent := &models.Agent{
		UserID:          userID,
		Department:      department,
		AvgResponseTime: avgResponseTimeSec,
		Status:          "online",
	}
	return db.Create(agent).Error
}

func createScopedDeliveryTestAgent(db *gorm.DB, userID uint, name, department, tenantID, workspaceID string, avgResponseTimeSec int) error {
	user := &models.User{
		ID:       userID,
		Username: "user_" + name,
		Email:    name + "@test.com",
		Name:     name,
		Role:     "agent",
	}
	if err := db.Create(user).Error; err != nil {
		return err
	}
	agent := &models.Agent{
		UserID:          userID,
		TenantID:        tenantID,
		WorkspaceID:     workspaceID,
		Department:      department,
		AvgResponseTime: avgResponseTimeSec,
		Status:          "online",
	}
	return db.Create(agent).Error
}

func gamificationScopedContext(tenantID, workspaceID string) context.Context {
	return platformauth.ContextWithScope(context.Background(), tenantID, workspaceID)
}

func TestHandlerServiceGetLeaderboard(t *testing.T) {
	db := newGamificationDeliveryTestDB(t)
	svc := gamificationdelivery.NewHandlerService(db)

	start := time.Now().Add(-48 * time.Hour)
	end := time.Now()

	_ = createDeliveryTestAgent(db, 1, "张三", "技术支持", 300)
	_ = createDeliveryTestAgent(db, 2, "李四", "技术支持", 600)
	_ = createDeliveryTestAgent(db, 3, "王五", "客户服务", 1200)

	now := time.Now()
	tickets := []models.Ticket{
		{
			CustomerID: 1,
			AgentID:    gamificationUintPtr(1),
			Status:     "resolved",
			ResolvedAt: &[]time.Time{now.Add(-24 * time.Hour)}[0],
			Priority:   "1",
			Title:      "工单1",
		},
		{
			CustomerID: 2,
			AgentID:    gamificationUintPtr(1),
			Status:     "resolved",
			ResolvedAt: &[]time.Time{now.Add(-12 * time.Hour)}[0],
			Priority:   "2",
			Title:      "工单2",
		},
		{
			CustomerID: 3,
			AgentID:    gamificationUintPtr(2),
			Status:     "resolved",
			ResolvedAt: &[]time.Time{now.Add(-18 * time.Hour)}[0],
			Priority:   "1",
			Title:      "工单3",
		},
	}
	for _, ticket := range tickets {
		if err := db.Create(&ticket).Error; err != nil {
			t.Fatalf("create ticket: %v", err)
		}
	}

	csats := []models.CustomerSatisfaction{
		{
			TicketID:   1,
			CustomerID: 1,
			AgentID:    gamificationUintPtr(1),
			Rating:     5,
			CreatedAt:  time.Now().Add(-23 * time.Hour),
		},
		{
			TicketID:   2,
			CustomerID: 2,
			AgentID:    gamificationUintPtr(1),
			Rating:     4,
			CreatedAt:  time.Now().Add(-11 * time.Hour),
		},
		{
			TicketID:   3,
			CustomerID: 3,
			AgentID:    gamificationUintPtr(2),
			Rating:     3,
			CreatedAt:  time.Now().Add(-17 * time.Hour),
		},
	}
	for _, csat := range csats {
		if err := db.Create(&csat).Error; err != nil {
			t.Fatalf("create csat: %v", err)
		}
	}

	resp, err := svc.GetLeaderboard(context.Background(), &gamificationdelivery.LeaderboardRequest{
		StartDate: start,
		EndDate:   end,
		Limit:     10,
	})
	if err != nil {
		t.Fatalf("GetLeaderboard failed: %v", err)
	}

	if len(resp.Entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(resp.Entries))
	}
	if resp.Entries[0].Rank != 1 {
		t.Errorf("expected rank 1, got %d", resp.Entries[0].Rank)
	}
	if len(resp.Entries[0].Badges) == 0 {
		t.Error("expected badges for top performer")
	}
}

func TestHandlerServiceGetLeaderboard_Validation(t *testing.T) {
	db := newGamificationDeliveryTestDB(t)
	svc := gamificationdelivery.NewHandlerService(db)

	now := time.Now()

	tests := []struct {
		name    string
		req     *gamificationdelivery.LeaderboardRequest
		wantErr bool
	}{
		{
			name: "valid request",
			req: &gamificationdelivery.LeaderboardRequest{
				StartDate: now.Add(-24 * time.Hour),
				EndDate:   now,
				Limit:     10,
			},
			wantErr: false,
		},
		{
			name:    "nil request",
			req:     nil,
			wantErr: true,
		},
		{
			name: "missing start date",
			req: &gamificationdelivery.LeaderboardRequest{
				EndDate: now,
				Limit:   10,
			},
			wantErr: true,
		},
		{
			name: "missing end date",
			req: &gamificationdelivery.LeaderboardRequest{
				StartDate: now.Add(-24 * time.Hour),
				Limit:     10,
			},
			wantErr: true,
		},
		{
			name: "end before start",
			req: &gamificationdelivery.LeaderboardRequest{
				StartDate: now,
				EndDate:   now.Add(-24 * time.Hour),
				Limit:     10,
			},
			wantErr: true,
		},
		{
			name: "zero limit defaults to 10",
			req: &gamificationdelivery.LeaderboardRequest{
				StartDate: now.Add(-24 * time.Hour),
				EndDate:   now,
				Limit:     0,
			},
			wantErr: false,
		},
		{
			name: "limit over 100 capped",
			req: &gamificationdelivery.LeaderboardRequest{
				StartDate: now.Add(-24 * time.Hour),
				EndDate:   now,
				Limit:     200,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := svc.GetLeaderboard(context.Background(), tt.req)
			if (err != nil) != tt.wantErr {
				t.Errorf("GetLeaderboard() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestHandlerServiceGetLeaderboard_WithDepartment(t *testing.T) {
	db := newGamificationDeliveryTestDB(t)
	svc := gamificationdelivery.NewHandlerService(db)

	_ = createDeliveryTestAgent(db, 1, "技术张三", "技术支持", 300)
	_ = createDeliveryTestAgent(db, 2, "客服李四", "客户服务", 600)

	now := time.Now()
	resp, err := svc.GetLeaderboard(context.Background(), &gamificationdelivery.LeaderboardRequest{
		StartDate:  now.Add(-24 * time.Hour),
		EndDate:    now,
		Limit:      10,
		Department: "技术支持",
	})
	if err != nil {
		t.Fatalf("GetLeaderboard failed: %v", err)
	}

	for _, entry := range resp.Entries {
		if entry.Department != "技术支持" {
			t.Errorf("expected only 技术支持 department, got %s", entry.Department)
		}
	}
}

func TestHandlerServiceGetLeaderboard_NoActivity(t *testing.T) {
	db := newGamificationDeliveryTestDB(t)
	svc := gamificationdelivery.NewHandlerService(db)

	_ = createDeliveryTestAgent(db, 1, "张三", "技术支持", 300)

	now := time.Now()
	resp, err := svc.GetLeaderboard(context.Background(), &gamificationdelivery.LeaderboardRequest{
		StartDate: now.Add(-24 * time.Hour),
		EndDate:   now,
		Limit:     10,
	})
	if err != nil {
		t.Fatalf("GetLeaderboard failed: %v", err)
	}

	if len(resp.Entries) != 0 {
		t.Fatalf("expected 0 entries for agents with no activity, got %d", len(resp.Entries))
	}
}

func TestHandlerServiceGetLeaderboard_ScopedByWorkspace(t *testing.T) {
	db := newGamificationDeliveryTestDB(t)
	svc := gamificationdelivery.NewHandlerService(db)

	start := time.Now().Add(-48 * time.Hour)
	end := time.Now()

	if err := createScopedDeliveryTestAgent(db, 11, "AgentA", "技术支持", "tenant-a", "workspace-a", 120); err != nil {
		t.Fatalf("create agent A: %v", err)
	}
	if err := createScopedDeliveryTestAgent(db, 12, "AgentB", "技术支持", "tenant-a", "workspace-b", 180); err != nil {
		t.Fatalf("create agent B: %v", err)
	}

	ticketA := models.Ticket{
		ID:          101,
		CustomerID:  1,
		AgentID:     gamificationUintPtr(11),
		Status:      "resolved",
		ResolvedAt:  &[]time.Time{time.Now().Add(-24 * time.Hour)}[0],
		Priority:    "high",
		Title:       "ticket-a",
		TenantID:    "tenant-a",
		WorkspaceID: "workspace-a",
	}
	ticketB := models.Ticket{
		ID:          102,
		CustomerID:  2,
		AgentID:     gamificationUintPtr(12),
		Status:      "resolved",
		ResolvedAt:  &[]time.Time{time.Now().Add(-20 * time.Hour)}[0],
		Priority:    "high",
		Title:       "ticket-b",
		TenantID:    "tenant-a",
		WorkspaceID: "workspace-b",
	}
	if err := db.Create(&ticketA).Error; err != nil {
		t.Fatalf("create ticket A: %v", err)
	}
	if err := db.Create(&ticketB).Error; err != nil {
		t.Fatalf("create ticket B: %v", err)
	}

	csatA := models.CustomerSatisfaction{
		TenantID:    "tenant-a",
		WorkspaceID: "workspace-a",
		TicketID:    ticketA.ID,
		CustomerID:  1,
		AgentID:     gamificationUintPtr(11),
		Rating:      5,
		CreatedAt:   time.Now().Add(-23 * time.Hour),
	}
	csatB := models.CustomerSatisfaction{
		TenantID:    "tenant-a",
		WorkspaceID: "workspace-b",
		TicketID:    ticketB.ID,
		CustomerID:  2,
		AgentID:     gamificationUintPtr(12),
		Rating:      4,
		CreatedAt:   time.Now().Add(-19 * time.Hour),
	}
	if err := db.Create(&csatA).Error; err != nil {
		t.Fatalf("create csat A: %v", err)
	}
	if err := db.Create(&csatB).Error; err != nil {
		t.Fatalf("create csat B: %v", err)
	}

	resp, err := svc.GetLeaderboard(gamificationScopedContext("tenant-a", "workspace-a"), &gamificationdelivery.LeaderboardRequest{
		StartDate: start,
		EndDate:   end,
		Limit:     10,
	})
	if err != nil {
		t.Fatalf("GetLeaderboard scoped failed: %v", err)
	}
	if len(resp.Entries) != 1 {
		t.Fatalf("expected 1 scoped entry, got %d (%+v)", len(resp.Entries), resp.Entries)
	}
	if resp.Entries[0].AgentID != 11 {
		t.Fatalf("expected workspace A agent only, got %+v", resp.Entries[0])
	}
}
