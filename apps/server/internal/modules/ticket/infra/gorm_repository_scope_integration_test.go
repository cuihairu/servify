//go:build integration
// +build integration

package infra

import (
	"context"
	"errors"
	"testing"
	"time"

	"servify/apps/server/internal/models"
	"servify/apps/server/internal/modules/ticket/application"
	"servify/apps/server/internal/modules/ticket/domain"
	platformauth "servify/apps/server/internal/platform/auth"

	"gorm.io/gorm"
)

func scopedTicketContext(tenantID, workspaceID string) context.Context {
	return platformauth.ContextWithScope(context.Background(), tenantID, workspaceID)
}

func TestGormRepositoryAppliesTicketScopeOnCreateAndRead(t *testing.T) {
	db := newTicketInfraTestDB(t)
	repo := NewGormRepository(db)

	ctxA := scopedTicketContext("tenant-a", "workspace-a")
	ctxB := scopedTicketContext("tenant-b", "workspace-b")
	now := time.Now()

	ticket := &domain.Ticket{
		Title:       "Scoped ticket",
		Description: "desc",
		CustomerID:  1,
		Status:      "open",
		Priority:    "normal",
		Category:    "billing",
		Source:      "web",
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := repo.CreateTicket(ctxA, ticket); err != nil {
		t.Fatalf("create ticket: %v", err)
	}

	var stored models.Ticket
	if err := db.First(&stored, ticket.ID).Error; err != nil {
		t.Fatalf("load stored ticket: %v", err)
	}
	if stored.TenantID != "tenant-a" || stored.WorkspaceID != "workspace-a" {
		t.Fatalf("unexpected ticket scope: %+v", stored)
	}

	if _, err := repo.GetTicket(ctxA, ticket.ID); err != nil {
		t.Fatalf("get ticket with matching scope: %v", err)
	}
	if _, err := repo.GetTicket(ctxB, ticket.ID); err == nil {
		t.Fatal("expected scoped lookup to reject cross-tenant ticket")
	}
}

func TestGormRepositoryFiltersTicketListsAndStatsByScope(t *testing.T) {
	db := newTicketInfraTestDB(t)
	repo := NewGormRepository(db)
	now := time.Now()

	seed := []models.Ticket{
		{
			ID:          1,
			TenantID:    "tenant-a",
			WorkspaceID: "workspace-a",
			Title:       "A",
			CustomerID:  1,
			Status:      "open",
			Priority:    "normal",
			Category:    "billing",
			Source:      "web",
			CreatedAt:   now,
			UpdatedAt:   now,
		},
		{
			ID:          2,
			TenantID:    "tenant-b",
			WorkspaceID: "workspace-b",
			Title:       "B",
			CustomerID:  2,
			Status:      "resolved",
			Priority:    "high",
			Category:    "technical",
			Source:      "web",
			CreatedAt:   now,
			UpdatedAt:   now,
		},
	}
	if err := db.Create(&seed).Error; err != nil {
		t.Fatalf("seed tickets: %v", err)
	}

	ctxA := scopedTicketContext("tenant-a", "workspace-a")
	items, total, err := repo.ListTicketModels(ctxA, application.ListTicketsQuery{Page: 1, PageSize: 20})
	if err != nil {
		t.Fatalf("list ticket models: %v", err)
	}
	if total != 1 || len(items) != 1 || items[0].ID != 1 {
		t.Fatalf("unexpected scoped tickets: total=%d items=%+v", total, items)
	}

	stats, err := repo.GetTicketStats(ctxA, nil)
	if err != nil {
		t.Fatalf("get ticket stats: %v", err)
	}
	if stats.Total != 1 || stats.Pending != 1 || stats.Resolved != 0 {
		t.Fatalf("unexpected scoped stats: %+v", stats)
	}
}

func TestGormRepositoryListTicketCustomFieldsByScope(t *testing.T) {
	db := newTicketInfraTestDB(t)
	repo := NewGormRepository(db)

	seed := []models.CustomField{
		{ID: 1, TenantID: "tenant-a", WorkspaceID: "workspace-a", Resource: "ticket", Key: "severity", Name: "Severity", Type: "select", Active: true},
		{ID: 2, TenantID: "tenant-b", WorkspaceID: "workspace-b", Resource: "ticket", Key: "region", Name: "Region", Type: "string", Active: true},
	}
	if err := db.Create(&seed).Error; err != nil {
		t.Fatalf("seed custom fields: %v", err)
	}

	fields, err := repo.ListTicketCustomFields(scopedTicketContext("tenant-a", "workspace-a"), false)
	if err != nil {
		t.Fatalf("list ticket custom fields: %v", err)
	}
	if len(fields) != 1 || fields[0].Key != "severity" {
		t.Fatalf("unexpected scoped custom fields: %+v", fields)
	}
}

func TestGormRepositoryAssignTicketModelReturnsNotFoundWhenScopedOut(t *testing.T) {
	db := newTicketInfraTestDB(t)
	repo := NewGormRepository(db)
	now := time.Now()

	ticket := &models.Ticket{
		ID:          1,
		TenantID:    "tenant-a",
		WorkspaceID: "workspace-a",
		Title:       "A",
		CustomerID:  1,
		Status:      "open",
		Priority:    "normal",
		Category:    "billing",
		Source:      "web",
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := db.Create(ticket).Error; err != nil {
		t.Fatalf("seed ticket: %v", err)
	}

	err := repo.AssignTicketModel(scopedTicketContext("tenant-b", "workspace-b"), 1, 9, nil, "open", "assigned", 99, "assign")
	if err == nil || !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("expected ErrRecordNotFound, got %v", err)
	}
}
