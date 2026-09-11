package delivery

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"servify/apps/server/internal/models"
	automationapp "servify/apps/server/internal/modules/automation/application"
)

type stubAutomationRepo struct {
	triggers      []models.AutomationTrigger
	listErr       error
	listedEvents  []string
	createErr     error
	created       []*automationapp.TriggerRequest
	deletedIDs    []uint
	deleteErr     error
	runs          []models.AutomationRun
	runsTotal     int64
	runsErr       error
	queries       []automationapp.RunListQuery
	fetched       []uint
	getTicketErr  error
	updatePriErr  error
	priority      string
	updateTagsErr error
	tags          []string
	commentErr    error
	comments      []string
	runsRecorded  []string
}

func (s *stubAutomationRepo) ListTriggers(ctx context.Context) ([]models.AutomationTrigger, error) {
	if s.listErr != nil {
		return nil, s.listErr
	}
	return s.triggers, nil
}
func (s *stubAutomationRepo) ListActiveTriggersByEvent(ctx context.Context, event string) ([]models.AutomationTrigger, error) {
	s.listedEvents = append(s.listedEvents, event)
	return s.triggers, nil
}
func (s *stubAutomationRepo) CreateTrigger(ctx context.Context, req automationapp.TriggerRequest) (*models.AutomationTrigger, error) {
	s.created = append(s.created, &req)
	if s.createErr != nil {
		return nil, s.createErr
	}
	return &models.AutomationTrigger{ID: 1, Name: req.Name, Event: req.Event}, nil
}
func (s *stubAutomationRepo) DeleteTrigger(ctx context.Context, id uint) error {
	s.deletedIDs = append(s.deletedIDs, id)
	return s.deleteErr
}
func (s *stubAutomationRepo) ListRuns(ctx context.Context, query automationapp.RunListQuery) ([]models.AutomationRun, int64, error) {
	s.queries = append(s.queries, query)
	if s.runsErr != nil {
		return nil, 0, s.runsErr
	}
	return s.runs, s.runsTotal, nil
}
func (s *stubAutomationRepo) RecordRun(ctx context.Context, triggerID uint, ticketID uint, status, message string) error {
	s.runsRecorded = append(s.runsRecorded, status)
	return nil
}
func (s *stubAutomationRepo) GetTicket(ctx context.Context, ticketID uint) (*models.Ticket, error) {
	s.fetched = append(s.fetched, ticketID)
	if s.getTicketErr != nil {
		return nil, s.getTicketErr
	}
	return &models.Ticket{ID: ticketID, Priority: "normal", Status: "open", Tags: ""}, nil
}
func (s *stubAutomationRepo) UpdateTicketPriority(ctx context.Context, ticketID uint, priority string) error {
	if s.updatePriErr != nil {
		return s.updatePriErr
	}
	s.priority = priority
	return nil
}
func (s *stubAutomationRepo) UpdateTicketTags(ctx context.Context, ticketID uint, tags string) error {
	if s.updateTagsErr != nil {
		return s.updateTagsErr
	}
	s.tags = append(s.tags, tags)
	return nil
}
func (s *stubAutomationRepo) CreateTicketComment(ctx context.Context, ticketID uint, content string) error {
	if s.commentErr != nil {
		return s.commentErr
	}
	s.comments = append(s.comments, content)
	return nil
}

func newDeliveryTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	name := strings.NewReplacer("/", "_", " ", "_").Replace(t.Name())
	dsn := uniqueMemDSN("file:automation_delivery_" + name + "")
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(1)
	if err := db.AutoMigrate(&models.Ticket{}, &models.TicketComment{}, &models.AutomationTrigger{}, &models.AutomationRun{}); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Migrator().DropTable(&models.AutomationRun{}, &models.AutomationTrigger{}, &models.TicketComment{}, &models.Ticket{})
	})
	return db
}

func TestNewHandlerServiceWithDB(t *testing.T) {
	db := newDeliveryTestDB(t)
	adapter := NewHandlerService(db)
	if adapter == nil {
		t.Fatal("expected adapter")
	}
	now := time.Now()
	if err := db.Create(&models.Ticket{ID: 3, Title: "t", Priority: "normal", Status: "open", CreatedAt: now, UpdatedAt: now}).Error; err != nil {
		t.Fatalf("seed ticket: %v", err)
	}
	resp, err := adapter.BatchRun(context.Background(), &automationapp.BatchRunRequest{
		Event:     "ticket_updated",
		TicketIDs: []uint{3},
		DryRun:    true,
	})
	if err != nil {
		t.Fatalf("BatchRun() error = %v", err)
	}
	if resp.TicketsProcessed != 1 {
		t.Fatalf("expected 1 processed ticket, got %+v", resp)
	}
}

