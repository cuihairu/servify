package delivery

import (
	"context"
	"errors"
	assistdomain "servify/apps/server/internal/modules/assist/domain"
	"testing"

	assistapp "servify/apps/server/internal/modules/assist/application"
)

// adapterRepo 内联仓储桩：内存实现 assistapp.Repository，供适配器透传验证。
type adapterRepo struct {
	sessions    map[uint]*assistdomain.RemoteAssistSession
	annotations map[uint]*assistdomain.RemoteAssistAnnotation
	owners      map[string]uint
	nextID      uint
}

func newAdapterRepo() *adapterRepo {
	return &adapterRepo{
		sessions:    map[uint]*assistdomain.RemoteAssistSession{},
		annotations: map[uint]*assistdomain.RemoteAssistAnnotation{},
		owners:      map[string]uint{"sess-1": 5},
	}
}

func (m *adapterRepo) CreateSession(_ context.Context, session *assistdomain.RemoteAssistSession) error {
	m.nextID++
	session.ID = m.nextID
	cp := *session
	m.sessions[session.ID] = &cp
	return nil
}

func (m *adapterRepo) GetSession(_ context.Context, id uint) (*assistdomain.RemoteAssistSession, error) {
	session, ok := m.sessions[id]
	if !ok {
		return nil, errors.New("record not found")
	}
	cp := *session
	return &cp, nil
}

func (m *adapterRepo) ListSessions(_ context.Context, conversationSessionID string, limit int) ([]assistdomain.RemoteAssistSession, error) {
	var out []assistdomain.RemoteAssistSession
	for _, s := range m.sessions {
		if conversationSessionID != "" && s.ConversationSessionID != conversationSessionID {
			continue
		}
		out = append(out, *s)
	}
	return out, nil
}

func (m *adapterRepo) SaveSession(_ context.Context, session *assistdomain.RemoteAssistSession) error {
	cp := *session
	m.sessions[session.ID] = &cp
	return nil
}

func (m *adapterRepo) GetConversationSessionOwner(_ context.Context, sessionID string) (uint, error) {
	owner, ok := m.owners[sessionID]
	if !ok {
		return 0, errors.New("conversation session not found")
	}
	return owner, nil
}

func (m *adapterRepo) ListAnnotations(_ context.Context, assistSessionID uint) ([]assistdomain.RemoteAssistAnnotation, error) {
	var out []assistdomain.RemoteAssistAnnotation
	for _, a := range m.annotations {
		if a.AssistSessionID == assistSessionID {
			out = append(out, *a)
		}
	}
	return out, nil
}

func (m *adapterRepo) CreateAnnotation(_ context.Context, annotation *assistdomain.RemoteAssistAnnotation) error {
	m.nextID++
	annotation.ID = m.nextID
	cp := *annotation
	m.annotations[annotation.ID] = &cp
	return nil
}

func (m *adapterRepo) DeleteAnnotation(_ context.Context, id uint) error {
	if _, ok := m.annotations[id]; !ok {
		return errors.New("record not found")
	}
	delete(m.annotations, id)
	return nil
}

// TestHandlerServiceAdapterDelegates 覆盖全部 9 个适配方法的成功与错误透传。
func TestHandlerServiceAdapterDelegates(t *testing.T) {
	ctx := context.Background()
	repo := newAdapterRepo()
	adapter := NewHandlerService(assistapp.NewAssistService(repo))

	// StartSession
	started, err := adapter.StartSession(ctx, StartCommand{ConversationSessionID: "sess-1", AgentUserID: 9})
	if err != nil || started.ID == 0 || started.Status != assistapp.StatusActive {
		t.Fatalf("StartSession() = %+v, %v", started, err)
	}
	if _, err := adapter.StartSession(ctx, StartCommand{ConversationSessionID: ""}); !errors.Is(err, ErrAssistSessionRequired) {
		t.Fatalf("StartSession() empty error = %v, want ErrAssistSessionRequired", err)
	}

	// EndSession
	ended, err := adapter.EndSession(ctx, started.ID, EndCommand{RecordingKey: "uploads/rec.webm"})
	if err != nil || ended.Status != assistapp.StatusEnded {
		t.Fatalf("EndSession() = %+v, %v", ended, err)
	}
	if _, err := adapter.EndSession(ctx, started.ID, EndCommand{}); !errors.Is(err, ErrAssistAlreadyEnded) {
		t.Fatalf("EndSession() again error = %v, want ErrAssistAlreadyEnded", err)
	}

	// GetSession
	got, err := adapter.GetSession(ctx, started.ID)
	if err != nil || got.ID != started.ID {
		t.Fatalf("GetSession() = %+v, %v", got, err)
	}
	if _, err := adapter.GetSession(ctx, 0); !errors.Is(err, ErrAssistNotFound) {
		t.Fatalf("GetSession(0) error = %v, want ErrAssistNotFound", err)
	}

	// ListSessions
	sessions, err := adapter.ListSessions(ctx, "sess-1", 10)
	if err != nil || len(sessions) != 1 {
		t.Fatalf("ListSessions() = %+v, %v", sessions, err)
	}

	// AttachRecording
	attached, err := adapter.AttachRecording(ctx, started.ID, 5, RecordingMeta{Key: "uploads/other.webm"})
	if err != nil || attached.RecordingKey != "uploads/other.webm" {
		t.Fatalf("AttachRecording() = %+v, %v", attached, err)
	}
	if _, err := adapter.AttachRecording(ctx, started.ID, 6, RecordingMeta{Key: "k"}); !errors.Is(err, ErrAssistForbidden) {
		t.Fatalf("AttachRecording() wrong owner error = %v, want ErrAssistForbidden", err)
	}

	// AddAnnotation
	annotation, err := adapter.AddAnnotation(ctx, started.ID, AnnotationCommand{
		TimestampMs: 1000, Shape: assistapp.ShapeRect, Payload: `{"x":1}`, CreatedBy: 9,
	})
	if err != nil || annotation.ID == 0 || annotation.Shape != assistapp.ShapeRect {
		t.Fatalf("AddAnnotation() = %+v, %v", annotation, err)
	}
	if _, err := adapter.AddAnnotation(ctx, started.ID, AnnotationCommand{Shape: "circle", Payload: "{}"}); !errors.Is(err, ErrAssistShapeInvalid) {
		t.Fatalf("AddAnnotation() bad shape error = %v, want ErrAssistShapeInvalid", err)
	}

	// ListAnnotations
	annotations, err := adapter.ListAnnotations(ctx, started.ID)
	if err != nil || len(annotations) != 1 {
		t.Fatalf("ListAnnotations() = %+v, %v", annotations, err)
	}

	// DeleteAnnotation
	if err := adapter.DeleteAnnotation(ctx, annotation.ID); err != nil {
		t.Fatalf("DeleteAnnotation() error = %v", err)
	}
	if err := adapter.DeleteAnnotation(ctx, 0); !errors.Is(err, ErrAssistNotFound) {
		t.Fatalf("DeleteAnnotation(0) error = %v, want ErrAssistNotFound", err)
	}
}
