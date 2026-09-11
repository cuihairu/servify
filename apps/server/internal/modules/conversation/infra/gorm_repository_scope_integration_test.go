//go:build integration
// +build integration

package infra

import (
	"context"
	"fmt"
	"testing"
	"time"

	"servify/apps/server/internal/models"
	"servify/apps/server/internal/modules/conversation/domain"
	platformauth "servify/apps/server/internal/platform/auth"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func newConversationInfraTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(uniqueMemDSN("file:conversation_scope")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&models.User{}, &models.Session{}, &models.Message{}); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	return db
}

func scopedConversationContext(tenantID, workspaceID string) context.Context {
	return platformauth.ContextWithScope(context.Background(), tenantID, workspaceID)
}

func TestConversationRepositoryAppliesScopeOnCreateAndRead(t *testing.T) {
	db := newConversationInfraTestDB(t)
	repo := NewGormRepository(db)
	now := time.Now()
	customerID := uint(11)

	ctxA := scopedConversationContext("tenant-a", "workspace-a")
	ctxB := scopedConversationContext("tenant-b", "workspace-b")

	conversation := &domain.Conversation{
		ID:         "conv-a",
		CustomerID: &customerID,
		Status:     domain.ConversationStatusActive,
		Channel: domain.ChannelBinding{
			Channel:   "web",
			SessionID: "conv-a",
		},
		StartedAt: now,
	}
	if err := repo.CreateConversation(ctxA, conversation); err != nil {
		t.Fatalf("create conversation: %v", err)
	}

	message := &domain.ConversationMessage{
		ConversationID: "conv-a",
		Sender:         domain.ParticipantRoleCustomer,
		Kind:           domain.MessageKindText,
		Content:        "hello",
		CreatedAt:      now,
	}
	if err := repo.AppendMessage(ctxA, message); err != nil {
		t.Fatalf("append message: %v", err)
	}

	var storedSession models.Session
	if err := db.First(&storedSession, "id = ?", "conv-a").Error; err != nil {
		t.Fatalf("load stored session: %v", err)
	}
	if storedSession.TenantID != "tenant-a" || storedSession.WorkspaceID != "workspace-a" {
		t.Fatalf("unexpected session scope: %+v", storedSession)
	}

	var storedMessage models.Message
	if err := db.First(&storedMessage, "session_id = ?", "conv-a").Error; err != nil {
		t.Fatalf("load stored message: %v", err)
	}
	if storedMessage.TenantID != "tenant-a" || storedMessage.WorkspaceID != "workspace-a" {
		t.Fatalf("unexpected message scope: %+v", storedMessage)
	}

	if _, err := repo.GetConversation(ctxA, "conv-a"); err != nil {
		t.Fatalf("get conversation with matching scope: %v", err)
	}
	if _, err := repo.GetConversation(ctxB, "conv-a"); err == nil {
		t.Fatal("expected scoped lookup to reject cross-tenant conversation")
	}
	if _, err := repo.ListRecentMessages(ctxB, "conv-a", 10); err != nil {
		t.Fatalf("list recent messages with mismatched scope returned error: %v", err)
	}
}

func TestConversationRepositoryFiltersMessagesByScope(t *testing.T) {
	db := newConversationInfraTestDB(t)
	repo := NewGormRepository(db)
	now := time.Now()

	if err := db.Create(&models.Message{
		SessionID:   "conv-shared",
		TenantID:    "tenant-a",
		WorkspaceID: "workspace-a",
		Content:     "visible",
		Type:        "text",
		Sender:      "user",
		CreatedAt:   now,
	}).Error; err != nil {
		t.Fatalf("seed message a: %v", err)
	}
	if err := db.Create(&models.Message{
		SessionID:   "conv-shared",
		TenantID:    "tenant-b",
		WorkspaceID: "workspace-b",
		Content:     "hidden",
		Type:        "text",
		Sender:      "user",
		CreatedAt:   now.Add(time.Second),
	}).Error; err != nil {
		t.Fatalf("seed message b: %v", err)
	}

	items, err := repo.ListRecentMessages(scopedConversationContext("tenant-a", "workspace-a"), "conv-shared", 10)
	if err != nil {
		t.Fatalf("list messages: %v", err)
	}
	if len(items) != 1 || items[0].Content != "visible" {
		t.Fatalf("unexpected scoped messages: %+v", items)
	}
}

func TestConversationRepositoryListMessagesBeforeScopesPivotLookup(t *testing.T) {
	db := newConversationInfraTestDB(t)
	repo := NewGormRepository(db)
	now := time.Now()

	if err := db.Create(&models.Message{
		SessionID:   "conv-shared",
		TenantID:    "tenant-a",
		WorkspaceID: "workspace-a",
		Content:     "visible",
		Type:        "text",
		Sender:      "user",
		CreatedAt:   now,
	}).Error; err != nil {
		t.Fatalf("seed visible message: %v", err)
	}
	var hidden models.Message
	hidden = models.Message{
		SessionID:   "conv-shared",
		TenantID:    "tenant-b",
		WorkspaceID: "workspace-b",
		Content:     "hidden",
		Type:        "text",
		Sender:      "user",
		CreatedAt:   now.Add(time.Second),
	}
	if err := db.Create(&hidden).Error; err != nil {
		t.Fatalf("seed hidden message: %v", err)
	}

	_, err := repo.ListMessagesBefore(scopedConversationContext("tenant-a", "workspace-a"), "conv-shared", fmt.Sprintf("%d", hidden.ID), 10)
	if err == nil {
		t.Fatal("expected scoped pivot lookup to reject cross-tenant message")
	}
}
