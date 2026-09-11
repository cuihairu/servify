package delivery

import (
	"context"
	"strings"
	"testing"
	"time"

	"servify/apps/server/internal/models"
	ticketapp "servify/apps/server/internal/modules/ticket/application"
	ticketcontract "servify/apps/server/internal/modules/ticket/contract"
	ticketinfra "servify/apps/server/internal/modules/ticket/infra"

	"gorm.io/gorm"
)

func uintPtr(v uint) *uint { return &v }

func strPtrOf(v string) *string { return &v }

func TestNewHandlerServiceAdapterBranches(t *testing.T) {
	db := newTicketDeliveryDB(t)

	if NewHandlerServiceAdapter(db, nil, nil) == nil {
		t.Fatal("expected adapter with default command service")
	}
	cmd := ticketapp.NewCommandService(ticketinfra.NewGormRepository(db))
	if NewHandlerServiceAdapter(db, cmd, nil) == nil {
		t.Fatal("expected adapter with provided command service")
	}
}

func TestHandlerServiceAdapterGetTicketByIDBranches(t *testing.T) {
	db := newTicketDeliveryDB(t)
	adapter := NewHandlerServiceAdapter(db, nil, nil)
	ctx := context.Background()

	seedTicketUser(t, db, 1, "alice")
	seedDeliveryTicket(t, db, models.Ticket{ID: 5, Title: "read me", CustomerID: 1, Status: "open"})

	got, err := adapter.GetTicketByID(ctx, 5)
	if err != nil {
		t.Fatalf("GetTicketByID() error = %v", err)
	}
	if got.Title != "read me" || got.CustomerID != 1 {
		t.Fatalf("unexpected ticket: %+v", got)
	}

	if _, err := adapter.GetTicketByID(ctx, 999); err == nil || !strings.Contains(err.Error(), "ticket not found") {
		t.Fatalf("expected ticket not found, got %v", err)
	}
}

func TestHandlerServiceAdapterUpdateTicketWithCustomFieldMutation(t *testing.T) {
	db := newTicketDeliveryDB(t)
	bus := &recordingBus{}
	adapter := newTicketDeliveryAdapter(t, db, bus)
	ctx := context.Background()

	seedTicketUser(t, db, 1, "alice")
	fields := []models.CustomField{
		{ID: 1, Resource: "ticket", Key: "severity", Name: "Severity", Type: "string", Active: true},
		{ID: 2, Resource: "ticket", Key: "region", Name: "Region", Type: "string", Active: true},
	}
	if err := db.Create(&fields).Error; err != nil {
		t.Fatalf("seed custom fields: %v", err)
	}
	seedDeliveryTicket(t, db, models.Ticket{ID: 3, Title: "old", CustomerID: 1, Status: "open"})
	if err := db.Create(&models.TicketCustomFieldValue{TicketID: 3, CustomFieldID: 2, Value: "cn"}).Error; err != nil {
		t.Fatalf("seed custom field value: %v", err)
	}

	status := "resolved"
	updated, err := adapter.UpdateTicket(ctx, 3, &ticketcontract.UpdateTicketRequest{
		Status:       &status,
		CustomFields: map[string]interface{}{"severity": "low", "region": nil},
	}, 42)
	if err != nil {
		t.Fatalf("UpdateTicket() error = %v", err)
	}
	if updated.Status != "resolved" || updated.Title != "old" {
		t.Fatalf("unexpected updated ticket: %+v", updated)
	}

	var values []models.TicketCustomFieldValue
	if err := db.Where("ticket_id = ?", 3).Find(&values).Error; err != nil {
		t.Fatalf("load custom field values: %v", err)
	}
	if len(values) != 1 || values[0].CustomFieldID != 1 || values[0].Value != "low" {
		t.Fatalf("expected region deleted and severity upserted, got %+v", values)
	}

	var history []models.TicketStatus
	if err := db.Where("ticket_id = ?", 3).Find(&history).Error; err != nil {
		t.Fatalf("load status history: %v", err)
	}
	if len(history) != 1 || history[0].FromStatus != "open" || history[0].ToStatus != "resolved" || history[0].UserID != 42 {
		t.Fatalf("unexpected status history: %+v", history)
	}

	var stored models.Ticket
	if err := db.First(&stored, 3).Error; err != nil {
		t.Fatalf("load ticket: %v", err)
	}
	if stored.ResolvedAt == nil {
		t.Fatal("expected resolved_at to be set")
	}
}

