package infra

import (
	"context"
	"errors"
	assistdomain "servify/apps/server/internal/modules/assist/domain"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"servify/apps/server/internal/models"
	assistapp "servify/apps/server/internal/modules/assist/application"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

var assistMemDBSeq atomic.Uint32

func newAssistUnitTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	name := strings.NewReplacer("/", "_", " ", "_").Replace(t.Name())
	dsn := "file:assist_" + name + "_" + strconv.FormatUint(uint64(assistMemDBSeq.Add(1)), 10) + "?mode=memory&cache=shared"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(1)
	if err := db.AutoMigrate(&assistdomain.RemoteAssistSession{}, &assistdomain.RemoteAssistAnnotation{}, &models.Session{}); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Migrator().DropTable(&assistdomain.RemoteAssistSession{}, &assistdomain.RemoteAssistAnnotation{}, &models.Session{})
		_ = sqlDB.Close()
	})
	return db
}

func seedAssistConversationSession(t *testing.T, db *gorm.DB, sessionID string, userID uint) {
	t.Helper()
	if err := db.Create(&models.Session{ID: sessionID, UserID: userID, Status: "active"}).Error; err != nil {
		t.Fatalf("seed session: %v", err)
	}
}

func newAssistTestService(t *testing.T) (*assistapp.Service, *gorm.DB) {
	t.Helper()
	db := newAssistUnitTestDB(t)
	return assistapp.NewAssistService(NewGormRepository(db)), db
}

func TestAssistService_StartEndLifecycle(t *testing.T) {
	svc, db := newAssistTestService(t)
	seedAssistConversationSession(t, db, "sess-1", 5)

	session, err := svc.StartSession(context.Background(), assistapp.StartCommand{
		ConversationSessionID: "sess-1", AgentUserID: 9, TenantID: "t1", WorkspaceID: "w1",
	})
	if err != nil {
		t.Fatalf("StartSession() error = %v", err)
	}
	if session.Status != assistapp.StatusActive || session.ConversationSessionID != "sess-1" {
		t.Fatalf("unexpected session: %+v", session)
	}

	// 录制元数据随 end 落库
	ended, err := svc.EndSession(context.Background(), session.ID, assistapp.EndCommand{
		RecordingKey: "uploads/rec.webm", RecordingMime: "video/webm", RecordingDurationMs: 95000, RecordingSize: 4096,
	})
	if err != nil {
		t.Fatalf("EndSession() error = %v", err)
	}
	if ended.Status != assistapp.StatusEnded || ended.EndedAt == nil {
		t.Fatalf("unexpected ended session: %+v", ended)
	}
	if ended.RecordingKey != "uploads/rec.webm" || ended.RecordingDurationMs != 95000 {
		t.Fatalf("recording meta not persisted: %+v", ended)
	}

	// 二次 end 冲突
	if _, err := svc.EndSession(context.Background(), session.ID, assistapp.EndCommand{}); !errors.Is(err, assistapp.ErrAssistAlreadyEnded) {
		t.Fatalf("EndSession() again error = %v, want assistapp.ErrAssistAlreadyEnded", err)
	}
}

func TestAssistService_StartValidatesConversationSession(t *testing.T) {
	svc, db := newAssistTestService(t)
	seedAssistConversationSession(t, db, "sess-1", 5)

	if _, err := svc.StartSession(context.Background(), assistapp.StartCommand{ConversationSessionID: "  "}); !errors.Is(err, assistapp.ErrAssistSessionRequired) {
		t.Fatalf("empty conversation session error = %v, want assistapp.ErrAssistSessionRequired", err)
	}
	if _, err := svc.StartSession(context.Background(), assistapp.StartCommand{ConversationSessionID: "missing"}); !errors.Is(err, assistapp.ErrAssistNotFound) {
		t.Fatalf("unknown conversation session error = %v, want assistapp.ErrAssistNotFound", err)
	}
}

func TestAssistService_AttachRecordingOwnership(t *testing.T) {
	svc, db := newAssistTestService(t)
	seedAssistConversationSession(t, db, "sess-1", 5)

	session, err := svc.StartSession(context.Background(), assistapp.StartCommand{ConversationSessionID: "sess-1", AgentUserID: 9})
	if err != nil {
		t.Fatalf("StartSession() error = %v", err)
	}

	if _, err := svc.AttachRecording(context.Background(), session.ID, 6, assistapp.RecordingMeta{Key: "k"}); !errors.Is(err, assistapp.ErrAssistForbidden) {
		t.Fatalf("wrong owner error = %v, want assistapp.ErrAssistForbidden", err)
	}
	got, err := svc.AttachRecording(context.Background(), session.ID, 5, assistapp.RecordingMeta{
		Key: "uploads/rec.webm", Mime: "video/webm", DurationMs: 1000, Size: 2048,
	})
	if err != nil {
		t.Fatalf("AttachRecording() error = %v", err)
	}
	if got.RecordingKey != "uploads/rec.webm" || got.RecordingSize != 2048 {
		t.Fatalf("recording meta not attached: %+v", got)
	}
}

