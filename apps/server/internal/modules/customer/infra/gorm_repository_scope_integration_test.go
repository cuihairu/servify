//go:build integration
// +build integration

package infra

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"servify/apps/server/internal/models"
	customerapp "servify/apps/server/internal/modules/customer/application"
	platformauth "servify/apps/server/internal/platform/auth"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func newCustomerInfraTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := uniqueMemDSN("file:customer_infra_" + strings.ReplaceAll(t.Name(), "/", "_") + "")
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&models.User{}, &models.Customer{}, &models.Session{}, &models.Ticket{}, &models.Message{}); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	return db
}

func scopedCustomerContext(tenantID, workspaceID string) context.Context {
	return platformauth.ContextWithScope(context.Background(), tenantID, workspaceID)
}

func TestCustomerRepositoryAppliesScopeOnCreateAndRead(t *testing.T) {
	db := newCustomerInfraTestDB(t)
	repo := NewGormRepository(db)
	ctxA := scopedCustomerContext("tenant-a", "workspace-a")
	ctxB := scopedCustomerContext("tenant-b", "workspace-b")

	user, err := repo.CreateCustomer(ctxA, customerapp.CreateCustomerCommand{
		Username: "alice",
		Email:    "alice@example.com",
		Name:     "Alice",
		Source:   "web",
		Priority: "normal",
	})
	if err != nil {
		t.Fatalf("create customer: %v", err)
	}

	var stored models.Customer
	if err := db.First(&stored, "user_id = ?", user.ID).Error; err != nil {
		t.Fatalf("load customer: %v", err)
	}
	if stored.TenantID != "tenant-a" || stored.WorkspaceID != "workspace-a" {
		t.Fatalf("unexpected customer scope: %+v", stored)
	}

	if _, err := repo.GetCustomerByID(ctxA, user.ID); err != nil {
		t.Fatalf("get customer with matching scope: %v", err)
	}
	if _, err := repo.GetCustomerByID(ctxB, user.ID); err == nil {
		t.Fatal("expected cross-tenant customer lookup to fail")
	}
}

func TestCustomerRepositoryFiltersListAndStatsByScope(t *testing.T) {
	db := newCustomerInfraTestDB(t)
	repo := NewGormRepository(db)
	now := time.Now()

	users := []models.User{
		{ID: 1, Username: "alice", Email: "alice@example.com", Role: "customer", Status: "active", CreatedAt: now, UpdatedAt: now},
		{ID: 2, Username: "bob", Email: "bob@example.com", Role: "customer", Status: "active", CreatedAt: now, UpdatedAt: now},
	}
	if err := db.Create(&users).Error; err != nil {
		t.Fatalf("seed users: %v", err)
	}
	customers := []models.Customer{
		{UserID: 1, TenantID: "tenant-a", WorkspaceID: "workspace-a", Company: "A Co", Source: "web", Priority: "normal", CreatedAt: now, UpdatedAt: now},
		{UserID: 2, TenantID: "tenant-b", WorkspaceID: "workspace-b", Company: "B Co", Source: "referral", Priority: "high", CreatedAt: now, UpdatedAt: now},
	}
	if err := db.Create(&customers).Error; err != nil {
		t.Fatalf("seed customers: %v", err)
	}

	ctxA := scopedCustomerContext("tenant-a", "workspace-a")
	items, total, err := repo.ListCustomers(ctxA, customerapp.ListCustomersQuery{
		Page:      1,
		PageSize:  20,
		SortBy:    "created_at",
		SortOrder: "desc",
	})
	if err != nil {
		t.Fatalf("list customers: %v", err)
	}
	if total != 1 || len(items) != 1 || items[0].User.ID != 1 {
		t.Fatalf("unexpected scoped customer list: total=%d items=%+v", total, items)
	}

	stats, err := repo.GetStats(ctxA)
	if err != nil {
		t.Fatalf("get stats: %v", err)
	}
	if stats.Total != 1 {
		t.Fatalf("unexpected scoped stats: %+v", stats)
	}
}

