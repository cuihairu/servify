//go:build integration
// +build integration

package delivery_test

import (
	"context"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"servify/apps/server/internal/models"
	suggestioncontract "servify/apps/server/internal/modules/suggestion/contract"
	suggestiondelivery "servify/apps/server/internal/modules/suggestion/delivery"
	platformauth "servify/apps/server/internal/platform/auth"
)

func newSuggestionDeliveryTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	name := t.Name()
	dsn := uniqueMemDSN("file:suggestion_delivery_" + name + "")
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(
		&models.Ticket{},
		&models.KnowledgeDoc{},
	); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}
	return db
}

func suggestionScopedContext(tenantID, workspaceID string) context.Context {
	return platformauth.ContextWithScope(context.Background(), tenantID, workspaceID)
}

func TestHandlerServiceSuggest_EmptyQuery(t *testing.T) {
	db := newSuggestionDeliveryTestDB(t)
	svc := suggestiondelivery.NewHandlerService(db)

	resp, err := svc.Suggest(context.Background(), &suggestioncontract.SuggestionRequest{
		Query: "",
	})
	if err != nil {
		t.Fatalf("Suggest() error = %v", err)
	}
	if resp == nil {
		t.Fatal("expected response, got nil")
	}
	if resp.Intent.Label != "general" {
		t.Errorf("expected intent 'general', got '%s'", resp.Intent.Label)
	}
}

func TestHandlerServiceSuggest_WithTickets(t *testing.T) {
	db := newSuggestionDeliveryTestDB(t)
	svc := suggestiondelivery.NewHandlerService(db)

	ticket := &models.Ticket{
		Title:       "Login error",
		Description: "User cannot login to the system",
		Status:      "open",
		Category:    "technical",
		Priority:    "high",
	}
	if err := db.Create(ticket).Error; err != nil {
		t.Fatalf("create ticket: %v", err)
	}

	resp, err := svc.Suggest(context.Background(), &suggestioncontract.SuggestionRequest{
		Query: "login",
	})
	if err != nil {
		t.Fatalf("Suggest() error = %v", err)
	}
	if resp == nil {
		t.Fatal("expected response, got nil")
	}
}

func TestHandlerServiceSuggest_WithKnowledgeDocs(t *testing.T) {
	db := newSuggestionDeliveryTestDB(t)
	svc := suggestiondelivery.NewHandlerService(db)

	doc := &models.KnowledgeDoc{
		Title:    "API Guide",
		Content:  "How to use the API",
		Category: "Technical",
		Tags:     "api,guide",
	}
	if err := db.Create(doc).Error; err != nil {
		t.Fatalf("create doc: %v", err)
	}

	resp, err := svc.Suggest(context.Background(), &suggestioncontract.SuggestionRequest{
		Query: "api guide",
	})
	if err != nil {
		t.Fatalf("Suggest() error = %v", err)
	}
	if resp == nil {
		t.Fatal("expected response, got nil")
	}
}

func TestHandlerServiceSuggest_CustomLimits(t *testing.T) {
	db := newSuggestionDeliveryTestDB(t)
	svc := suggestiondelivery.NewHandlerService(db)

	resp, err := svc.Suggest(context.Background(), &suggestioncontract.SuggestionRequest{
		Query:              "test",
		TicketLimit:        10,
		KnowledgeDocLimit:  15,
		CandidateTicketMax: 500,
	})
	if err != nil {
		t.Fatalf("Suggest() error = %v", err)
	}
	if resp == nil {
		t.Fatal("expected response, got nil")
	}
}

func TestHandlerServiceSuggest_ScopedByWorkspace(t *testing.T) {
	db := newSuggestionDeliveryTestDB(t)
	svc := suggestiondelivery.NewHandlerService(db)

	if err := db.Create(&models.Ticket{
		Title:       "Login error A",
		Description: "User cannot login to workspace A",
		Status:      "open",
		Category:    "technical",
		Priority:    "high",
		TenantID:    "tenant-a",
		WorkspaceID: "workspace-a",
	}).Error; err != nil {
		t.Fatalf("create ticket A: %v", err)
	}
	if err := db.Create(&models.Ticket{
		Title:       "Login error B",
		Description: "User cannot login to workspace B",
		Status:      "open",
		Category:    "technical",
		Priority:    "high",
		TenantID:    "tenant-a",
		WorkspaceID: "workspace-b",
	}).Error; err != nil {
		t.Fatalf("create ticket B: %v", err)
	}
	if err := db.Create(&models.KnowledgeDoc{
		Title:       "Workspace A login guide",
		Content:     "Reset password in workspace A",
		Category:    "Technical",
		Tags:        "login,workspace-a",
		TenantID:    "tenant-a",
		WorkspaceID: "workspace-a",
	}).Error; err != nil {
		t.Fatalf("create doc A: %v", err)
	}
	if err := db.Create(&models.KnowledgeDoc{
		Title:       "Workspace B login guide",
		Content:     "Reset password in workspace B",
		Category:    "Technical",
		Tags:        "login,workspace-b",
		TenantID:    "tenant-a",
		WorkspaceID: "workspace-b",
	}).Error; err != nil {
		t.Fatalf("create doc B: %v", err)
	}

	resp, err := svc.Suggest(suggestionScopedContext("tenant-a", "workspace-a"), &suggestioncontract.SuggestionRequest{
		Query: "login workspace",
	})
	if err != nil {
		t.Fatalf("Suggest() scoped error = %v", err)
	}
	if len(resp.SimilarTickets) != 1 || resp.SimilarTickets[0].Title != "Login error A" {
		t.Fatalf("unexpected scoped tickets: %+v", resp.SimilarTickets)
	}
	if len(resp.KnowledgeDocs) != 1 || resp.KnowledgeDocs[0].Title != "Workspace A login guide" {
		t.Fatalf("unexpected scoped docs: %+v", resp.KnowledgeDocs)
	}
}
