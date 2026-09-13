package application

import (
	"context"
	"errors"
	"testing"

	"servify/apps/server/internal/models"
)

// mockRepo 应用层内联仓储桩：内存实现 + 按方法注入错误。
type mockRepo struct {
	sessions    map[uint]*models.RemoteAssistSession
	annotations map[uint]*models.RemoteAssistAnnotation
	owners      map[string]uint
	nextID      uint

	// 错误注入
	getOwnerErr     error
	createErr       error
	getSessionErr   error
	listSessionsErr error
	saveErr         error
	listAnnotErr    error
	createAnnotErr  error
	deleteAnnotErr  error

	// 参数捕获
	lastListLimit int
}

func newMockRepo() *mockRepo {
	return &mockRepo{
		sessions:    map[uint]*models.RemoteAssistSession{},
		annotations: map[uint]*models.RemoteAssistAnnotation{},
		owners:      map[string]uint{},
	}
}

func (m *mockRepo) CreateSession(_ context.Context, session *models.RemoteAssistSession) error {
	if m.createErr != nil {
		return m.createErr
	}
	m.nextID++
	session.ID = m.nextID
	cp := *session
	m.sessions[session.ID] = &cp
	return nil
}

func (m *mockRepo) GetSession(_ context.Context, id uint) (*models.RemoteAssistSession, error) {
	if m.getSessionErr != nil {
		return nil, m.getSessionErr
	}
	session, ok := m.sessions[id]
	if !ok {
		return nil, errors.New("record not found")
	}
	cp := *session
	return &cp, nil
}

func (m *mockRepo) ListSessions(_ context.Context, conversationSessionID string, limit int) ([]models.RemoteAssistSession, error) {
	if m.listSessionsErr != nil {
		return nil, m.listSessionsErr
	}
	m.lastListLimit = limit
	var out []models.RemoteAssistSession
	for _, s := range m.sessions {
		if conversationSessionID != "" && s.ConversationSessionID != conversationSessionID {
			continue
		}
		out = append(out, *s)
	}
	return out, nil
}

func (m *mockRepo) SaveSession(_ context.Context, session *models.RemoteAssistSession) error {
	if m.saveErr != nil {
		return m.saveErr
	}
	cp := *session
	m.sessions[session.ID] = &cp
	return nil
}

func (m *mockRepo) GetConversationSessionOwner(_ context.Context, sessionID string) (uint, error) {
	if m.getOwnerErr != nil {
		return 0, m.getOwnerErr
	}
	owner, ok := m.owners[sessionID]
	if !ok {
		return 0, errors.New("conversation session not found")
	}
	return owner, nil
}

func (m *mockRepo) ListAnnotations(_ context.Context, assistSessionID uint) ([]models.RemoteAssistAnnotation, error) {
	if m.listAnnotErr != nil {
		return nil, m.listAnnotErr
	}
	var out []models.RemoteAssistAnnotation
	for _, a := range m.annotations {
		if a.AssistSessionID == assistSessionID {
			out = append(out, *a)
		}
	}
	return out, nil
}

func (m *mockRepo) CreateAnnotation(_ context.Context, annotation *models.RemoteAssistAnnotation) error {
	if m.createAnnotErr != nil {
		return m.createAnnotErr
	}
	m.nextID++
	annotation.ID = m.nextID
	cp := *annotation
	m.annotations[annotation.ID] = &cp
	return nil
}

func (m *mockRepo) GetAnnotation(_ context.Context, id uint) (*models.RemoteAssistAnnotation, error) {
	annotation, ok := m.annotations[id]
	if !ok {
		return nil, errors.New("record not found")
	}
	cp := *annotation
	return &cp, nil
}

func (m *mockRepo) DeleteAnnotation(_ context.Context, id uint) error {
	if m.deleteAnnotErr != nil {
		return m.deleteAnnotErr
	}
	if _, ok := m.annotations[id]; !ok {
		return errors.New("record not found")
	}
	delete(m.annotations, id)
	return nil
}

func seedMockSession(t *testing.T, repo *mockRepo, id uint, status string) *models.RemoteAssistSession {
	t.Helper()
	repo.owners["sess-1"] = 5
	session := &models.RemoteAssistSession{
		ID: id, ConversationSessionID: "sess-1", AgentUserID: 9, Status: status,
	}
	repo.sessions[id] = session
	return session
}