func TestCustomerRepositoryGetActivityStaysScoped(t *testing.T) {
	db := newCustomerInfraTestDB(t)
	repo := NewGormRepository(db)
	now := time.Now()

	users := []models.User{
		{ID: 1, Username: "alice", Email: "alice@example.com", Role: "customer", Status: "active", CreatedAt: now, UpdatedAt: now},
	}
	if err := db.Create(&users).Error; err != nil {
		t.Fatalf("seed users: %v", err)
	}
	customers := []models.Customer{
		{UserID: 1, TenantID: "tenant-a", WorkspaceID: "workspace-a", Company: "A Co", Source: "web", Priority: "normal", CreatedAt: now, UpdatedAt: now},
	}
	if err := db.Create(&customers).Error; err != nil {
		t.Fatalf("seed customers: %v", err)
	}

	sessionA := models.Session{
		ID:          "sess-a",
		UserID:      1,
		Status:      "active",
		TenantID:    "tenant-a",
		WorkspaceID: "workspace-a",
		CreatedAt:   now.Add(-2 * time.Hour),
		UpdatedAt:   now.Add(-2 * time.Hour),
	}
	sessionB := models.Session{
		ID:          "sess-b",
		UserID:      1,
		Status:      "active",
		TenantID:    "tenant-a",
		WorkspaceID: "workspace-b",
		CreatedAt:   now.Add(-1 * time.Hour),
		UpdatedAt:   now.Add(-1 * time.Hour),
	}
	if err := db.Create(&[]models.Session{sessionA, sessionB}).Error; err != nil {
		t.Fatalf("seed sessions: %v", err)
	}

	agentID := uint(11)
	ticketA := models.Ticket{
		ID:          101,
		CustomerID:  1,
		AgentID:     &agentID,
		Title:       "ticket-a",
		Status:      "open",
		TenantID:    "tenant-a",
		WorkspaceID: "workspace-a",
		CreatedAt:   now.Add(-90 * time.Minute),
		UpdatedAt:   now.Add(-90 * time.Minute),
	}
	ticketB := models.Ticket{
		ID:          102,
		CustomerID:  1,
		AgentID:     &agentID,
		Title:       "ticket-b",
		Status:      "open",
		TenantID:    "tenant-a",
		WorkspaceID: "workspace-b",
		CreatedAt:   now.Add(-30 * time.Minute),
		UpdatedAt:   now.Add(-30 * time.Minute),
	}
	if err := db.Create(&[]models.Ticket{ticketA, ticketB}).Error; err != nil {
		t.Fatalf("seed tickets: %v", err)
	}

	messageA := models.Message{
		ID:          201,
		SessionID:   "sess-a",
		UserID:      1,
		Type:        "text",
		Content:     "visible-in-a",
		TenantID:    "tenant-a",
		WorkspaceID: "workspace-a",
		CreatedAt:   now.Add(-80 * time.Minute),
	}
	messageB := models.Message{
		ID:          202,
		SessionID:   "sess-b",
		UserID:      1,
		Type:        "text",
		Content:     "hidden-from-a",
		TenantID:    "tenant-a",
		WorkspaceID: "workspace-b",
		CreatedAt:   now.Add(-20 * time.Minute),
	}
	if err := db.Create(&[]models.Message{messageA, messageB}).Error; err != nil {
		t.Fatalf("seed messages: %v", err)
	}

	activity, err := repo.GetCustomerActivity(scopedCustomerContext("tenant-a", "workspace-a"), 1, 10)
	if err != nil {
		t.Fatalf("GetCustomerActivity: %v", err)
	}
	if len(activity.RecentSessions) != 1 || activity.RecentSessions[0].ID != "sess-a" {
		t.Fatalf("unexpected scoped sessions: %+v", activity.RecentSessions)
	}
	if len(activity.RecentTickets) != 1 || activity.RecentTickets[0].ID != 101 {
		t.Fatalf("unexpected scoped tickets: %+v", activity.RecentTickets)
	}
	if len(activity.RecentMessages) != 1 || activity.RecentMessages[0].ID != 201 {
		t.Fatalf("unexpected scoped messages: %+v", activity.RecentMessages)
	}
}

func TestCustomerRepositoryUpdateCustomerReturnsNotFoundWhenMissing(t *testing.T) {
	db := newCustomerInfraTestDB(t)
	repo := NewGormRepository(db)

	name := "updated"
	_, err := repo.UpdateCustomer(context.Background(), 999, customerapp.UpdateCustomerCommand{Name: &name})
	if err == nil || !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("expected ErrRecordNotFound, got %v", err)
	}
}

func TestCustomerRepositoryUpdateTagsReturnsNotFoundWhenMissing(t *testing.T) {
	db := newCustomerInfraTestDB(t)
	repo := NewGormRepository(db)

	err := repo.UpdateTags(context.Background(), 999, []string{"vip"})
	if err == nil || !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("expected ErrRecordNotFound, got %v", err)
	}
}
