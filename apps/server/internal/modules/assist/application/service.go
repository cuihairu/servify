package application

import (
	"context"
	"encoding/json"
	"errors"
	assistdomain "servify/apps/server/internal/modules/assist/domain"
	platformauth "servify/apps/server/internal/platform/auth"
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
	ErrAssistSessionActive      = errors.New("remote assist session already active for this conversation")
	ErrAssistConsentDeclined    = errors.New("remote assist consent declined by customer")
	ErrAssistConsentDecided     = errors.New("remote assist consent already decided")
	ErrAssistAgentSessionLimit  = errors.New("remote assist active session limit reached for this agent")
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

// 对方同意状态（RemoteAssistSession.ConsentStatus）。
const (
	ConsentPending  = "pending"
	ConsentGranted  = "granted"
	ConsentDeclined = "declined"
)

// StartCommand 发起协助；租户/工作区从 ctx 取（主体 token scope），
// 不再接受客户端自报值（RA-1 隔离收口）。
type StartCommand struct {
	ConversationSessionID string
	AgentUserID           uint
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

// Actor 操作主体（管理面写操作的归属校验依据）：UserID 来自认证
// claims（不由请求体自报），IsAdmin 取 principal_kind=admin。
type Actor struct {
	UserID  uint
	IsAdmin bool
}

// Options 会话治理参数（企业级 R1）：零值 = 保持既有行为（不限时、
// 不限并发）。装配层从 config.remote_assist 接线。
type Options struct {
	// SessionTTL 是 active 会话的最长存活时长；超时视为异常终止
	// （status=failed）而非继续占用单活跃名额。0 = 不限。
	SessionTTL time.Duration
	// MaxActivePerAgent 是单坐席同时持有的 active 协助上限；0 = 不限。
	MaxActivePerAgent int
}

// Service 远程协助会话/录制/标注的应用层入口。
type Service struct {
	repo Repository
	now  func() time.Time
	opts Options
}

// NewAssistService 构造应用服务；Options 可省略（零值 = 治理参数关闭）。
func NewAssistService(repo Repository, opts ...Options) *Service {
	o := Options{}
	if len(opts) > 0 {
		o = opts[0]
	}
	return &Service{repo: repo, now: time.Now, opts: o}
}

// StartSession 发起一次远程协助（状态 active、consent pending）。
// 同一客服会话同时只允许一个 active 协助（RA-2 单活跃约束）；配置了
// SessionTTL 时先懒清扫过期会话再判定，配置了 MaxActivePerAgent 时
// 校验坐席并发上限。
func (s *Service) StartSession(ctx context.Context, cmd StartCommand) (*assistdomain.RemoteAssistSession, error) {
	if strings.TrimSpace(cmd.ConversationSessionID) == "" {
		return nil, ErrAssistSessionRequired
	}
	if _, err := s.repo.GetConversationSessionOwner(ctx, cmd.ConversationSessionID); err != nil {
		return nil, ErrAssistNotFound
	}
	if s.opts.SessionTTL > 0 {
		if _, err := s.repo.ExpireStaleActiveSessions(ctx, s.now().Add(-s.opts.SessionTTL)); err != nil {
			return nil, err
		}
	}
	if s.opts.MaxActivePerAgent > 0 {
		active, err := s.repo.CountActiveSessionsByAgent(ctx, cmd.AgentUserID)
		if err != nil {
			return nil, err
		}
		if active >= int64(s.opts.MaxActivePerAgent) {
			return nil, ErrAssistAgentSessionLimit
		}
	}
	if activeID, err := s.repo.FindActiveSessionIDByConversation(ctx, cmd.ConversationSessionID); err != nil {
		return nil, err
	} else if activeID != 0 {
		return nil, ErrAssistSessionActive
	}
	session := &assistdomain.RemoteAssistSession{
		TenantID:              platformauth.TenantIDFromContext(ctx),
		WorkspaceID:           platformauth.WorkspaceIDFromContext(ctx),
		ConversationSessionID: cmd.ConversationSessionID,
		AgentUserID:           cmd.AgentUserID,
		Status:                StatusActive,
		ConsentStatus:         ConsentPending,
		StartedAt:             s.now(),
	}
	if err := s.repo.CreateSession(ctx, session); err != nil {
		return nil, err
	}
	return session, nil
}

// EndSession 结束协助并（可选）落录制元数据。归属校验：非 admin 只能
// 结束自己发起的协助（admin 在 scope 内可结束任意协助；跨 scope 命中
// 与不存在同返 not found，见仓储层）。
func (s *Service) EndSession(ctx context.Context, id uint, actor Actor, cmd EndCommand) (*assistdomain.RemoteAssistSession, error) {
	session, err := s.repo.GetSession(ctx, id)
	if err != nil {
		return nil, ErrAssistNotFound
	}
	if !actor.IsAdmin && session.AgentUserID != actor.UserID {
		return nil, ErrAssistForbidden
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

// GetSession 查询单次协助。配置了 SessionTTL 时对查到的 active 会话做
// 单行懒过期（超时视为异常终止，如实反映生命周期）。
func (s *Service) GetSession(ctx context.Context, id uint) (*assistdomain.RemoteAssistSession, error) {
	if id == 0 {
		return nil, ErrAssistNotFound
	}
	session, err := s.repo.GetSession(ctx, id)
	if err != nil {
		return nil, err
	}
	if s.opts.SessionTTL > 0 && session.Status == StatusActive && s.expired(session) {
		now := s.now()
		session.Status = StatusFailed
		session.EndedAt = &now
		if err := s.repo.SaveSession(ctx, session); err != nil {
			return nil, err
		}
	}
	return session, nil
}

// ListSessions 按会话（可选）列出协助记录，默认上限 100。
func (s *Service) ListSessions(ctx context.Context, conversationSessionID string, limit int) ([]assistdomain.RemoteAssistSession, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	return s.repo.ListSessions(ctx, conversationSessionID, limit)
}

// AttachRecording 访客面上传录制后回写元数据；校验协助会话归属该访客。
// consent=declined 的协助拒绝回写（对方明确拒绝后不得再落录制证据）。
func (s *Service) AttachRecording(ctx context.Context, id uint, customerUserID uint, meta RecordingMeta) (*assistdomain.RemoteAssistSession, error) {
	session, err := s.repo.GetSession(ctx, id)
	if err != nil {
		return nil, ErrAssistNotFound
	}
	ownerID, err := s.repo.GetConversationSessionOwner(ctx, session.ConversationSessionID)
	if err != nil || ownerID != customerUserID {
		return nil, ErrAssistForbidden
	}
	if session.ConsentStatus == ConsentDeclined {
		return nil, ErrAssistConsentDeclined
	}
	applyRecording(session, meta)
	if err := s.repo.SaveSession(ctx, session); err != nil {
		return nil, err
	}
	return session, nil
}

// RespondConsent 访客对协助邀请表态（RA-4 同意状态机）。
// accept=true → granted（协助继续）；accept=false → declined 并结束协助
// （协助未发生，记录保留供审计）。重复不同表态返回冲突；同一表态幂等。
func (s *Service) RespondConsent(ctx context.Context, id uint, customerUserID uint, accept bool) (*assistdomain.RemoteAssistSession, error) {
	session, err := s.repo.GetSession(ctx, id)
	if err != nil {
		return nil, ErrAssistNotFound
	}
	ownerID, err := s.repo.GetConversationSessionOwner(ctx, session.ConversationSessionID)
	if err != nil || ownerID != customerUserID {
		return nil, ErrAssistForbidden
	}
	next := ConsentGranted
	if !accept {
		next = ConsentDeclined
	}
	if session.ConsentStatus == next {
		return session, nil
	}
	if session.ConsentStatus == ConsentGranted || session.ConsentStatus == ConsentDeclined {
		return nil, ErrAssistConsentDecided
	}
	now := s.now()
	session.ConsentStatus = next
	session.ConsentAt = &now
	if next == ConsentDeclined {
		// 拒绝即终止：协助不再进行，生命周期以 ended 收口（拒绝原因由
		// consent_status 承载，不新造 status 枚举值）。
		session.Status = StatusEnded
		session.EndedAt = &now
	}
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

// DeleteAnnotation 删除单条标注。归属校验：非 admin 只能删除自己
// 创建的标注（仓储层按 created_by 过滤，命中为零与不存在同返 not
// found，不回显存在性）。
func (s *Service) DeleteAnnotation(ctx context.Context, id uint, actor Actor) error {
	if id == 0 {
		return ErrAssistNotFound
	}
	return s.repo.DeleteAnnotation(ctx, id, actor.UserID, !actor.IsAdmin)
}

// expired 判断 active 会话是否已超 TTL。
func (s *Service) expired(session *assistdomain.RemoteAssistSession) bool {
	return s.opts.SessionTTL > 0 && session.StartedAt.Before(s.now().Add(-s.opts.SessionTTL))
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
