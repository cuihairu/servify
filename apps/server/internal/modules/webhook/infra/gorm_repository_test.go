package infra

import (
	"context"
	"errors"
	webhookdomain "servify/apps/server/internal/modules/webhook/domain"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"servify/apps/server/internal/models"
	"servify/apps/server/internal/modules/webhook/application"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

var memSQLiteDBSeq atomic.Uint32

// uniqueMemDSN 给命名内存库 DSN 追加全局唯一序号，
// 避免 -count 重跑、-run 反复执行或并行测试命中同一命名库。
func uniqueMemDSN(base string) string {
	return base + "_" + strconv.FormatUint(uint64(memSQLiteDBSeq.Add(1)), 10) + "?mode=memory&cache=shared"
}

func newWebhookUnitTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	name := strings.NewReplacer("/", "_", " ", "_").Replace(t.Name())
	dsn := uniqueMemDSN("file:webhook_" + name + "")
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(1)
	if err := db.AutoMigrate(&webhookdomain.WebhookEndpoint{}, &webhookdomain.WebhookDelivery{}, &models.Ticket{}, &models.Session{}, &models.VoiceCall{}); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Migrator().DropTable(&webhookdomain.WebhookDelivery{}, &webhookdomain.WebhookEndpoint{}, &models.Ticket{}, &models.Session{}, &models.VoiceCall{})
	})
	return db
}

