package application

import (
	"context"
	"encoding/json"
	"errors"
	assistdomain "servify/apps/server/internal/modules/assist/domain"
	"strings"
	"time"
)

// 远程协助应用层错误（delivery 侧映射 HTTP 状态）。
var (
	ErrAssistNotFound           = errors.New("remote assist session not found")
	ErrAssistAnnotationNotFound = errors.New("remote assist annotation not found")
	ErrAssistSessionRequired    = errors.New("conversation session id required")
	ErrAssistShapeInvalid       = errors.New("annotation shape must be one of rect|freehand|arrow")
	ErrAssistPayloadInvalid     = errors.New("annotation payload must be a valid JSON object")
	ErrAssistForbidden          = errors.New("remote assist session does not belong to this customer")
	ErrAssistAlreadyEnded       = errors.New("remote assist session already ended")
)

// 可选标注形状（Canvas 覆盖层）。
const (
	ShapeRect     = "rect"
	ShapeFreehand = "freehand"
	ShapeArrow    = "arrow"
)

// 会话状态。
const (
	StatusActive = "active"
	StatusEnded  = "ended"
	StatusFailed = "failed"
)

// StartCommand 发起协助。
type StartCommand struct {
	ConversationSessionID string
	AgentUserID           uint
	TenantID              string
	WorkspaceID           string
}

// EndCommand 结束协助；录制元数据可缺省（访客可能经访客面单独回传）。
type EndCommand struct {
	Outcome             string // ended|failed，空默认 ended
	RecordingKey        string
	RecordingMime       string
	RecordingDurationMs int64
	RecordingSize       int64
}

// AnnotationCommand 新增标注。
type AnnotationCommand struct {
	TimestampMs int64
	Shape       string
	Payload     string
	CreatedBy   uint
}

// RecordingMeta 录制元数据（访客面上传后回写）。
type RecordingMeta struct {
	Key        string
	Mime       string
	DurationMs int64
	Size       int64
}

// Service 远程协助会话/录制/标注的应用层入口。
type Service struct {
	repo Repository
	now  func() time.Time
}

func NewAssistService(repo Repository) *Service {
	return &Service{repo: repo, now: time.Now}
}

// StartSession 发起一次远程协助（状态 active）。
func (s *Service) StartSession(ctx context.Context, cmd StartCommand) (*assistdomain.RemoteAssistSession, error) {
	if strings.TrimSpace(cmd.ConversationSessionID) == "" {
		return nil, ErrAssistSessionRequired
	}
	if _, err := s.repo.GetConversationSessionOwner(ctx, cmd.ConversationSessionID); err != nil {
		return nil, ErrAssistNotFound
	}
	session := &assistdomain.RemoteAssistSession{
		TenantID:              cmd.TenantID,
		WorkspaceID:           cmd.WorkspaceID,
		ConversationSessionID: cmd.ConversationSessionID,
		AgentUserID:           cmd.AgentUserID,
		Status:                StatusActive,
		StartedAt:             s.now(),
	}
	if err := s.repo.CreateSession(ctx, session); err != nil {
		return nil, err
	}
	return session, nil
}

// EndSession 结束协助并（可选）落录制元数据。
func (s *Service) EndSession(ctx context.Context, id uint, cmd EndCommand) (*assistdomain.RemoteAssistSession, error) {
	session, err := s.repo.GetSession(ctx, id)
	if err != nil {
		return nil, ErrAssistNotFound
	}
	if session.Status != StatusActive {
		return nil, ErrAssistAlreadyEnded
	}
	outcome := cmd.Outcome
	if outcome != StatusFailed {
		outcome = StatusEnded
	}
	now := s.now()
	session.Status = outcome
	session.EndedAt = &now
	applyRecording(session, RecordingMeta{
		Key:        cmd.RecordingKey,
		Mime:       cmd.RecordingMime,
		DurationMs: cmd.RecordingDurationMs,
		Size:       cmd.RecordingSize,
	})
	if err := s.repo.SaveSession(ctx, session); err != nil {
		return nil, err
	}
	return session, nil
}

// GetSession 查询单次协助。
func (s *Service) GetSession(ctx context.Context, id uint) (*assistdomain.RemoteAssistSession, error) {
	if id == 0 {
		return nil, ErrAssistNotFound
	}
	return s.repo.GetSession(ctx, id)
}

// ListSessions 按会话（可选）列出协助记录，默认上限 100。
func (s *Service) ListSessions(ctx context.Context, conversationSessionID string, limit int) ([]assistdomain.RemoteAssistSession, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	return s.repo.ListSessions(ctx, conversationSessionID, limit)
}

// AttachRecording 访客面上传录制后回写元数据；校验协助会话归属该访客。
func (s *Service) AttachRecording(ctx context.Context, id uint, customerUserID uint, meta RecordingMeta) (*assistdomain.RemoteAssistSession, error) {
	session, err := s.repo.GetSession(ctx, id)
	if err != nil {
		return nil, ErrAssistNotFound
	}
	ownerID, err := s.repo.GetConversationSessionOwner(ctx, session.ConversationSessionID)
	if err != nil || ownerID != customerUserID {
		return nil, ErrAssistForbidden
	}
	applyRecording(session, meta)
	if err := s.repo.SaveSession(ctx, session); err != nil {
		return nil, err
	}
	return session, nil
}

// AddAnnotation 追加标注（shape 白名单 + payload 必须是 JSON object）。
func (s *Service) AddAnnotation(ctx context.Context, assistSessionID uint, cmd AnnotationCommand) (*assistdomain.RemoteAssistAnnotation, error) {
	if _, err := s.repo.GetSession(ctx, assistSessionID); err != nil {
		return nil, ErrAssistNotFound
	}
	switch cmd.Shape {
	case ShapeRect, ShapeFreehand, ShapeArrow:
	default:
		return nil, ErrAssistShapeInvalid
	}
	trimmed := strings.TrimSpace(cmd.Payload)
	if trimmed == "" || !json.Valid([]byte(trimmed)) || !strings.HasPrefix(trimmed, "{") {
		return nil, ErrAssistPayloadInvalid
	}
	annotation := &assistdomain.RemoteAssistAnnotation{
		AssistSessionID: assistSessionID,
		TimestampMs:     cmd.TimestampMs,
		Shape:           cmd.Shape,
		Payload:         trimmed,
		CreatedBy:       cmd.CreatedBy,
		CreatedAt:       s.now(),
	}
	if err := s.repo.CreateAnnotation(ctx, annotation); err != nil {
		return nil, err
	}
	return annotation, nil
}

// ListAnnotations 按协助会话列出全部标注（按时间戳升序）。
func (s *Service) ListAnnotations(ctx context.Context, assistSessionID uint) ([]assistdomain.RemoteAssistAnnotation, error) {
	return s.repo.ListAnnotations(ctx, assistSessionID)
}

// DeleteAnnotation 删除单条标注。
func (s *Service) DeleteAnnotation(ctx context.Context, id uint) error {
	if id == 0 {
		return ErrAssistNotFound
	}
	return s.repo.DeleteAnnotation(ctx, id)
}

func applyRecording(session *assistdomain.RemoteAssistSession, meta RecordingMeta) {
	if meta.Key != "" {
		session.RecordingKey = meta.Key
	}
	if meta.Mime != "" {
		session.RecordingMime = meta.Mime
	}
	if meta.DurationMs > 0 {
		session.RecordingDurationMs = meta.DurationMs
	}
	if meta.Size > 0 {
		session.RecordingSize = meta.Size
	}
}