func TestAssistAppStartSession(t *testing.T) {
	ctx := context.Background()

	t.Run("conversation session required", func(t *testing.T) {
		svc := NewAssistService(newMockRepo())
		if _, err := svc.StartSession(ctx, StartCommand{ConversationSessionID: "   "}); !errors.Is(err, ErrAssistSessionRequired) {
			t.Fatalf("want ErrAssistSessionRequired, got %v", err)
		}
	})

	t.Run("owner lookup error maps to not found", func(t *testing.T) {
		repo := newMockRepo()
		repo.getOwnerErr = errors.New("db down")
		svc := NewAssistService(repo)
		if _, err := svc.StartSession(ctx, StartCommand{ConversationSessionID: "sess-1"}); !errors.Is(err, ErrAssistNotFound) {
			t.Fatalf("want ErrAssistNotFound, got %v", err)
		}
	})

	t.Run("unknown conversation session maps to not found", func(t *testing.T) {
		svc := NewAssistService(newMockRepo())
		if _, err := svc.StartSession(ctx, StartCommand{ConversationSessionID: "missing"}); !errors.Is(err, ErrAssistNotFound) {
			t.Fatalf("want ErrAssistNotFound, got %v", err)
		}
	})

	t.Run("create error propagates", func(t *testing.T) {
		repo := newMockRepo()
		repo.owners["sess-1"] = 5
		repo.createErr = errors.New("insert boom")
		svc := NewAssistService(repo)
		_, err := svc.StartSession(ctx, StartCommand{ConversationSessionID: "sess-1"})
		if err == nil || err.Error() != "insert boom" {
			t.Fatalf("want raw create error, got %v", err)
		}
	})

	t.Run("success creates active session", func(t *testing.T) {
		repo := newMockRepo()
		repo.owners["sess-1"] = 5
		svc := NewAssistService(repo)
		session, err := svc.StartSession(ctx, StartCommand{
			ConversationSessionID: "sess-1", AgentUserID: 9, TenantID: "t1", WorkspaceID: "w1",
		})
		if err != nil {
			t.Fatalf("StartSession() error = %v", err)
		}
		if session.ID == 0 || session.Status != StatusActive || session.AgentUserID != 9 ||
			session.TenantID != "t1" || session.WorkspaceID != "w1" || session.StartedAt.IsZero() {
			t.Fatalf("unexpected session: %+v", session)
		}
	})
}

func TestAssistAppEndSession(t *testing.T) {
	ctx := context.Background()

	t.Run("unknown session maps to not found", func(t *testing.T) {
		svc := NewAssistService(newMockRepo())
		if _, err := svc.EndSession(ctx, 42, EndCommand{}); !errors.Is(err, ErrAssistNotFound) {
			t.Fatalf("want ErrAssistNotFound, got %v", err)
		}
	})

	t.Run("already ended", func(t *testing.T) {
		repo := newMockRepo()
		seedMockSession(t, repo, 1, StatusEnded)
		svc := NewAssistService(repo)
		if _, err := svc.EndSession(ctx, 1, EndCommand{}); !errors.Is(err, ErrAssistAlreadyEnded) {
			t.Fatalf("want ErrAssistAlreadyEnded, got %v", err)
		}
	})

	t.Run("save error propagates", func(t *testing.T) {
		repo := newMockRepo()
		seedMockSession(t, repo, 1, StatusActive)
		repo.saveErr = errors.New("update boom")
		svc := NewAssistService(repo)
		_, err := svc.EndSession(ctx, 1, EndCommand{})
		if err == nil || err.Error() != "update boom" {
			t.Fatalf("want raw save error, got %v", err)
		}
	})

	t.Run("failed outcome and partial recording meta", func(t *testing.T) {
		repo := newMockRepo()
		seeded := seedMockSession(t, repo, 1, StatusActive)
		seeded.RecordingKey = "old/key.webm"
		seeded.RecordingDurationMs = 111
		svc := NewAssistService(repo)
		got, err := svc.EndSession(ctx, 1, EndCommand{Outcome: StatusFailed, RecordingKey: "new/key.webm"})
		if err != nil {
			t.Fatalf("EndSession() error = %v", err)
		}
		if got.Status != StatusFailed || got.EndedAt == nil {
			t.Fatalf("unexpected ended session: %+v", got)
		}
		if got.RecordingKey != "new/key.webm" {
			t.Fatalf("recording key not applied: %+v", got)
		}
		if got.RecordingDurationMs != 111 {
			t.Fatalf("zero meta fields must not overwrite existing values: %+v", got)
		}
	})

	t.Run("empty outcome defaults to ended", func(t *testing.T) {
		repo := newMockRepo()
		seedMockSession(t, repo, 1, StatusActive)
		svc := NewAssistService(repo)
		got, err := svc.EndSession(ctx, 1, EndCommand{})
		if err != nil {
			t.Fatalf("EndSession() error = %v", err)
		}
		if got.Status != StatusEnded {
			t.Fatalf("status = %s, want ended", got.Status)
		}
	})
}