func TestAssistService_Annotations(t *testing.T) {
	svc, db := newAssistTestService(t)
	seedAssistConversationSession(t, db, "sess-1", 5)
	session, err := svc.StartSession(context.Background(), assistapp.StartCommand{ConversationSessionID: "sess-1"})
	if err != nil {
		t.Fatalf("StartSession() error = %v", err)
	}

	if _, err := svc.AddAnnotation(context.Background(), session.ID, assistapp.AnnotationCommand{
		TimestampMs: 12000, Shape: "rect", Payload: `{"x":0.1,"y":0.2}`,
	}); err != nil {
		t.Fatalf("AddAnnotation() error = %v", err)
	}
	if _, err := svc.AddAnnotation(context.Background(), session.ID, assistapp.AnnotationCommand{
		TimestampMs: 5000, Shape: "freehand", Payload: `{"points":[1,2,3]}`,
	}); err != nil {
		t.Fatalf("AddAnnotation() error = %v", err)
	}

	// 非法形状 / 非法 payload
	if _, err := svc.AddAnnotation(context.Background(), session.ID, assistapp.AnnotationCommand{Shape: "circle", Payload: "{}"}); !errors.Is(err, assistapp.ErrAssistShapeInvalid) {
		t.Fatalf("invalid shape error = %v, want assistapp.ErrAssistShapeInvalid", err)
	}
	if _, err := svc.AddAnnotation(context.Background(), session.ID, assistapp.AnnotationCommand{Shape: "rect", Payload: "not-json"}); !errors.Is(err, assistapp.ErrAssistPayloadInvalid) {
		t.Fatalf("invalid payload error = %v, want assistapp.ErrAssistPayloadInvalid", err)
	}
	if _, err := svc.AddAnnotation(context.Background(), 999, assistapp.AnnotationCommand{Shape: "rect", Payload: "{}"}); !errors.Is(err, assistapp.ErrAssistNotFound) {
		t.Fatalf("unknown assist error = %v, want assistapp.ErrAssistNotFound", err)
	}

	annotations, err := svc.ListAnnotations(context.Background(), session.ID)
	if err != nil {
		t.Fatalf("ListAnnotations() error = %v", err)
	}
	if len(annotations) != 2 || annotations[0].TimestampMs > annotations[1].TimestampMs {
		t.Fatalf("annotations not sorted: %+v", annotations)
	}

	// 删除后剩一条
	if err := svc.DeleteAnnotation(context.Background(), annotations[0].ID); err != nil {
		t.Fatalf("DeleteAnnotation() error = %v", err)
	}
	if err := svc.DeleteAnnotation(context.Background(), annotations[0].ID); err == nil {
		t.Fatal("deleting missing annotation should fail")
	}
	remaining, _ := svc.ListAnnotations(context.Background(), session.ID)
	if len(remaining) != 1 {
		t.Fatalf("expected 1 annotation left, got %d", len(remaining))
	}
}

func TestAssistService_ListSessions(t *testing.T) {
	svc, db := newAssistTestService(t)
	seedAssistConversationSession(t, db, "sess-1", 5)
	seedAssistConversationSession(t, db, "sess-other", 6)
	for i := 0; i < 3; i++ {
		if _, err := svc.StartSession(context.Background(), assistapp.StartCommand{ConversationSessionID: "sess-1"}); err != nil {
			t.Fatalf("StartSession() error = %v", err)
		}
	}
	if _, err := svc.StartSession(context.Background(), assistapp.StartCommand{ConversationSessionID: "sess-other"}); err != nil {
		t.Fatalf("StartSession() error = %v", err)
	}

	all, err := svc.ListSessions(context.Background(), "", 0)
	if err != nil {
		t.Fatalf("ListSessions() error = %v", err)
	}
	if len(all) != 4 {
		t.Fatalf("expected 4 sessions, got %d", len(all))
	}
	filtered, err := svc.ListSessions(context.Background(), "sess-1", 2)
	if err != nil {
		t.Fatalf("ListSessions() error = %v", err)
	}
	if len(filtered) != 2 {
		t.Fatalf("expected limit 2, got %d", len(filtered))
	}
}
