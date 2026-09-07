package delivery

import (
	"context"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"servify/apps/server/internal/models"
	ticketcontract "servify/apps/server/internal/modules/ticket/contract"
	"servify/apps/server/internal/platform/eventbus"

	"github.com/glebarez/sqlite"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

func newTicketDeliveryDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := "file:ticket_delivery_" + strings.ReplaceAll(t.Name(), "/", "_") + "?mode=memory&cache=shared"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("db handle: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() {
		_ = sqlDB.Close()
	})
	if err := db.AutoMigrate(
		&models.User{},
		&models.Agent{},
		&models.Ticket{},
		&models.TicketFile{},
		&models.CustomField{},
		&models.TicketCustomFieldValue{},
		&models.TicketStatus{},
		&models.TicketComment{},
		&models.Session{},
	); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	return db
}

func newTicketDeliveryLogger() *logrus.Logger {
	logger := logrus.New()
	logger.SetOutput(io.Discard)
	return logger
}

type recordingBus struct {
	mu     sync.Mutex
	events []string
}

func (b *recordingBus) Subscribe(eventName string, handler eventbus.Handler) {}

func (b *recordingBus) Publish(ctx context.Context, event eventbus.Event) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.events = append(b.events, event.Name())
	return nil
}

func (b *recordingBus) published() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]string(nil), b.events...)
}

func newTicketDeliveryAdapter(t *testing.T, db *gorm.DB, bus *recordingBus) *HandlerServiceAdapter {
	t.Helper()
	adapter := NewHandlerServiceWithDependencies(HandlerAssemblyDependencies{
		DB:     db,
		Logger: newTicketDeliveryLogger(),
		Bus:    bus,
	})
	if adapter == nil {
		t.Fatal("expected adapter")
	}
	return adapter
}

func seedTicketUser(t *testing.T, db *gorm.DB, id uint, name string) {
	t.Helper()
	if err := db.Create(&models.User{
		ID: id, Username: name, Email: name + "@example.com", Password: "x", Role: "customer",
	}).Error; err != nil {
		t.Fatalf("seed user %d: %v", id, err)
	}
}

func seedDeliveryTicket(t *testing.T, db *gorm.DB, ticket models.Ticket) *models.Ticket {
	t.Helper()
	if err := db.Create(&ticket).Error; err != nil {
		t.Fatalf("seed ticket: %v", err)
	}
	return &ticket
}

func TestHandlerAssemblyCreateTicketFullFlow(t *testing.T) {
	db := newTicketDeliveryDB(t)
	bus := &recordingBus{}
	adapter := newTicketDeliveryAdapter(t, db, bus)
	ctx := context.Background()

	seedTicketUser(t, db, 1, "alice")
	if err := db.Create(&models.CustomField{
		ID: 7, Resource: "ticket", Key: "severity", Name: "Severity", Type: "string", Active: true,
	}).Error; err != nil {
		t.Fatalf("seed custom field: %v", err)
	}

	got, err := adapter.CreateTicket(ctx, &ticketcontract.CreateTicketRequest{
		Title:        "need help",
		Description:  "printer on fire",
		CustomerID:   1,
		CustomFields: map[string]interface{}{"severity": "high"},
		Tags:         "vip",
	})
	if err != nil {
		t.Fatalf("CreateTicket() error = %v", err)
	}
	if got.ID == 0 || got.Status != "open" || got.Title != "need help" || got.CustomerID != 1 {
		t.Fatalf("unexpected ticket: %+v", got)
	}

	var values []models.TicketCustomFieldValue
	if err := db.Where("ticket_id = ?", got.ID).Find(&values).Error; err != nil {
		t.Fatalf("load custom field values: %v", err)
	}
	if len(values) != 1 || values[0].CustomFieldID != 7 || values[0].Value != "high" {
		t.Fatalf("unexpected custom field values: %+v", values)
	}

	var history []models.TicketStatus
	if err := db.Where("ticket_id = ?", got.ID).Find(&history).Error; err != nil {
		t.Fatalf("load status history: %v", err)
	}
	if len(history) != 1 || history[0].ToStatus != "open" || history[0].Reason != "工单创建" {
		t.Fatalf("unexpected status history: %+v", history)
	}

	names := bus.published()
	if len(names) != 1 || names[0] != "ticket.created" {
		t.Fatalf("expected ticket.created event, got %v", names)
	}
}