func TestAssistAppGetSession(t *testing.T) {
	ctx := context.Background()
	repo := newMockRepo()
	seedMockSession(t, repo, 1, StatusActive)
	svc := NewAssistService(repo)

	if _, err := svc.GetSession(ctx, 0); !errors.Is(err, ErrAssistNotFound) {
		t.Fatalf("id=0 want ErrAssistNotFound, got %v", err)
	}
	repo.getSessionErr = errors.New("db down")
	if _, err := svc.GetSession(ctx, 1); err == nil || err.Error() != "db down" {
		t.Fatalf("want raw repo error, got %v", err)
	}
	repo.getSessionErr = nil
	got, err := svc.GetSession(ctx, 1)
	if err != nil || got.ID != 1 {
		t.Fatalf("GetSession() = %+v, %v", got, err)
	}
}

func TestAssistAppListSessions(t *testing.T) {
	ctx := context.Background()
	repo := newMockRepo()
	seedMockSession(t, repo, 1, StatusActive)
	svc := NewAssistService(repo)

	// 非法 limit 归一化为 100
	if _, err := svc.ListSessions(ctx, "", 0); err != nil {
		t.Fatalf("ListSessions() error = %v", err)
	}
	if repo.lastListLimit != 100 {
		t.Fatalf("limit 0 must normalize to 100, got %d", repo.lastListLimit)
	}
	if _, err := svc.ListSessions(ctx, "", 500); err != nil {
		t.Fatalf("ListSessions() error = %v", err)
	}
	if repo.lastListLimit != 100 {
		t.Fatalf("limit 500 must normalize to 100, got %d", repo.lastListLimit)
	}
	if _, err := svc.ListSessions(ctx, "sess-1", 5); err != nil {
		t.Fatalf("ListSessions() error = %v", err)
	}
	if repo.lastListLimit != 5 {
		t.Fatalf("limit 5 must be kept, got %d", repo.lastListLimit)
	}
	repo.listSessionsErr = errors.New("list boom")
	if _, err := svc.ListSessions(ctx, "", 5); err == nil || err.Error() != "list boom" {
		t.Fatalf("want raw list error, got %v", err)
	}
}

func TestAssistAppAttachRecording(t *testing.T) {
	ctx := context.Background()

	t.Run("unknown session maps to not found", func(t *testing.T) {
		svc := NewAssistService(newMockRepo())
		if _, err := svc.AttachRecording(ctx, 42, 5, RecordingMeta{Key: "k"}); !errors.Is(err, ErrAssistNotFound) {
			t.Fatalf("want ErrAssistNotFound, got %v", err)
		}
	})

	t.Run("owner lookup error maps to forbidden", func(t *testing.T) {
		repo := newMockRepo()
		seedMockSession(t, repo, 1, StatusActive)
		repo.getOwnerErr = errors.New("db down")
		svc := NewAssistService(repo)
		if _, err := svc.AttachRecording(ctx, 1, 5, RecordingMeta{Key: "k"}); !errors.Is(err, ErrAssistForbidden) {
			t.Fatalf("want ErrAssistForbidden, got %v", err)
		}
	})

	t.Run("wrong owner maps to forbidden", func(t *testing.T) {
		repo := newMockRepo()
		seedMockSession(t, repo, 1, StatusActive) // owner = 5
		svc := NewAssistService(repo)
		if _, err := svc.AttachRecording(ctx, 1, 6, RecordingMeta{Key: "k"}); !errors.Is(err, ErrAssistForbidden) {
			t.Fatalf("want ErrAssistForbidden, got %v", err)
		}
	})

	t.Run("save error propagates", func(t *testing.T) {
		repo := newMockRepo()
		seedMockSession(t, repo, 1, StatusActive)
		repo.saveErr = errors.New("update boom")
		svc := NewAssistService(repo)
		_, err := svc.AttachRecording(ctx, 1, 5, RecordingMeta{Key: "k"})
		if err == nil || err.Error() != "update boom" {
			t.Fatalf("want raw save error, got %v", err)
		}
	})

	t.Run("success attaches meta and keeps non-active status", func(t *testing.T) {
		repo := newMockRepo()
		seedMockSession(t, repo, 1, StatusEnded)
		svc := NewAssistService(repo)
		got, err := svc.AttachRecording(ctx, 1, 5, RecordingMeta{
			Key: "uploads/rec.webm", Mime: "video/webm", DurationMs: 1500, Size: 4096,
		})
		if err != nil {
			t.Fatalf("AttachRecording() error = %v", err)
		}
		if got.RecordingKey != "uploads/rec.webm" || got.RecordingMime != "video/webm" ||
			got.RecordingDurationMs != 1500 || got.RecordingSize != 4096 {
			t.Fatalf("meta not applied: %+v", got)
		}
	})
}

