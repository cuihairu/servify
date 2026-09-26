package application

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"servify/apps/server/internal/modules/translation/domain"
	platformauth "servify/apps/server/internal/platform/auth"
)

// 偏好面错误（delivery 侧映射 HTTP 状态）。未设置偏好不是错误——
// GetSessionLanguage 以空串表达"未设置"。
var (
	ErrTranslationSessionRequired = errors.New("conversation session id is required")
	ErrTranslationPrefConflict    = errors.New("translation language preference belongs to another scope")
	ErrTranslationViewerInvalid   = errors.New("viewer role must be agent or visitor")
)

// viewer 角色常量（domain 承载取值，application 侧再导出供服务面引用）。
const (
	ViewerRoleAgent   = domain.ViewerRoleAgent
	ViewerRoleVisitor = domain.ViewerRoleVisitor
)

// normalizeViewerRole 归一化 viewer 角色；未知角色返回空串由调用方拒。
func normalizeViewerRole(viewer string) string {
	switch strings.ToLower(strings.TrimSpace(viewer)) {
	case domain.ViewerRoleAgent:
		return domain.ViewerRoleAgent
	case domain.ViewerRoleVisitor:
		return domain.ViewerRoleVisitor
	default:
		return ""
	}
}

// PreferenceStore 会话语言偏好的持久化契约（infra 提供 GORM 实现，pg/sqlite
// 双轨）。租户归属由实现按 ctx 自取（与 assist 仓储同口径）；跨 scope 命中
// 与不存在同语义（不回显存在性）。
type PreferenceStore interface {
	UpsertPreference(ctx context.Context, pref *domain.TranslationLanguagePreference) error
	GetPreference(ctx context.Context, sessionID, viewerRole string) (*domain.TranslationLanguagePreference, error)
	DeletePreference(ctx context.Context, sessionID, viewerRole string) error
}

// PreferenceService 会话语言偏好服务：同一会话两个读向各自一条偏好——
// agent 角色是坐席读译文的目标语言（访客 → 坐席方向，hub 在落库后消费），
// visitor 角色是访客读译文的目标语言（坐席 → 访客方向，坐席发送口消费）
// （Phase 1 刀一存储面、刀三角色维度）。
type PreferenceService struct {
	store PreferenceStore
}

// NewPreferenceService 构造偏好服务。
func NewPreferenceService(store PreferenceStore) *PreferenceService {
	return &PreferenceService{store: store}
}

// SetSessionLanguage upsert 指定读向的目标语言（校验与 Translate 同口径：
// BCP-47 常用子集、小写规范化）。租户/工作区取自认证 ctx，不接受请求方
// 自报；viewer 角色由服务端按认证主体推导，同样不接受自报。
func (s *PreferenceService) SetSessionLanguage(ctx context.Context, sessionID, viewer, targetLang string) (string, error) {
	if s == nil || s.store == nil {
		return "", ErrTranslationUnavailable
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return "", ErrTranslationSessionRequired
	}
	role := normalizeViewerRole(viewer)
	if role == "" {
		return "", ErrTranslationViewerInvalid
	}
	lang := normalizeLang(targetLang)
	if lang == "" {
		return "", ErrTranslationTargetRequired
	}
	if !langTagPattern.MatchString(lang) {
		return "", fmt.Errorf("%w: %q", ErrTranslationLangInvalid, targetLang)
	}
	pref := &domain.TranslationLanguagePreference{
		TenantID:              platformauth.TenantIDFromContext(ctx),
		WorkspaceID:           platformauth.WorkspaceIDFromContext(ctx),
		ConversationSessionID: sessionID,
		ViewerRole:            role,
		TargetLang:            lang,
		UpdatedAt:             time.Now(),
	}
	if err := s.store.UpsertPreference(ctx, pref); err != nil {
		return "", err
	}
	return pref.TargetLang, nil
}

// GetSessionLanguage 返回指定读向的当前目标语言；未设置返回空串（不是
// 错误）。偏好表与会话表解耦：先于首条消息设置也合法。
func (s *PreferenceService) GetSessionLanguage(ctx context.Context, sessionID, viewer string) (string, error) {
	if s == nil || s.store == nil {
		return "", ErrTranslationUnavailable
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return "", ErrTranslationSessionRequired
	}
	role := normalizeViewerRole(viewer)
	if role == "" {
		return "", ErrTranslationViewerInvalid
	}
	pref, err := s.store.GetPreference(ctx, sessionID, role)
	if err != nil {
		return "", err
	}
	if pref == nil {
		return "", nil
	}
	return pref.TargetLang, nil
}

// ClearSessionLanguage 清除指定读向的偏好（幂等：无偏好也返回成功）。
func (s *PreferenceService) ClearSessionLanguage(ctx context.Context, sessionID, viewer string) error {
	if s == nil || s.store == nil {
		return ErrTranslationUnavailable
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return ErrTranslationSessionRequired
	}
	role := normalizeViewerRole(viewer)
	if role == "" {
		return ErrTranslationViewerInvalid
	}
	return s.store.DeletePreference(ctx, sessionID, role)
}
