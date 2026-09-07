package delivery

import (
	"context"
	"strings"
	"testing"
	"time"

	"servify/apps/server/internal/models"
)

func TestRuntimeAdapterSyncTransferAssignmentBranches(t *testing.T) {
	db := newTicketDeliveryDB(t)
	bus := &recordingBus{}
	adapter := NewRuntimeAdapter(bus)
	ctx := context.Background()

	seedTicketUser(t, db, 1, "alice")
	seedAssignableAgent(t, db, 2, "bob")
	now := time.Now()
	seedDeliveryTicket(t, db, models.Ticket{ID: 31, Title: "open one", CustomerID: 1, Status: "open", CreatedAt: now.Add(-time.Hour), UpdatedAt: now.Add(-time.Hour)})
	seedDeliveryTicket(t, db, models.Ticket{ID: 32, Title: "wip one", CustomerID: 1, Status: "in_progress", CreatedAt: now.Add(-time.Hour), UpdatedAt: now.Add(-time.Hour)})

	if err := adapter.SyncTransferAssignment(ctx, db, 999, 2, 1); err == nil {
		t.Fatal("expected error for missing ticket")
	}

	if err := adapter.SyncTransferAssignment(ctx, db, 31, 2, 1); err != nil {
		t.Fatalf("SyncTransferAssignment() error = %v", err)
	}
	var openStored models.Ticket
	if err := db.First(&openStored, 31).Error; err != nil {
		t.Fatalf("load ticket: %v", err)
	}
	if openStored.AgentID == nil || *openStored.AgentID != 2 || openStored.Status != "assigned" {
		t.Fatalf("expected open ticket to become assigned, got %+v", openStored)
	}

	if err := adapter.SyncTransferAssignment(ctx, db, 32, 2, 1); err != nil {
		t.Fatalf("SyncTransferAssignment() error = %v", err)
	}
	var wipStored models.Ticket
	if err := db.First(&wipStored, 32).Error; err != nil {
		t.Fatalf("load ticket: %v", err)
	}
	if wipStored.AgentID == nil || *wipStored.AgentID != 2 || wipStored.Status != "in_progress" {
		t.Fatalf("expected in_progress ticket unchanged status, got %+v", wipStored)
	}

	if err := db.Exec(
		`INSERT INTO tickets (title, customer_id, status, priority, source, category, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		"empty status", 1, "", "normal", "web", "general", now, now,
	).Error; err != nil {
		t.Fatalf("seed empty status ticket: %v", err)
	}
	var empty models.Ticket
	if err := db.First(&empty, "title = ?", "empty status").Error; err != nil {
		t.Fatalf("load empty status ticket: %v", err)
	}
	if err := adapter.SyncTransferAssignment(ctx, db, empty.ID, 2, 1); err != nil {
		t.Fatalf("SyncTransferAssignment() error = %v", err)
	}
	var emptyStored models.Ticket
	if err := db.First(&emptyStored, empty.ID).Error; err != nil {
		t.Fatalf("reload ticket: %v", err)
	}
	if emptyStored.AgentID == nil || *emptyStored.AgentID != 2 || emptyStored.Status != "assigned" {
		t.Fatalf("expected empty status ticket to become assigned, got %+v", emptyStored)
	}

	names := bus.published()
	if len(names) != 3 || names[0] != "ticket.assigned" {
		t.Fatalf("expected three ticket.assigned events, got %v", names)
	}

	if err := db.Exec(`CREATE TRIGGER block_ticket_update BEFORE UPDATE ON tickets
BEGIN
	SELECT RAISE(ABORT, 'ticket update blocked');
END`).Error; err != nil {
		t.Fatalf("create trigger: %v", err)
	}
	err := adapter.SyncTransferAssignment(ctx, db, 31, 2, 1)
	if err == nil || !strings.Contains(err.Error(), "blocked") {
		t.Fatalf("expected update error, got %v", err)
	}
}

func TestReaderServiceAdapterBranches(t *testing.T) {
	db := newTicketDeliveryDB(t)
	adapter := NewReaderServiceAdapter(db)
	ctx := context.Background()

	seedTicketUser(t, db, 1, "alice")
	seedDeliveryTicket(t, db, models.Ticket{ID: 7, Title: "read me", CustomerID: 1, Status: "open"})

	got, err := adapter.GetTicketByID(ctx, 7)
	if err != nil {
		t.Fatalf("GetTicketByID() error = %v", err)
	}
	if got.Title != "read me" || got.ID != 7 {
		t.Fatalf("unexpected ticket: %+v", got)
	}

	if _, err := adapter.GetTicketByID(ctx, 999); err == nil || !strings.Contains(err.Error(), "ticket not found") {
		t.Fatalf("expected ticket not found, got %v", err)
	}
}
