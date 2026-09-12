package delivery

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"servify/apps/server/internal/models"
	routingapp "servify/apps/server/internal/modules/routing/application"
	routinginfra "servify/apps/server/internal/modules/routing/infra"
)

func newRoutingDeliveryTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := uniqueMemDSN("file:routing_delivery_" + strings.ReplaceAll(t.Name(), "/", "_") + "")
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&models.TransferRecord{}, &models.WaitingRecord{}); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	return db
}

func newRoutingDeliveryAdapter(db *gorm.DB) *SessionTransferAdapter {
	return NewSessionTransferAdapter(routingapp.NewService(routinginfra.NewGormRepository(db), nil), nil)
}

func TestSessionTransferAdapter_WaitingLifecycle(t *testing.T) {
	db := newRoutingDeliveryTestDB(t)
	adapter := newRoutingDeliveryAdapter(db)

	entry, err := adapter.AddToWaitingQueue(context.Background(), nil, "sess-1", "need_help", []string{"billing", "vip"}, 0, "high", "first contact")
	if err != nil {
		t.Fatalf("AddToWaitingQueue: %v", err)
	}
	if entry.SessionID != "sess-1" || entry.Status != "waiting" {
		t.Fatalf("unexpected waiting entry: %#v", entry)
	}
	if entry.TargetSkills != "billing,vip" {
		t.Fatalf("expected comma-separated legacy skills, got %q", entry.TargetSkills)
	}

	got, err := adapter.GetWaitingRecord(context.Background(), "sess-1")
	if err != nil {
		t.Fatalf("GetWaitingRecord: %v", err)
	}
	if got.SessionID != "sess-1" || got.Priority != "high" {
		t.Fatalf("unexpected fetched waiting record: %#v", got)
	}

	list, err := adapter.ListWaitingRecords(context.Background(), "waiting", 10)
	if err != nil {
		t.Fatalf("ListWaitingRecords: %v", err)
	}
	if len(list) != 1 || list[0].SessionID != "sess-1" {
		t.Fatalf("unexpected waiting list: %#v", list)
	}

	cancelled, err := adapter.CancelWaiting(context.Background(), nil, "sess-1", "user_left")
	if err != nil {
		t.Fatalf("CancelWaiting: %v", err)
	}
	if cancelled.Status != "cancelled" || cancelled.Notes != "user_left" {
		t.Fatalf("unexpected cancelled record: %#v", cancelled)
	}
}

func TestSessionTransferAdapter_AssignAgent_And_History(t *testing.T) {
	db := newRoutingDeliveryTestDB(t)
	adapter := newRoutingDeliveryAdapter(db)
	now := time.Now().UTC().Truncate(time.Second)
	fromAgentID := uint(3)

	record, err := adapter.AssignAgent(context.Background(), nil, AssignAgentCommand{
		SessionID:      "sess-assign",
		AgentID:        9,
		FromAgentID:    &fromAgentID,
		Reason:         "manual_transfer",
		Notes:          "vip escalation",
		SessionSummary: "customer requested escalation",
		AssignedAt:     now,
	})
	if err != nil {
		t.Fatalf("AssignAgent: %v", err)
	}
	if record.SessionID != "sess-assign" || record.ToAgentID == nil || *record.ToAgentID != 9 {
		t.Fatalf("unexpected transfer record: %#v", record)
	}
	if !record.TransferredAt.Equal(now) {
		t.Fatalf("expected transfer time %v, got %v", now, record.TransferredAt)
	}

	history, err := adapter.GetTransferHistory(context.Background(), "sess-assign")
	if err != nil {
		t.Fatalf("GetTransferHistory: %v", err)
	}
	if len(history) != 1 {
		t.Fatalf("expected 1 history item, got %d", len(history))
	}
	if history[0].Reason != "manual_transfer" || history[0].Notes != "vip escalation" {
		t.Fatalf("unexpected history item: %#v", history[0])
	}

	recent, err := adapter.ListRecentTransferHistory(context.Background(), 10)
	if err != nil {
		t.Fatalf("ListRecentTransferHistory: %v", err)
	}
	if len(recent) != 1 || recent[0].SessionID != "sess-assign" {
		t.Fatalf("unexpected recent history: %#v", recent)
	}
}

func TestSessionTransferAdapter_UsesTransactionScopedRepository(t *testing.T) {
	db := newRoutingDeliveryTestDB(t)
	adapter := newRoutingDeliveryAdapter(db)

	tx := db.Begin()
	if tx.Error != nil {
		t.Fatalf("begin tx: %v", tx.Error)
	}

	entry, err := adapter.AddToWaitingQueue(context.Background(), tx, "sess-tx", "need_help", []string{"cn"}, 0, "normal", "")
	if err != nil {
		t.Fatalf("AddToWaitingQueue with tx: %v", err)
	}
	if entry.SessionID != "sess-tx" {
		t.Fatalf("unexpected tx waiting record: %#v", entry)
	}

	if err := tx.Rollback().Error; err != nil {
		t.Fatalf("rollback tx: %v", err)
	}

	_, err = adapter.GetWaitingRecord(context.Background(), "sess-tx")
	if err == nil {
		t.Fatal("expected rolled-back waiting record to be absent")
	}
}
