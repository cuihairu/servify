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
	platformauth "servify/apps/server/internal/platform/auth"

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

	scoped := platformauth.ContextWithScope(context.Background(), "t1", "w1")
	session, err := svc.StartSession(scoped, assistapp.StartCommand{
		ConversationSessionID: "sess-1", AgentUserID: 9,
	})
	if err != nil {
		t.Fatalf("StartSession() error = %v", err)
	}
	if session.Status != assistapp.StatusActive || session.ConversationSessionID != "sess-1" {
		t.Fatalf("unexpected session: %+v", session)
	}
	if session.ConsentStatus != assistapp.ConsentPending {
		t.Fatalf("want consent pending, got %q", session.ConsentStatus)
	}
	if session.TenantID != "t1" || session.WorkspaceID != "w1" {
		t.Fatalf("scope not persisted from ctx: %+v", session)
	}

	// 同会话二次发起被单活跃约束拒绝（RA-2）
	if _, err := svc.StartSession(scoped, assistapp.StartCommand{ConversationSessionID: "sess-1"}); !errors.Is(err, assistapp.ErrAssistSessionActive) {
		t.Fatalf("second start error = %v, want assistapp.ErrAssistSessionActive", err)
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
	if err := svc.DeleteAnnotation(context.Background(), annotations[0].ID); !errors.Is(err, assistapp.ErrAssistAnnotationNotFound) {
		t.Fatalf("deleting missing annotation error = %v, want assistapp.ErrAssistAnnotationNotFound", err)
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
	// 单活跃约束：同会话多条记录须经 end 收口后再发起
	for i := 0; i < 3; i++ {
		session, err := svc.StartSession(context.Background(), assistapp.StartCommand{ConversationSessionID: "sess-1"})
		if err != nil {
			t.Fatalf("StartSession() #%d error = %v", i, err)
		}
		if i < 2 {
			if _, err := svc.EndSession(context.Background(), session.ID, assistapp.EndCommand{}); err != nil {
				t.Fatalf("EndSession() #%d error = %v", i, err)
			}
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

// TestAssistRepositoryScopeIsolation RA-1：读取面按 ctx scope 过滤，
// 跨租户/工作区命中与不存在同返 not found（404 语义）。
func TestAssistRepositoryScopeIsolation(t *testing.T) {
	svc, db := newAssistTestService(t)
	seedAssistConversationSession(t, db, "sess-a", 5)
	seedAssistConversationSession(t, db, "sess-b", 6)

	tenantA := platformauth.ContextWithScope(context.Background(), "tenant-a", "")
	tenantB := platformauth.ContextWithScope(context.Background(), "tenant-b", "")

	sessionA, err := svc.StartSession(tenantA, assistapp.StartCommand{ConversationSessionID: "sess-a"})
	if err != nil {
		t.Fatalf("StartSession() A error = %v", err)
	}
	sessionB, err := svc.StartSession(tenantB, assistapp.StartCommand{ConversationSessionID: "sess-b"})
	if err != nil {
		t.Fatalf("StartSession() B error = %v", err)
	}

	// GetSession：跨 scope 不可见（ErrAssistNotFound），本 scope 可见
	if _, err := svc.GetSession(tenantA, sessionB.ID); !errors.Is(err, assistapp.ErrAssistNotFound) {
		t.Fatalf("cross-tenant GetSession error = %v, want assistapp.ErrAssistNotFound", err)
	}
	if _, err := svc.GetSession(tenantA, sessionA.ID); err != nil {
		t.Fatalf("same-tenant GetSession() error = %v", err)
	}

	// EndSession：跨 scope 结束被拒
	if _, err := svc.EndSession(tenantA, sessionB.ID, assistapp.EndCommand{}); !errors.Is(err, assistapp.ErrAssistNotFound) {
		t.Fatalf("cross-tenant EndSession error = %v, want assistapp.ErrAssistNotFound", err)
	}

	// ListSessions：只看到本租户记录
	listA, err := svc.ListSessions(tenantA, "", 0)
	if err != nil {
		t.Fatalf("ListSessions() A error = %v", err)
	}
	if len(listA) != 1 || listA[0].ID != sessionA.ID {
		t.Fatalf("tenant A should only see its session, got %+v", listA)
	}

	// 单活跃查重也不跨租户误伤：tenant-b 有 active，tenant-a 在另一会话可发起
	seedAssistConversationSession(t, db, "sess-a2", 5)
	if _, err := svc.StartSession(tenantA, assistapp.StartCommand{ConversationSessionID: "sess-a2"}); err != nil {
		t.Fatalf("StartSession() A2 error = %v", err)
	}

	// 无 scope（如本地 dev）不过滤，全部可见
	all, err := svc.ListSessions(context.Background(), "", 0)
	if err != nil {
		t.Fatalf("ListSessions() unscoped error = %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("unscoped should see all 3, got %d", len(all))
	}
}

// TestAssistAnnotationScopeGuard RA-1：标注面经 sessions 子查询守卫，
// 跨 scope 的标注列表为空、删除返回 not found。
func TestAssistAnnotationScopeGuard(t *testing.T) {
	svc, db := newAssistTestService(t)
	seedAssistConversationSession(t, db, "sess-a", 5)

	tenantA := platformauth.ContextWithScope(context.Background(), "tenant-a", "")
	tenantB := platformauth.ContextWithScope(context.Background(), "tenant-b", "")

	sessionA, err := svc.StartSession(tenantA, assistapp.StartCommand{ConversationSessionID: "sess-a"})
	if err != nil {
		t.Fatalf("StartSession() error = %v", err)
	}
	annotation, err := svc.AddAnnotation(tenantA, sessionA.ID, assistapp.AnnotationCommand{
		TimestampMs: 1000, Shape: "rect", Payload: `{"x":0.1}`,
	})
	if err != nil {
		t.Fatalf("AddAnnotation() error = %v", err)
	}

	// 跨租客看不见标注
	if list, err := svc.ListAnnotations(tenantB, sessionA.ID); err != nil || len(list) != 0 {
		t.Fatalf("cross-tenant ListAnnotations = %v (err %v), want empty", list, err)
	}
	// 跨租户删不掉（not found 语义）
	if err := svc.DeleteAnnotation(tenantB, annotation.ID); !errors.Is(err, assistapp.ErrAssistAnnotationNotFound) {
		t.Fatalf("cross-tenant DeleteAnnotation error = %v, want assistapp.ErrAssistAnnotationNotFound", err)
	}
	// 本租户可见可删
	if list, err := svc.ListAnnotations(tenantA, sessionA.ID); err != nil || len(list) != 1 {
		t.Fatalf("same-tenant ListAnnotations = %v (err %v), want 1", list, err)
	}
	if err := svc.DeleteAnnotation(tenantA, annotation.ID); err != nil {
		t.Fatalf("same-tenant DeleteAnnotation() error = %v", err)
	}
}

// TestAssistService_ConsentLifecycle RA-4：同意/拒绝全链落库，
// declined 结束协助且阻断录制回写。
func TestAssistService_ConsentLifecycle(t *testing.T) {
	svc, db := newAssistTestService(t)
	seedAssistConversationSession(t, db, "sess-1", 5)
	seedAssistConversationSession(t, db, "sess-2", 6)

	ctx := context.Background()
	grantedSession, err := svc.StartSession(ctx, assistapp.StartCommand{ConversationSessionID: "sess-1"})
	if err != nil {
		t.Fatalf("StartSession() error = %v", err)
	}
	got, err := svc.RespondConsent(ctx, grantedSession.ID, 5, true)
	if err != nil {
		t.Fatalf("RespondConsent(accept) error = %v", err)
	}
	if got.ConsentStatus != assistapp.ConsentGranted || got.Status != assistapp.StatusActive || got.ConsentAt == nil {
		t.Fatalf("unexpected granted session: %+v", got)
	}
	// granted 后仍可回写录制
	if _, err := svc.AttachRecording(ctx, grantedSession.ID, 5, assistapp.RecordingMeta{Key: "k"}); err != nil {
		t.Fatalf("AttachRecording() after grant error = %v", err)
	}

	declinedSession, err := svc.StartSession(ctx, assistapp.StartCommand{ConversationSessionID: "sess-2"})
	if err != nil {
		t.Fatalf("StartSession() #2 error = %v", err)
	}
	declined, err := svc.RespondConsent(ctx, declinedSession.ID, 6, false)
	if err != nil {
		t.Fatalf("RespondConsent(decline) error = %v", err)
	}
	if declined.ConsentStatus != assistapp.ConsentDeclined || declined.Status != assistapp.StatusEnded || declined.EndedAt == nil {
		t.Fatalf("unexpected declined session: %+v", declined)
	}
	// declined 后录制回写被拒
	if _, err := svc.AttachRecording(ctx, declinedSession.ID, 6, assistapp.RecordingMeta{Key: "k"}); !errors.Is(err, assistapp.ErrAssistConsentDeclined) {
		t.Fatalf("AttachRecording() after decline error = %v, want assistapp.ErrAssistConsentDeclined", err)
	}
	// 相反表态冲突；同表态幂等
	if _, err := svc.RespondConsent(ctx, declinedSession.ID, 6, true); !errors.Is(err, assistapp.ErrAssistConsentDecided) {
		t.Fatalf("opposite answer error = %v, want assistapp.ErrAssistConsentDecided", err)
	}
	if _, err := svc.RespondConsent(ctx, declinedSession.ID, 6, false); err != nil {
		t.Fatalf("idempotent decline error = %v", err)
	}
}