func TestEndpointLifecycle(t *testing.T) {
	db := newWebhookUnitTestDB(t)
	repo := NewGormRepository(db)
	ctx := context.Background()

	ep := &webhookdomain.WebhookEndpoint{Name: "ops", URL: "https://ops.example.com/hook", Secret: "s", Active: true}
	if err := repo.CreateEndpoint(ctx, ep); err != nil {
		t.Fatalf("create: %v", err)
	}
	got, err := repo.GetEndpoint(ctx, ep.ID)
	if err != nil || got.Name != "ops" {
		t.Fatalf("get: %v %+v", err, got)
	}
	got.URL = "https://ops.example.com/v2"
	if err := repo.UpdateEndpoint(ctx, got); err != nil {
		t.Fatalf("update: %v", err)
	}
	list, err := repo.ListEndpoints(ctx)
	if err != nil || len(list) != 1 {
		t.Fatalf("list: %v %d", err, len(list))
	}
	if err := repo.DeleteEndpoint(ctx, ep.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := repo.GetEndpoint(ctx, ep.ID); !errors.Is(err, application.ErrNotFound) {
		t.Fatalf("expected ErrNotFound after delete, got %v", err)
	}
	if err := repo.DeleteEndpoint(ctx, 999); !errors.Is(err, application.ErrNotFound) {
		t.Fatalf("delete missing should be ErrNotFound, got %v", err)
	}
}

func TestDeliveryClaimsAndResults(t *testing.T) {
	db := newWebhookUnitTestDB(t)
	repo := NewGormRepository(db)
	ctx := context.Background()

	future := time.Now().Add(time.Hour).UTC()
	rows := []webhookdomain.WebhookDelivery{
		{EndpointID: 1, EventName: "ticket.created", Status: webhookdomain.WebhookDeliveryStatusPending, Payload: "{}"},
		{EndpointID: 1, EventName: "ticket.assigned", Status: webhookdomain.WebhookDeliveryStatusPending, Payload: "{}", NextRetryAt: &future},
		{EndpointID: 1, EventName: "ticket.closed", Status: webhookdomain.WebhookDeliveryStatusDead, Payload: "{}"},
	}
	for i := range rows {
		if err := repo.CreateDelivery(ctx, &rows[i]); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	due, err := repo.ClaimDueDeliveries(ctx, time.Now().UTC(), 10)
	if err != nil || len(due) != 1 || due[0].EventName != "ticket.created" {
		t.Fatalf("only immediately-due pending rows should be claimed: %v %+v", err, due)
	}

	if err := repo.MarkDeliverySuccess(ctx, rows[0].ID, 200, 12, time.Now().UTC()); err != nil {
		t.Fatalf("success: %v", err)
	}
	successRow, _ := repo.GetDelivery(ctx, rows[0].ID)
	if successRow.Status != webhookdomain.WebhookDeliveryStatusSuccess || successRow.Attempt != 1 || successRow.DeliveredAt == nil {
		t.Fatalf("unexpected success row: %+v", successRow)
	}

	next := time.Now().Add(time.Minute).UTC()
	if err := repo.MarkDeliveryFailure(ctx, rows[1].ID, 3, 500, 40, "boom", false, &next); err != nil {
		t.Fatalf("failure: %v", err)
	}
	failedRow, _ := repo.GetDelivery(ctx, rows[1].ID)
	if failedRow.Attempt != 3 || failedRow.NextRetryAt == nil {
		t.Fatalf("unexpected retried row: %+v", failedRow)
	}

	if err := repo.MarkDeliveryFailure(ctx, rows[2].ID, 7, 0, 0, "gave up", true, nil); err != nil {
		t.Fatalf("dead: %v", err)
	}
	deadRow, _ := repo.GetDelivery(ctx, rows[2].ID)
	if deadRow.Status != webhookdomain.WebhookDeliveryStatusDead {
		t.Fatalf("unexpected dead row: %+v", deadRow)
	}
}

func TestResetDeliveryForRedeliver(t *testing.T) {
	db := newWebhookUnitTestDB(t)
	repo := NewGormRepository(db)
	ctx := context.Background()

	row := webhookdomain.WebhookDelivery{EndpointID: 1, EventName: "e", Status: webhookdomain.WebhookDeliveryStatusFailed, Attempt: 6, LastError: "x"}
	if err := repo.CreateDelivery(ctx, &row); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := repo.ResetDeliveryForRedeliver(ctx, row.ID); err != nil {
		t.Fatalf("reset: %v", err)
	}
	got, _ := repo.GetDelivery(ctx, row.ID)
	if got.Status != webhookdomain.WebhookDeliveryStatusPending || got.Attempt != 0 || got.NextRetryAt != nil || got.LastError != "" {
		t.Fatalf("reset should clear retry state: %+v", got)
	}
	if err := repo.ResetDeliveryForRedeliver(ctx, 999); !errors.Is(err, application.ErrNotFound) {
		t.Fatalf("reset missing should be ErrNotFound, got %v", err)
	}
}

func TestListDeliveriesFilterAndPage(t *testing.T) {
	db := newWebhookUnitTestDB(t)
	repo := NewGormRepository(db)
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		status := webhookdomain.WebhookDeliveryStatusSuccess
		if i%2 == 0 {
			status = webhookdomain.WebhookDeliveryStatusPending
		}
		row := webhookdomain.WebhookDelivery{EndpointID: uint(i + 1), EventName: "e", Status: status, Payload: "{}"}
		if err := repo.CreateDelivery(ctx, &row); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	items, total, err := repo.ListDeliveries(ctx, application.DeliveryListQuery{Status: webhookdomain.WebhookDeliveryStatusPending, Page: 1, PageSize: 2})
	if err != nil || total != 3 || len(items) != 2 {
		t.Fatalf("filter: %v total=%d len=%d", err, total, len(items))
	}
	items, total, err = repo.ListDeliveries(ctx, application.DeliveryListQuery{EndpointID: 2, Page: 1, PageSize: 10})
	if err != nil || total != 1 {
		t.Fatalf("endpoint filter: %v total=%d", err, total)
	}
}

func TestSnapshotLookups(t *testing.T) {
	db := newWebhookUnitTestDB(t)
	repo := NewGormRepository(db)
	ctx := context.Background()

	if err := db.Create(&models.Ticket{ID: 9, Title: "printer on fire"}).Error; err != nil {
		t.Fatalf("seed ticket: %v", err)
	}
	if err := db.Create(&models.Session{ID: "sess-42"}).Error; err != nil {
		t.Fatalf("seed session: %v", err)
	}
	if err := db.Create(&models.VoiceCall{ID: "call-42", SessionID: "sess-42", Status: "started", StartedAt: time.Now(), CreatedAt: time.Now(), UpdatedAt: time.Now()}).Error; err != nil {
		t.Fatalf("seed call: %v", err)
	}

	ticket, err := repo.GetTicketSnapshot(ctx, 9)
	if err != nil || ticket.Title != "printer on fire" {
		t.Fatalf("ticket snapshot: %v %+v", err, ticket)
	}
	session, err := repo.GetSessionSnapshot(ctx, "sess-42")
	if err != nil || session.ID != "sess-42" {
		t.Fatalf("session snapshot: %v %+v", err, session)
	}
	call, err := repo.GetCallSnapshot(ctx, "call-42")
	if err != nil || call.ID != "call-42" || call.SessionID != "sess-42" {
		t.Fatalf("call snapshot: %v %+v", err, call)
	}
	if _, err := repo.GetTicketSnapshot(ctx, 1); !errors.Is(err, application.ErrNotFound) {
		t.Fatalf("missing ticket should be ErrNotFound, got %v", err)
	}
	if _, err := repo.GetCallSnapshot(ctx, "call-none"); !errors.Is(err, application.ErrNotFound) {
		t.Fatalf("missing call should be ErrNotFound, got %v", err)
	}
}