func TestHandlerServiceAdapterUpdateTicketWithoutCustomFields(t *testing.T) {
	db := newTicketDeliveryDB(t)
	adapter := newTicketDeliveryAdapter(t, db, &recordingBus{})
	ctx := context.Background()

	seedTicketUser(t, db, 1, "alice")
	seedDeliveryTicket(t, db, models.Ticket{ID: 4, Title: "before", CustomerID: 1, Status: "open"})

	title := "after"
	updated, err := adapter.UpdateTicket(ctx, 4, &ticketcontract.UpdateTicketRequest{Title: &title}, 5)
	if err != nil {
		t.Fatalf("UpdateTicket() error = %v", err)
	}
	if updated.Title != "after" || updated.Status != "open" {
		t.Fatalf("unexpected updated ticket: %+v", updated)
	}

	var history []models.TicketStatus
	if err := db.Where("ticket_id = ?", 4).Find(&history).Error; err != nil {
		t.Fatalf("load status history: %v", err)
	}
	if len(history) != 0 {
		t.Fatalf("expected no status history, got %+v", history)
	}
}

func TestHandlerServiceAdapterUpdateTicketPrepareError(t *testing.T) {
	db := newTicketDeliveryDB(t)
	adapter := newTicketDeliveryAdapter(t, db, &recordingBus{})

	seedTicketUser(t, db, 1, "alice")
	seedDeliveryTicket(t, db, models.Ticket{ID: 3, Title: "t", CustomerID: 1, Status: "open"})

	status := "in_progress"
	if _, err := adapter.UpdateTicket(context.Background(), 3, &ticketcontract.UpdateTicketRequest{
		Status: &status,
	}, 5); err == nil || !strings.Contains(err.Error(), "invalid status transition") {
		t.Fatalf("expected invalid status transition, got %v", err)
	}
}

func TestHandlerServiceAdapterUpdateTicketPersistError(t *testing.T) {
	db := newTicketDeliveryDB(t)
	adapter := newTicketDeliveryAdapter(t, db, &recordingBus{})

	seedTicketUser(t, db, 1, "alice")
	seedDeliveryTicket(t, db, models.Ticket{ID: 3, Title: "t", CustomerID: 1, Status: "open"})

	if err := db.Exec(`CREATE TRIGGER abort_status_insert BEFORE INSERT ON ticket_statuses
BEGIN
	SELECT RAISE(ABORT, 'status insert blocked');
END`).Error; err != nil {
		t.Fatalf("create trigger: %v", err)
	}

	status := "resolved"
	if _, err := adapter.UpdateTicket(context.Background(), 3, &ticketcontract.UpdateTicketRequest{
		Status: &status,
	}, 5); err == nil {
		t.Fatal("expected persist error")
	}
}

func TestHandlerServiceAdapterUpdateTicketSideEffectsError(t *testing.T) {
	db := newTicketDeliveryDB(t)
	adapter := newTicketDeliveryAdapter(t, db, &recordingBus{})

	seedTicketUser(t, db, 1, "alice")
	seedDeliveryTicket(t, db, models.Ticket{ID: 3, Title: "t", CustomerID: 1, Status: "open"})

	if err := db.Exec(`CREATE TRIGGER soft_delete_ticket AFTER INSERT ON ticket_statuses
BEGIN
	UPDATE tickets SET deleted_at = '2030-01-01 00:00:00' WHERE id = NEW.ticket_id;
END`).Error; err != nil {
		t.Fatalf("create trigger: %v", err)
	}

	status := "resolved"
	if _, err := adapter.UpdateTicket(context.Background(), 3, &ticketcontract.UpdateTicketRequest{
		Status: &status,
	}, 5); err == nil || !strings.Contains(err.Error(), "ticket not found") {
		t.Fatalf("expected reload error, got %v", err)
	}
}