func TestHandlerAssemblyCreateTicketAutoAssignsAgent(t *testing.T) {
	db := newTicketDeliveryDB(t)
	bus := &recordingBus{}
	adapter := newTicketDeliveryAdapter(t, db, bus)

	seedTicketUser(t, db, 1, "alice")
	seedAssignableAgent(t, db, 2, "bob")

	got, err := adapter.CreateTicket(context.Background(), &ticketcontract.CreateTicketRequest{
		Title: "auto assign me", CustomerID: 1,
	})
	if err != nil {
		t.Fatalf("CreateTicket() error = %v", err)
	}

	var stored models.Ticket
	deadline := time.Now().Add(5 * time.Second)
	for {
		if err := db.First(&stored, got.ID).Error; err == nil && stored.AgentID != nil && *stored.AgentID == 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("auto assignment did not happen: %+v", stored)
		}
		time.Sleep(2 * time.Millisecond)
	}
	if stored.Status != "assigned" {
		t.Fatalf("expected assigned status after auto assignment, got %+v", stored)
	}

	var agent models.Agent
	if err := db.Where("user_id = ?", 2).First(&agent).Error; err != nil {
		t.Fatalf("load agent: %v", err)
	}
	if agent.CurrentLoad != 1 {
		t.Fatalf("expected agent load 1, got %d", agent.CurrentLoad)
	}

	assigned := false
	for _, name := range bus.published() {
		if name == "ticket.assigned" {
			assigned = true
		}
	}
	if !assigned {
		t.Fatalf("expected ticket.assigned event, got %v", bus.published())
	}
}

func TestHandlerAssemblyCreateTicketPrepareErrors(t *testing.T) {
	t.Run("customer missing", func(t *testing.T) {
		db := newTicketDeliveryDB(t)
		adapter := newTicketDeliveryAdapter(t, db, &recordingBus{})

		_, err := adapter.CreateTicket(context.Background(), &ticketcontract.CreateTicketRequest{
			Title: "t", CustomerID: 999,
		})
		if err == nil || !strings.Contains(err.Error(), "customer not found") {
			t.Fatalf("expected customer not found, got %v", err)
		}
	})

	t.Run("custom field listing fails", func(t *testing.T) {
		db := newTicketDeliveryDB(t)
		adapter := newTicketDeliveryAdapter(t, db, &recordingBus{})
		seedTicketUser(t, db, 1, "alice")

		if err := db.Migrator().DropTable(&models.CustomField{}); err != nil {
			t.Fatalf("drop custom fields: %v", err)
		}
		if _, err := adapter.CreateTicket(context.Background(), &ticketcontract.CreateTicketRequest{
			Title: "t", CustomerID: 1,
		}); err == nil || !strings.Contains(err.Error(), "custom_fields") {
			t.Fatalf("expected custom field listing error, got %v", err)
		}
	})
}

func TestHandlerAssemblyCreateTicketPersistError(t *testing.T) {
	db := newTicketDeliveryDB(t)
	adapter := newTicketDeliveryAdapter(t, db, &recordingBus{})
	seedTicketUser(t, db, 1, "alice")

	if err := db.Migrator().DropTable(&models.TicketStatus{}); err != nil {
		t.Fatalf("drop ticket statuses: %v", err)
	}
	if _, err := adapter.CreateTicket(context.Background(), &ticketcontract.CreateTicketRequest{
		Title: "t", CustomerID: 1,
	}); err == nil {
		t.Fatal("expected persist error")
	}
}

func TestHandlerAssemblyCreateTicketSideEffectsError(t *testing.T) {
	db := newTicketDeliveryDB(t)
	adapter := newTicketDeliveryAdapter(t, db, &recordingBus{})
	seedTicketUser(t, db, 1, "alice")

	if err := db.Exec(`CREATE TRIGGER soft_delete_ticket AFTER INSERT ON ticket_statuses
BEGIN
	UPDATE tickets SET deleted_at = '2030-01-01 00:00:00' WHERE id = NEW.ticket_id;
END`).Error; err != nil {
		t.Fatalf("create trigger: %v", err)
	}
	if _, err := adapter.CreateTicket(context.Background(), &ticketcontract.CreateTicketRequest{
		Title: "t", CustomerID: 1,
	}); err == nil || !strings.Contains(err.Error(), "ticket not found") {
		t.Fatalf("expected reload error, got %v", err)
	}
}

func TestHandlerAssemblyUpdateTicketCustomFieldLoadError(t *testing.T) {
	db := newTicketDeliveryDB(t)
	adapter := newTicketDeliveryAdapter(t, db, &recordingBus{})
	seedTicketUser(t, db, 1, "alice")
	seedDeliveryTicket(t, db, models.Ticket{ID: 3, Title: "t", CustomerID: 1, Status: "open"})

	if err := db.Migrator().DropTable(&models.CustomField{}); err != nil {
		t.Fatalf("drop custom fields: %v", err)
	}
	if _, err := adapter.UpdateTicket(context.Background(), 3, &ticketcontract.UpdateTicketRequest{
		CustomFields: map[string]interface{}{"severity": "low"},
	}, 5); err == nil || !strings.Contains(err.Error(), "custom_fields") {
		t.Fatalf("expected custom field listing error, got %v", err)
	}
}