func TestAssistAppAddAnnotation(t *testing.T) {
	ctx := context.Background()
	repo := newMockRepo()
	seedMockSession(t, repo, 1, StatusActive)
	svc := NewAssistService(repo)

	cases := []struct {
		name    string
		session uint
		cmd     AnnotationCommand
		wantErr error
	}{
		{name: "unknown assist session", session: 99, cmd: AnnotationCommand{Shape: ShapeRect, Payload: "{}"}, wantErr: ErrAssistNotFound},
		{name: "invalid shape", session: 1, cmd: AnnotationCommand{Shape: "circle", Payload: "{}"}, wantErr: ErrAssistShapeInvalid},
		{name: "empty shape", session: 1, cmd: AnnotationCommand{Payload: "{}"}, wantErr: ErrAssistShapeInvalid},
		{name: "empty payload", session: 1, cmd: AnnotationCommand{Shape: ShapeRect}, wantErr: ErrAssistPayloadInvalid},
		{name: "non json payload", session: 1, cmd: AnnotationCommand{Shape: ShapeRect, Payload: "   not-json   "}, wantErr: ErrAssistPayloadInvalid},
		{name: "array payload", session: 1, cmd: AnnotationCommand{Shape: ShapeRect, Payload: "[1,2]"}, wantErr: ErrAssistPayloadInvalid},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := svc.AddAnnotation(ctx, tc.session, tc.cmd); !errors.Is(err, tc.wantErr) {
				t.Fatalf("want %v, got %v", tc.wantErr, err)
			}
		})
	}

	t.Run("create error propagates", func(t *testing.T) {
		repo := newMockRepo()
		seedMockSession(t, repo, 1, StatusActive)
		repo.createAnnotErr = errors.New("insert boom")
		svc := NewAssistService(repo)
		_, err := svc.AddAnnotation(ctx, 1, AnnotationCommand{Shape: ShapeArrow, Payload: `{"x":1}`})
		if err == nil || err.Error() != "insert boom" {
			t.Fatalf("want raw create error, got %v", err)
		}
	})

	t.Run("success trims payload", func(t *testing.T) {
		svc2 := NewAssistService(newMockRepoWithSession(t))
		got, err := svc2.AddAnnotation(ctx, 1, AnnotationCommand{
			TimestampMs: 12000, Shape: ShapeFreehand, Payload: `  {"points":[1]}  `, CreatedBy: 9,
		})
		if err != nil {
			t.Fatalf("AddAnnotation() error = %v", err)
		}
		if got.Payload != `{"points":[1]}` || got.Shape != ShapeFreehand || got.CreatedBy != 9 || got.CreatedAt.IsZero() {
			t.Fatalf("unexpected annotation: %+v", got)
		}
	})
}

func newMockRepoWithSession(t *testing.T) *mockRepo {
	t.Helper()
	repo := newMockRepo()
	seedMockSession(t, repo, 1, StatusActive)
	return repo
}

func TestAssistAppListAndDeleteAnnotation(t *testing.T) {
	ctx := context.Background()
	repo := newMockRepo()
	seedMockSession(t, repo, 1, StatusActive)
	repo.annotations[7] = &models.RemoteAssistAnnotation{ID: 7, AssistSessionID: 1, Shape: ShapeRect, Payload: "{}"}
	svc := NewAssistService(repo)

	items, err := svc.ListAnnotations(ctx, 1)
	if err != nil || len(items) != 1 || items[0].ID != 7 {
		t.Fatalf("ListAnnotations() = %+v, %v", items, err)
	}
	repo.listAnnotErr = errors.New("list boom")
	if _, err := svc.ListAnnotations(ctx, 1); err == nil || err.Error() != "list boom" {
		t.Fatalf("want raw list error, got %v", err)
	}

	if err := svc.DeleteAnnotation(ctx, 0); !errors.Is(err, ErrAssistNotFound) {
		t.Fatalf("id=0 want ErrAssistNotFound, got %v", err)
	}
	repo.deleteAnnotErr = errors.New("delete boom")
	if err := svc.DeleteAnnotation(ctx, 7); err == nil || err.Error() != "delete boom" {
		t.Fatalf("want raw delete error, got %v", err)
	}
	repo.deleteAnnotErr = nil
	if err := svc.DeleteAnnotation(ctx, 7); err != nil {
		t.Fatalf("DeleteAnnotation() error = %v", err)
	}
}