func TestHandlerServiceAdapterListTicketsBranches(t *testing.T) {
	db := newTicketDeliveryDB(t)
	adapter := newTicketDeliveryAdapter(t, db, &recordingBus{})
	ctx := context.Background()

	seedTicketUser(t, db, 1, "alice")
	seedTicketUser(t, db, 2, "bob")
	now := time.Now()
	seedDeliveryTicket(t, db, models.Ticket{
		ID: 11, Title: "printer help", CustomerID: 1, Status: "open", Priority: "high",
		Category: "billing", Source: "web", Tags: "vip,urgent", CreatedAt: now.Add(-2 * time.Hour), UpdatedAt: now.Add(-2 * time.Hour),
	})
	seedDeliveryTicket(t, db, models.Ticket{
		ID: 12, Title: "other topic", CustomerID: 2, AgentID: uintPtr(2), Status: "resolved", Priority: "low",
		Category: "technical", Source: "email", CreatedAt: now.Add(-time.Hour), UpdatedAt: now.Add(-time.Hour),
	})

	items, total, err := adapter.ListTickets(ctx, &ticketcontract.ListTicketRequest{
		Page: 1, PageSize: 20,
		Status:     []string{"open"},
		Priority:   []string{"high"},
		Category:   []string{"billing"},
		Source:     []string{"web"},
		Tag:        "vip",
		CustomerID: uintPtr(1),
		Search:     "printer",
		SortBy:     "priority",
		SortOrder:  "asc",
	})
	if err != nil {
		t.Fatalf("ListTickets() error = %v", err)
	}
	if total != 1 || len(items) != 1 || items[0].ID != 11 {
		t.Fatalf("unexpected list result: total=%d items=%+v", total, items)
	}

	items, total, err = adapter.ListTickets(ctx, &ticketcontract.ListTicketRequest{Page: 1, PageSize: 20})
	if err != nil {
		t.Fatalf("ListTickets() error = %v", err)
	}
	if total != 2 || len(items) != 2 {
		t.Fatalf("expected 2 tickets, got total=%d items=%+v", total, items)
	}

	if err := db.Migrator().DropTable(&models.Ticket{}); err != nil {
		t.Fatalf("drop tickets: %v", err)
	}
	if _, _, err := adapter.ListTickets(ctx, &ticketcontract.ListTicketRequest{Page: 1, PageSize: 20}); err == nil {
		t.Fatal("expected list error")
	}
}

func TestHandlerServiceAdapterListTicketCustomFieldsBranches(t *testing.T) {
	db := newTicketDeliveryDB(t)
	adapter := newTicketDeliveryAdapter(t, db, &recordingBus{})
	ctx := context.Background()

	fields := []models.CustomField{
		{ID: 1, Resource: "ticket", Key: "severity", Name: "Severity", Type: "string", Active: true},
		{ID: 2, Resource: "customer", Key: "segment", Name: "Segment", Type: "string", Active: true},
		{ID: 3, Resource: "ticket", Key: "region", Name: "Region", Type: "string", Active: false},
	}
	if err := db.Create(&fields).Error; err != nil {
		t.Fatalf("seed custom fields: %v", err)
	}
	if err := db.Model(&models.CustomField{}).Where("id = ?", 3).Update("active", false).Error; err != nil {
		t.Fatalf("deactivate custom field: %v", err)
	}

	active, err := adapter.ListTicketCustomFields(ctx, true)
	if err != nil {
		t.Fatalf("ListTicketCustomFields(true) error = %v", err)
	}
	if len(active) != 1 || active[0].Key != "severity" {
		t.Fatalf("unexpected active fields: %+v", active)
	}

	all, err := adapter.ListTicketCustomFields(ctx, false)
	if err != nil {
		t.Fatalf("ListTicketCustomFields(false) error = %v", err)
	}
	if len(all) != 2 || all[0].Key != "severity" || all[1].Key != "region" {
		t.Fatalf("unexpected fields: %+v", all)
	}

	if err := db.Migrator().DropTable(&models.CustomField{}); err != nil {
		t.Fatalf("drop custom fields: %v", err)
	}
	if _, err := adapter.ListTicketCustomFields(ctx, false); err == nil {
		t.Fatal("expected list error")
	}
}

func seedAssignableAgent(t *testing.T, db *gorm.DB, userID uint, name string) {
	t.Helper()
	seedTicketUser(t, db, userID, name)
	if err := db.Create(&models.Agent{
		UserID: userID, Status: "online", MaxConcurrent: 5, CurrentLoad: 0,
	}).Error; err != nil {
		t.Fatalf("seed agent: %v", err)
	}
}

