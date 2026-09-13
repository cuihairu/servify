package infra

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"servify/apps/server/internal/models"
	"servify/apps/server/internal/modules/webhook/application"
)

// dropDeliveryTable 制造真实的底层 DB 错误（非 ErrRecordNotFound），
// 覆盖仓储里 res.Error / 非 notfound 错误分支。
func dropDeliveryTable(t *testing.T, db interface {
	DropTable(values ...interface{}) error
}) {
	t.Helper()
	if err := db.DropTable(&models.WebhookDelivery{}); err != nil {
		t.Fatalf("drop delivery table: %v", err)
	}
}

func TestRepoGetEndpointSurfacesNonNotFoundDBError(t *testing.T) {
	db := newWebhookUnitTestDB(t)
	repo := NewGormRepository(db)
	if err := db.Migrator().DropTable(&models.WebhookEndpoint{}); err != nil {
		t.Fatalf("drop: %v", err)
	}
	_, err := repo.GetEndpoint(context.Background(), 1)
	if err == nil || errors.Is(err, application.ErrNotFound) {
		t.Fatalf("dropped table must surface a real DB error, got %v", err)
	}
}

func TestRepoDeleteEndpointSurfacesDBError(t *testing.T) {
	db := newWebhookUnitTestDB(t)
	repo := NewGormRepository(db)
	if err := db.Migrator().DropTable(&models.WebhookEndpoint{}); err != nil {
		t.Fatalf("drop: %v", err)
	}
	if err := repo.DeleteEndpoint(context.Background(), 1); err == nil || errors.Is(err, application.ErrNotFound) {
		t.Fatalf("delete on dropped table must surface DB error, got %v", err)
	}
}

func TestRepoListDeliveriesSurfacesDBError(t *testing.T) {
	db := newWebhookUnitTestDB(t)
	repo := NewGormRepository(db)
	dropDeliveryTable(t, db.Migrator())
	_, _, err := repo.ListDeliveries(context.Background(), application.DeliveryListQuery{Page: 1, PageSize: 10})
	if err == nil {
		t.Fatal("count on dropped table must surface DB error")
	}
}

func TestRepoGetDeliveryNotFoundAndDBError(t *testing.T) {
	db := newWebhookUnitTestDB(t)
	repo := NewGormRepository(db)
	ctx := context.Background()

	if _, err := repo.GetDelivery(ctx, 4242); !errors.Is(err, application.ErrNotFound) {
		t.Fatalf("missing delivery should be ErrNotFound, got %v", err)
	}
	dropDeliveryTable(t, db.Migrator())
	if _, err := repo.GetDelivery(ctx, 4242); err == nil || errors.Is(err, application.ErrNotFound) {
		t.Fatalf("dropped table must surface a real DB error, got %v", err)
	}
}

func TestRepoResetAndMarkSurfacesDBErrors(t *testing.T) {
	db := newWebhookUnitTestDB(t)
	repo := NewGormRepository(db)
	ctx := context.Background()
	now := time.Now().UTC()
	retry := now.Add(time.Minute)

	// 未命中行：RowsAffected == 0 分支（普通 fmt.Errorf，非 ErrNotFound 哨兵）
	if err := repo.MarkDeliverySuccess(ctx, 777, 200, 1, now); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("mark success on missing row = %v", err)
	}
	if err := repo.MarkDeliveryFailure(ctx, 777, 1, 500, 1, "x", false, &retry); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("mark failure on missing row = %v", err)
	}

	dropDeliveryTable(t, db.Migrator())
	if err := repo.ResetDeliveryForRedeliver(ctx, 1); err == nil {
		t.Fatal("reset on dropped table must surface DB error")
	}
	if err := repo.MarkDeliverySuccess(ctx, 1, 200, 1, now); err == nil {
		t.Fatal("mark success on dropped table must surface DB error")
	}
	if err := repo.MarkDeliveryFailure(ctx, 1, 1, 500, 1, "x", true, nil); err == nil {
		t.Fatal("mark failure on dropped table must surface DB error")
	}
}

// isDeliveryNotFoundErr 已并入上方断言：Mark 系列未命中行返回普通 fmt.Errorf。

func TestRepoSnapshotLookupsSurfacesDBErrors(t *testing.T) {
	db := newWebhookUnitTestDB(t)
	repo := NewGormRepository(db)
	ctx := context.Background()

	// 未命中：ErrNotFound 哨兵
	if _, err := repo.GetSessionSnapshot(ctx, "no-such-session"); !errors.Is(err, application.ErrNotFound) {
		t.Fatalf("missing session should be ErrNotFound, got %v", err)
	}

	// 表被删：真实 DB 错误分支
	for _, drop := range []interface{}{&models.Ticket{}, &models.Session{}, &models.VoiceCall{}} {
		if err := db.Migrator().DropTable(drop); err != nil {
			t.Fatalf("drop %T: %v", drop, err)
		}
	}
	if _, err := repo.GetTicketSnapshot(ctx, 1); err == nil || errors.Is(err, application.ErrNotFound) {
		t.Fatalf("dropped ticket table must surface DB error, got %v", err)
	}
	if _, err := repo.GetSessionSnapshot(ctx, "sess-1"); err == nil || errors.Is(err, application.ErrNotFound) {
		t.Fatalf("dropped session table must surface DB error, got %v", err)
	}
	if _, err := repo.GetCallSnapshot(ctx, "call-1"); err == nil || errors.Is(err, application.ErrNotFound) {
		t.Fatalf("dropped call table must surface DB error, got %v", err)
	}
}

func TestNewHTTPDelivererBuildsDefaultClient(t *testing.T) {
	d := NewHTTPDeliverer()
	if d.client == nil || d.client.Timeout != defaultTimeout {
		t.Fatalf("default deliverer must carry a %v timeout client", defaultTimeout)
	}
	if d.now == nil {
		t.Fatal("clock must be initialized")
	}
}

func TestHTTPDelivererSendsEventIDHeader(t *testing.T) {
	var gotEventID string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotEventID = r.Header.Get("X-Servify-Event-ID")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	result := NewHTTPDelivererWithClient(server.Client()).Deliver(context.Background(), application.DeliveryRequest{
		URL: server.URL, EventName: "ticket.created", EventID: "evt-9", Body: []byte("{}"),
	})
	if !result.Success {
		t.Fatalf("expected success, got %+v", result)
	}
	if gotEventID != "evt-9" {
		t.Fatalf("event id header = %q", gotEventID)
	}
}

func TestHTTPDelivererRejectsMalformedURL(t *testing.T) {
	deliverer := NewHTTPDelivererWithClient(http.DefaultClient)
	result := deliverer.Deliver(context.Background(), application.DeliveryRequest{
		URL:       "http://exa mple.com/hook",
		EventName: "ping",
		Body:      []byte("{}"),
	})
	if result.Success || result.StatusCode != 0 {
		t.Fatalf("malformed URL must fail before any request, got %+v", result)
	}
	if result.Error == "" || !strings.Contains(result.Error, "build request") {
		t.Fatalf("error should mention request build: %q", result.Error)
	}
}