func TestHandlerAdapterListTriggers(t *testing.T) {
	repo := &stubAutomationRepo{triggers: []models.AutomationTrigger{{ID: 2, Name: "t"}}}
	adapter := NewHandlerServiceAdapter(automationapp.NewService(repo))
	got, err := adapter.ListTriggers(context.Background())
	if err != nil {
		t.Fatalf("ListTriggers() error = %v", err)
	}
	if len(got) != 1 || got[0].ID != 2 {
		t.Fatalf("unexpected triggers: %+v", got)
	}
	repo.listErr = errors.New("list failed")
	if _, err := adapter.ListTriggers(context.Background()); err == nil {
		t.Fatal("expected error to propagate")
	}
}

func TestHandlerAdapterCreateTrigger(t *testing.T) {
	repo := &stubAutomationRepo{}
	adapter := NewHandlerServiceAdapter(automationapp.NewService(repo))

	got, err := adapter.CreateTrigger(context.Background(), nil)
	if err != nil {
		t.Fatalf("CreateTrigger(nil) error = %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil trigger, got %+v", got)
	}

	created, err := adapter.CreateTrigger(context.Background(), &automationapp.TriggerRequest{Name: "n", Event: "ticket.created"})
	if err != nil {
		t.Fatalf("CreateTrigger() error = %v", err)
	}
	if created == nil || created.Name != "n" {
		t.Fatalf("unexpected trigger: %+v", created)
	}
	if len(repo.created) != 1 || repo.created[0].Name != "n" {
		t.Fatalf("unexpected created requests: %+v", repo.created)
	}

	repo.createErr = errors.New("insert failed")
	if _, err := adapter.CreateTrigger(context.Background(), &automationapp.TriggerRequest{Name: "n2", Event: "ticket.created"}); err == nil {
		t.Fatal("expected create error to propagate")
	}
}

func TestHandlerAdapterDeleteTrigger(t *testing.T) {
	repo := &stubAutomationRepo{}
	adapter := NewHandlerServiceAdapter(automationapp.NewService(repo))
	if err := adapter.DeleteTrigger(context.Background(), 5); err != nil {
		t.Fatalf("DeleteTrigger() error = %v", err)
	}
	if len(repo.deletedIDs) != 1 || repo.deletedIDs[0] != 5 {
		t.Fatalf("unexpected deleted ids: %v", repo.deletedIDs)
	}
	repo.deleteErr = errors.New("delete failed")
	if err := adapter.DeleteTrigger(context.Background(), 5); err == nil {
		t.Fatal("expected delete error to propagate")
	}
}

func TestHandlerAdapterListRuns(t *testing.T) {
	repo := &stubAutomationRepo{runs: []models.AutomationRun{{ID: 1}}, runsTotal: 1}
	adapter := NewHandlerServiceAdapter(automationapp.NewService(repo))

	runs, total, err := adapter.ListRuns(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListRuns(nil) error = %v", err)
	}
	if len(runs) != 1 || total != 1 {
		t.Fatalf("unexpected runs=%+v total=%d", runs, total)
	}
	if q := repo.queries[0]; q.Page != 1 || q.PageSize != 20 {
		t.Fatalf("expected default query, got %+v", q)
	}

	runs, total, err = adapter.ListRuns(context.Background(), &automationapp.RunListQuery{Page: 3, PageSize: 50, Status: "failed"})
	if err != nil {
		t.Fatalf("ListRuns() error = %v", err)
	}
	if len(runs) != 1 || total != 1 {
		t.Fatalf("unexpected runs=%+v total=%d", runs, total)
	}
	if q := repo.queries[1]; q.Page != 3 || q.PageSize != 50 || q.Status != "failed" {
		t.Fatalf("expected passthrough query, got %+v", q)
	}

	repo.runsErr = errors.New("query failed")
	if _, _, err := adapter.ListRuns(context.Background(), &automationapp.RunListQuery{}); err == nil {
		t.Fatal("expected error to propagate")
	}
}

func TestHandlerAdapterBatchRun(t *testing.T) {
	repo := &stubAutomationRepo{
		triggers: []models.AutomationTrigger{{
			ID:         9,
			Event:      "ticket.updated",
			Conditions: `[{"field":"ticket.priority","op":"eq","value":"normal"}]`,
			Actions:    `[{"type":"set_priority","params":{"priority":"high"}}]`,
		}},
	}
	adapter := NewHandlerServiceAdapter(automationapp.NewService(repo))

	resp, err := adapter.BatchRun(context.Background(), nil)
	if err != nil {
		t.Fatalf("BatchRun(nil) error = %v", err)
	}
	if resp != nil {
		t.Fatalf("expected nil response, got %+v", resp)
	}

	resp, err = adapter.BatchRun(context.Background(), &automationapp.BatchRunRequest{
		Event:     "ticket_updated",
		TicketIDs: []uint{4},
	})
	if err != nil {
		t.Fatalf("BatchRun() error = %v", err)
	}
	if resp.TicketsProcessed != 1 || resp.Matches != 1 {
		t.Fatalf("unexpected response: %+v", resp)
	}
	if repo.priority != "high" {
		t.Fatalf("expected priority update, got %q", repo.priority)
	}

	if _, err := adapter.BatchRun(context.Background(), &automationapp.BatchRunRequest{Event: "bogus", TicketIDs: []uint{1}}); err == nil {
		t.Fatal("expected validation error to propagate")
	}
}