func TestHandlerServiceAdapterAssignTicketBranches(t *testing.T) {
	db := newTicketDeliveryDB(t)
	bus := &recordingBus{}
	adapter := newTicketDeliveryAdapter(t, db, bus)
	ctx := context.Background()

	seedTicketUser(t, db, 1, "alice")
	seedAssignableAgent(t, db, 2, "bob")
	seedDeliveryTicket(t, db, models.Ticket{ID: 8, Title: "t", CustomerID: 1, Status: "open"})

	if err := adapter.AssignTicket(ctx, 999, 2, 99); err == nil {
		t.Fatal("expected load error for missing ticket")
	}

	if err := adapter.AssignTicket(ctx, 8, 2, 99); err != nil {
		t.Fatalf("AssignTicket() error = %v", err)
	}
	var stored models.Ticket
	if err := db.First(&stored, 8).Error; err != nil {
		t.Fatalf("load ticket: %v", err)
	}
	if stored.AgentID == nil || *stored.AgentID != 2 || stored.Status != "assigned" {
		t.Fatalf("unexpected ticket after assign: %+v", stored)
	}
	var agent models.Agent
	if err := db.Where("user_id = ?", 2).First(&agent).Error; err != nil {
		t.Fatalf("load agent: %v", err)
	}
	if agent.CurrentLoad != 1 {
		t.Fatalf("expected agent load 1, got %d", agent.CurrentLoad)
	}
	names := bus.published()
	if len(names) != 1 || names[0] != "ticket.assigned" {
		t.Fatalf("expected ticket.assigned event, got %v", names)
	}

	if err := adapter.AssignTicket(ctx, 8, 2, 99); err != nil {
		t.Fatalf("AssignTicket() same agent error = %v", err)
	}
	if err := db.Where("user_id = ?", 2).First(&agent).Error; err != nil {
		t.Fatalf("load agent: %v", err)
	}
	if agent.CurrentLoad != 1 {
		t.Fatalf("expected agent load unchanged, got %d", agent.CurrentLoad)
	}

	if err := adapter.AssignTicket(ctx, 8, 777, 99); err == nil || !strings.Contains(err.Error(), "agent not available") {
		t.Fatalf("expected agent not available, got %v", err)
	}
}

func TestHandlerServiceAdapterAssignTicketReloadError(t *testing.T) {
	db := newTicketDeliveryDB(t)
	adapter := newTicketDeliveryAdapter(t, db, &recordingBus{})

	seedTicketUser(t, db, 1, "alice")
	seedAssignableAgent(t, db, 2, "bob")
	seedDeliveryTicket(t, db, models.Ticket{ID: 8, Title: "t", CustomerID: 1, Status: "open"})

	if err := db.Exec(`CREATE TRIGGER soft_delete_ticket AFTER INSERT ON ticket_statuses
BEGIN
	UPDATE tickets SET deleted_at = '2030-01-01 00:00:00' WHERE id = NEW.ticket_id;
END`).Error; err != nil {
		t.Fatalf("create trigger: %v", err)
	}
	if err := adapter.AssignTicket(context.Background(), 8, 2, 99); err == nil || !strings.Contains(err.Error(), "ticket not found") {
		t.Fatalf("expected reload error, got %v", err)
	}
}

func TestHandlerServiceAdapterAddCommentBranches(t *testing.T) {
	db := newTicketDeliveryDB(t)
	adapter := newTicketDeliveryAdapter(t, db, &recordingBus{})
	ctx := context.Background()

	seedTicketUser(t, db, 1, "alice")
	seedTicketUser(t, db, 3, "carol")
	seedDeliveryTicket(t, db, models.Ticket{ID: 5, Title: "t", CustomerID: 1, Status: "open"})

	comment, err := adapter.AddComment(ctx, 5, 3, "hello", "note")
	if err != nil {
		t.Fatalf("AddComment() error = %v", err)
	}
	if comment.ID == 0 || comment.TicketID != 5 || comment.UserID != 3 || comment.Content != "hello" || comment.Type != "note" {
		t.Fatalf("unexpected comment: %+v", comment)
	}

	if _, err := adapter.AddComment(ctx, 5, 3, "   ", ""); err == nil {
		t.Fatal("expected content required error")
	}
	if _, err := adapter.AddComment(ctx, 999, 3, "x", ""); err == nil {
		t.Fatal("expected missing ticket error")
	}
}

func TestHandlerServiceAdapterCloseTicketBranches(t *testing.T) {
	db := newTicketDeliveryDB(t)
	bus := &recordingBus{}
	adapter := newTicketDeliveryAdapter(t, db, bus)
	ctx := context.Background()

	seedTicketUser(t, db, 1, "alice")
	seedTicketUser(t, db, 9, "dave")
	seedDeliveryTicket(t, db, models.Ticket{ID: 6, Title: "t", CustomerID: 1, Status: "open"})

	if err := adapter.CloseTicket(ctx, 6, 9, "done"); err != nil {
		t.Fatalf("CloseTicket() error = %v", err)
	}
	var stored models.Ticket
	if err := db.First(&stored, 6).Error; err != nil {
		t.Fatalf("load ticket: %v", err)
	}
	if stored.Status != "closed" || stored.ClosedAt == nil {
		t.Fatalf("unexpected ticket after close: %+v", stored)
	}
	var comments []models.TicketComment
	if err := db.Where("ticket_id = ? AND type = ?", 6, "system").Find(&comments).Error; err != nil {
		t.Fatalf("load comments: %v", err)
	}
	if len(comments) != 1 || !strings.Contains(comments[0].Content, "工单已关闭") || !strings.Contains(comments[0].Content, "done") {
		t.Fatalf("unexpected system comment: %+v", comments)
	}
	names := bus.published()
	if len(names) != 1 || names[0] != "ticket.closed" {
		t.Fatalf("expected ticket.closed event, got %v", names)
	}

	seedDeliveryTicket(t, db, models.Ticket{ID: 16, Title: "t2", CustomerID: 1, Status: "open"})
	if err := db.Exec(`CREATE TRIGGER abort_status_insert BEFORE INSERT ON ticket_statuses
BEGIN
	SELECT RAISE(ABORT, 'status insert blocked');
END`).Error; err != nil {
		t.Fatalf("create trigger: %v", err)
	}
	if err := adapter.CloseTicket(ctx, 16, 9, "boom"); err == nil {
		t.Fatal("expected close error")
	}
}

