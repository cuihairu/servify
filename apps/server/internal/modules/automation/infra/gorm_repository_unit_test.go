package infra

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

func newAutomationUnitTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	name := strings.NewReplacer("/", "_", " ", "_").Replace(t.Name())
	dsn := uniqueMemDSN("file:automation_" + name + "")
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

func seedAutomationTicket(t *testing.T, db *gorm.DB, id uint, priority, status, tags string) *models.Ticket {
	t.Helper()
	now := time.Now()
	ticket := &models.Ticket{
		ID:        id,
		Title:     "ticket",
		Priority:  priority,
		Status:    status,
		Tags:      tags,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := db.Create(ticket).Error; err != nil {
		t.Fatalf("seed ticket: %v", err)
	}
	return ticket
}

func TestGormRepositoryCreateTriggerUnit(t *testing.T) {
	db := newAutomationUnitTestDB(t)
	repo := NewGormRepository(db)
	ctx := context.Background()

	active := false
	created, err := repo.CreateTrigger(ctx, automationapp.TriggerRequest{
		Name:  "raise",
		Event: "ticket.updated",
		Conditions: []automationapp.TriggerCondition{
			{Field: "ticket.status", Op: "eq", Value: "open"},
		},
		Actions: []automationapp.TriggerAction{
			{Type: "set_priority", Params: map[string]interface{}{"priority": "high"}},
		},
		Active: &active,
	})
	if err != nil {
		t.Fatalf("create trigger: %v", err)
	}
	if created == nil || created.ID == 0 {
		t.Fatalf("expected trigger id, got %+v", created)
	}
	if !created.Active {
		t.Fatal("expected active=false to be persisted")
	}
	if !strings.Contains(created.Conditions, "ticket.status") || !strings.Contains(created.Actions, "set_priority") {
		t.Fatalf("unexpected json payloads: conditions=%q actions=%q", created.Conditions, created.Actions)
	}

	defaultActive, err := repo.CreateTrigger(ctx, automationapp.TriggerRequest{
		Name:  "default-active",
		Event: "ticket.created",
	})
	if err != nil {
		t.Fatalf("create default trigger: %v", err)
	}
	if !defaultActive.Active {
		t.Fatal("expected active default true")
	}
}

func TestGormRepositoryCreateTriggerMarshalErrors(t *testing.T) {
	db := newAutomationUnitTestDB(t)
	repo := NewGormRepository(db)
	ctx := context.Background()

	_, err := repo.CreateTrigger(ctx, automationapp.TriggerRequest{
		Name:       "bad-cond",
		Event:      "ticket.created",
		Conditions: []automationapp.TriggerCondition{{Field: "f", Op: "eq", Value: make(chan int)}},
	})
	if err == nil || !strings.Contains(err.Error(), "invalid conditions") {
		t.Fatalf("expected invalid conditions error, got %v", err)
	}

	_, err = repo.CreateTrigger(ctx, automationapp.TriggerRequest{
		Name:    "bad-actions",
		Event:   "ticket.created",
		Actions: []automationapp.TriggerAction{{Type: "t", Params: map[string]interface{}{"bad": make(chan int)}}},
	})
	if err == nil || !strings.Contains(err.Error(), "invalid actions") {
		t.Fatalf("expected invalid actions error, got %v", err)
	}
}

func TestGormRepositoryListTriggersUnit(t *testing.T) {
	db := newAutomationUnitTestDB(t)
	repo := NewGormRepository(db)
	ctx := context.Background()

	for i, name := range []string{"a", "b", "c"} {
		if _, err := repo.CreateTrigger(ctx, automationapp.TriggerRequest{Name: name, Event: "ticket.created"}); err != nil {
			t.Fatalf("seed trigger %d: %v", i, err)
		}
	}
	triggers, err := repo.ListTriggers(ctx)
	if err != nil {
		t.Fatalf("list triggers: %v", err)
	}
	if len(triggers) != 3 {
		t.Fatalf("expected 3 triggers, got %d", len(triggers))
	}
	if triggers[0].Name != "c" || triggers[2].Name != "a" {
		t.Fatalf("expected id DESC ordering, got %+v", triggers)
	}
}

func TestGormRepositoryListActiveTriggersByEventUnit(t *testing.T) {
	db := newAutomationUnitTestDB(t)
	repo := NewGormRepository(db)
	ctx := context.Background()

	// GORM skips zero-value fields with default:true on create, so seed the
	// inactive trigger via raw SQL.
	now := time.Now()
	if err := db.Exec(
		`INSERT INTO automation_triggers (name, event, conditions, actions, active, created_at, updated_at) VALUES ('off', 'ticket.updated', '', '', 0, ?, ?)`,
		now, now,
	).Error; err != nil {
		t.Fatalf("seed off trigger: %v", err)
	}
	if _, err := repo.CreateTrigger(ctx, automationapp.TriggerRequest{Name: "first", Event: "ticket.updated"}); err != nil {
		t.Fatalf("seed first trigger: %v", err)
	}
	if _, err := repo.CreateTrigger(ctx, automationapp.TriggerRequest{Name: "other-event", Event: "ticket.created"}); err != nil {
		t.Fatalf("seed other trigger: %v", err)
	}
	if _, err := repo.CreateTrigger(ctx, automationapp.TriggerRequest{Name: "second", Event: "ticket.updated"}); err != nil {
		t.Fatalf("seed second trigger: %v", err)
	}

	triggers, err := repo.ListActiveTriggersByEvent(ctx, "ticket.updated")
	if err != nil {
		t.Fatalf("list active triggers: %v", err)
	}
	if len(triggers) != 2 {
		t.Fatalf("expected 2 active triggers, got %+v", triggers)
	}
	if triggers[0].Name != "first" || triggers[1].Name != "second" {
		t.Fatalf("expected id ASC ordering, got %+v", triggers)
	}
}

func TestGormRepositoryDeleteTriggerUnit(t *testing.T) {
	db := newAutomationUnitTestDB(t)
	repo := NewGormRepository(db)
	ctx := context.Background()

	created, err := repo.CreateTrigger(ctx, automationapp.TriggerRequest{Name: "doomed", Event: "ticket.created"})
	if err != nil {
		t.Fatalf("seed trigger: %v", err)
	}
	if err := repo.DeleteTrigger(ctx, created.ID); err != nil {
		t.Fatalf("delete trigger: %v", err)
	}
	if err := repo.DeleteTrigger(ctx, created.ID); err == nil || err.Error() != "trigger not found" {
		t.Fatalf("expected trigger not found, got %v", err)
	}
}

func TestGormRepositoryTriggerQueryErrors(t *testing.T) {
	db := newAutomationUnitTestDB(t)
	repo := NewGormRepository(db)
	ctx := context.Background()

	if _, err := repo.ListTriggers(ctx); err != nil {
		t.Fatalf("list on empty table should succeed: %v", err)
	}
	if err := db.Migrator().DropTable(&models.AutomationTrigger{}); err != nil {
		t.Fatalf("drop table: %v", err)
	}
	if _, err := repo.ListTriggers(ctx); err == nil {
		t.Fatal("expected list error after drop")
	}
	if _, err := repo.ListActiveTriggersByEvent(ctx, "ticket.created"); err == nil {
		t.Fatal("expected list active error after drop")
	}
	if _, err := repo.CreateTrigger(ctx, automationapp.TriggerRequest{Name: "n", Event: "ticket.created"}); err == nil {
		t.Fatal("expected create error after drop")
	}
	if err := repo.DeleteTrigger(ctx, 1); err == nil {
		t.Fatal("expected delete error after drop")
	}
}

func TestGormRepositoryListRunsUnit(t *testing.T) {
	db := newAutomationUnitTestDB(t)
	repo := NewGormRepository(db)
	ctx := context.Background()

	trig, err := repo.CreateTrigger(ctx, automationapp.TriggerRequest{Name: "trig", Event: "ticket.created"})
	if err != nil {
		t.Fatalf("seed trigger: %v", err)
	}
	for i := 0; i < 3; i++ {
		status := "success"
		if i == 2 {
			status = "failed"
		}
		if err := repo.RecordRun(ctx, trig.ID, uint(i+1), status, ""); err != nil {
			t.Fatalf("record run %d: %v", i, err)
		}
	}

	runs, total, err := repo.ListRuns(ctx, automationapp.RunListQuery{Page: 1, PageSize: 2})
	if err != nil {
		t.Fatalf("list runs: %v", err)
	}
	if total != 3 || len(runs) != 2 {
		t.Fatalf("expected total=3 page size=2, got total=%d len=%d", total, len(runs))
	}
	if runs[0].ID < runs[1].ID {
		t.Fatalf("expected id DESC ordering, got %+v", runs)
	}
	if runs[0].Trigger.ID != trig.ID {
		t.Fatalf("expected preloaded trigger, got %+v", runs[0].Trigger)
	}

	runs, total, err = repo.ListRuns(ctx, automationapp.RunListQuery{Page: 2, PageSize: 2})
	if err != nil {
		t.Fatalf("list runs page 2: %v", err)
	}
	if total != 3 || len(runs) != 1 {
		t.Fatalf("expected remaining run, got total=%d len=%d", total, len(runs))
	}

	runs, total, err = repo.ListRuns(ctx, automationapp.RunListQuery{Page: 1, PageSize: 10, Status: "failed"})
	if err != nil {
		t.Fatalf("list runs by status: %v", err)
	}
	if total != 1 || len(runs) != 1 || runs[0].Status != "failed" {
		t.Fatalf("unexpected status filter result: total=%d runs=%+v", total, runs)
	}

	runs, total, err = repo.ListRuns(ctx, automationapp.RunListQuery{Page: 1, PageSize: 10, TriggerID: trig.ID})
	if err != nil {
		t.Fatalf("list runs by trigger: %v", err)
	}
	if total != 3 || len(runs) != 3 {
		t.Fatalf("unexpected trigger filter result: total=%d len=%d", total, len(runs))
	}

	runs, total, err = repo.ListRuns(ctx, automationapp.RunListQuery{Page: 1, PageSize: 10, TicketID: 2})
	if err != nil {
		t.Fatalf("list runs by ticket: %v", err)
	}
	if total != 1 || len(runs) != 1 || runs[0].TicketID != 2 {
		t.Fatalf("unexpected ticket filter result: total=%d runs=%+v", total, runs)
	}

	runs, _, err = repo.ListRuns(ctx, automationapp.RunListQuery{Page: 1, PageSize: 10, TriggerID: 999})
	if err != nil {
		t.Fatalf("list runs unknown trigger: %v", err)
	}
	if len(runs) != 0 {
		t.Fatalf("expected empty result, got %+v", runs)
	}
}

func TestGormRepositoryListRunsErrors(t *testing.T) {
	db := newAutomationUnitTestDB(t)
	repo := NewGormRepository(db)
	ctx := context.Background()

	if err := db.Migrator().DropTable(&models.AutomationRun{}); err != nil {
		t.Fatalf("drop runs table: %v", err)
	}
	if _, _, err := repo.ListRuns(ctx, automationapp.RunListQuery{Page: 1, PageSize: 10}); err == nil {
		t.Fatal("expected count error after drop")
	}
	if err := repo.RecordRun(ctx, 1, 1, "success", ""); err == nil {
		t.Fatal("expected record run error after drop")
	}
}

func TestGormRepositoryListRunsPreloadError(t *testing.T) {
	db := newAutomationUnitTestDB(t)
	repo := NewGormRepository(db)
	ctx := context.Background()

	if err := repo.RecordRun(ctx, 1, 1, "success", ""); err != nil {
		t.Fatalf("seed run: %v", err)
	}
	if err := db.Migrator().DropTable(&models.AutomationTrigger{}); err != nil {
		t.Fatalf("drop triggers table: %v", err)
	}
	runs, _, err := repo.ListRuns(ctx, automationapp.RunListQuery{Page: 1, PageSize: 10})
	if err == nil {
		t.Fatalf("expected preload error, got runs=%+v", runs)
	}
}

func TestGormRepositoryTicketOperationsUnit(t *testing.T) {
	db := newAutomationUnitTestDB(t)
	repo := NewGormRepository(db)
	ctx := context.Background()

	seedAutomationTicket(t, db, 1, "normal", "open", "base")

	got, err := repo.GetTicket(ctx, 1)
	if err != nil {
		t.Fatalf("get ticket: %v", err)
	}
	if got.Priority != "normal" || got.Status != "open" || got.Tags != "base" {
		t.Fatalf("unexpected ticket: %+v", got)
	}

	if _, err := repo.GetTicket(ctx, 42); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("expected record not found, got %v", err)
	}

	if err := repo.UpdateTicketPriority(ctx, 1, "urgent"); err != nil {
		t.Fatalf("update priority: %v", err)
	}
	if err := repo.UpdateTicketTags(ctx, 1, "base,vip"); err != nil {
		t.Fatalf("update tags: %v", err)
	}
	got, err = repo.GetTicket(ctx, 1)
	if err != nil {
		t.Fatalf("reload ticket: %v", err)
	}
	if got.Priority != "urgent" || got.Tags != "base,vip" {
		t.Fatalf("unexpected updated ticket: %+v", got)
	}

	if err := repo.CreateTicketComment(ctx, 1, "auto note"); err != nil {
		t.Fatalf("create comment: %v", err)
	}
	var comment models.TicketComment
	if err := db.First(&comment, "ticket_id = ?", 1).Error; err != nil {
		t.Fatalf("load comment: %v", err)
	}
	if comment.Content != "auto note" || comment.Type != "system" || comment.UserID != 0 {
		t.Fatalf("unexpected comment: %+v", comment)
	}
}

func TestGormRepositoryTicketQueryErrors(t *testing.T) {
	db := newAutomationUnitTestDB(t)
	repo := NewGormRepository(db)
	ctx := context.Background()

	if err := db.Migrator().DropTable(&models.Ticket{}, &models.TicketComment{}); err != nil {
		t.Fatalf("drop tables: %v", err)
	}
	if _, err := repo.GetTicket(ctx, 1); err == nil {
		t.Fatal("expected get ticket error after drop")
	}
	if err := repo.UpdateTicketPriority(ctx, 1, "high"); err == nil {
		t.Fatal("expected update priority error after drop")
	}
	if err := repo.UpdateTicketTags(ctx, 1, "vip"); err == nil {
		t.Fatal("expected update tags error after drop")
	}
	if err := repo.CreateTicketComment(ctx, 1, "note"); err == nil {
		t.Fatal("expected create comment error after drop")
	}
}