func TestHandlerServiceAdapterBulkUpdateTicketsBranches(t *testing.T) {
	db := newTicketDeliveryDB(t)
	adapter := newTicketDeliveryAdapter(t, db, &recordingBus{})
	ctx := context.Background()

	seedTicketUser(t, db, 1, "alice")
	seedDeliveryTicket(t, db, models.Ticket{ID: 21, Title: "t", CustomerID: 1, Status: "open"})

	status := "resolved"
	result, err := adapter.BulkUpdateTickets(ctx, &ticketcontract.BulkUpdateTicketRequest{
		TicketIDs: []uint{21, 22},
		Status:    &status,
		SetTags:   strPtrOf("a,b"),
	}, 5)
	if err != nil {
		t.Fatalf("BulkUpdateTickets() error = %v", err)
	}
	if len(result.Updated) != 1 || result.Updated[0] != 21 {
		t.Fatalf("unexpected updated ids: %+v", result.Updated)
	}
	if len(result.Failed) != 1 || result.Failed[0].TicketID != 22 || result.Failed[0].Error == "" {
		t.Fatalf("unexpected failures: %+v", result.Failed)
	}
	var stored models.Ticket
	if err := db.First(&stored, 21).Error; err != nil {
		t.Fatalf("load ticket: %v", err)
	}
	if stored.Status != "resolved" || stored.Tags != "a,b" {
		t.Fatalf("unexpected ticket after bulk update: %+v", stored)
	}

	if _, err := adapter.BulkUpdateTickets(ctx, &ticketcontract.BulkUpdateTicketRequest{}, 5); err == nil {
		t.Fatal("expected ticket ids required error")
	}
	if _, err := adapter.BulkUpdateTickets(ctx, &ticketcontract.BulkUpdateTicketRequest{
		TicketIDs: []uint{21}, AgentID: uintPtr(2), UnassignAgent: true,
	}, 5); err == nil {
		t.Fatal("expected conflict error")
	}
}

func TestHandlerServiceAdapterGetRelatedConversationsBranches(t *testing.T) {
	db := newTicketDeliveryDB(t)
	adapter := newTicketDeliveryAdapter(t, db, &recordingBus{})
	ctx := context.Background()

	seedTicketUser(t, db, 1, "alice")
	now := time.Now()
	sessions := []models.Session{
		{ID: "s1", UserID: 1, TicketID: uintPtr(5), Status: "ended", StartedAt: now.Add(-time.Hour), CreatedAt: now, UpdatedAt: now},
		{ID: "s2", UserID: 1, TicketID: uintPtr(5), Status: "ended", StartedAt: now.Add(-2 * time.Hour), CreatedAt: now, UpdatedAt: now},
		{ID: "s3", UserID: 1, Status: "active", StartedAt: now, CreatedAt: now, UpdatedAt: now},
	}
	if err := db.Create(&sessions).Error; err != nil {
		t.Fatalf("seed sessions: %v", err)
	}

	got, err := adapter.GetRelatedConversations(ctx, 5)
	if err != nil {
		t.Fatalf("GetRelatedConversations() error = %v", err)
	}
	if len(got) != 2 || got[0].ID != "s1" || got[1].ID != "s2" {
		t.Fatalf("unexpected sessions: %+v", got)
	}
	if got[0].User.ID != 1 {
		t.Fatalf("expected user preload, got %+v", got[0].User)
	}

	if err := db.Migrator().DropTable(&models.Session{}); err != nil {
		t.Fatalf("drop sessions: %v", err)
	}
	if _, err := adapter.GetRelatedConversations(ctx, 5); err == nil {
		t.Fatal("expected query error")
	}
}
