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
)

// PreferenceStore 会话语言偏好的持久化契约（infra 提供 GORM 实现，pg/sqlite
// 双轨）。租户归属由实现按 ctx 自取（与 assist 仓储同口径）；跨 scope 命中
// 与不存在同语义（不回显存在性）。
type PreferenceStore interface {
	UpsertPreference(ctx context.Context, pref *domain.TranslationLanguagePreference) error
	GetPreference(ctx context.Context, sessionID string) (*domain.TranslationLanguagePreference, error)
	DeletePreference(ctx context.Context, sessionID string) error
}

// PreferenceService 会话语言偏好服务：坐席为客服会话设置翻译目标语言，
// hub 在消息落库后按该偏好触发自动翻译（Phase 1 刀一：存储与 REST 面；
// hub 集成在刀二接线）。
type PreferenceService struct {
	store PreferenceStore
}

// NewPreferenceService 构造偏好服务。
func NewPreferenceService(store PreferenceStore) *PreferenceService {
	return &PreferenceService{store: store}
}

// SetSessionLanguage upsert 会话目标语言（校验与 Translate 同口径：BCP-47
// 常用子集、小写规范化）。租户/工作区取自认证 ctx，不接受请求方自报。
func (s *PreferenceService) SetSessionLanguage(ctx context.Context, sessionID, targetLang string) (string, error) {
	if s == nil || s.store == nil {
		return "", ErrTranslationUnavailable
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return "", ErrTranslationSessionRequired
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
		TargetLang:            lang,
		UpdatedAt:             time.Now(),
	}
	if err := s.store.UpsertPreference(ctx, pref); err != nil {
		return "", err
	}
	return pref.TargetLang, nil
}

// GetSessionLanguage 返回会话当前目标语言；未设置返回空串（不是错误）。
// 偏好表与会话表解耦：先于首条消息设置也合法。
func (s *PreferenceService) GetSessionLanguage(ctx context.Context, sessionID string) (string, error) {
	if s == nil || s.store == nil {
		return "", ErrTranslationUnavailable
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return "", ErrTranslationSessionRequired
	}
	pref, err := s.store.GetPreference(ctx, sessionID)
	if err != nil {
		return "", err
	}
	if pref == nil {
		return "", nil
	}
	return pref.TargetLang, nil
}

// ClearSessionLanguage 清除会话偏好（幂等：无偏好也返回成功）。
func (s *PreferenceService) ClearSessionLanguage(ctx context.Context, sessionID string) error {
	if s == nil || s.store == nil {
		return ErrTranslationUnavailable
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return ErrTranslationSessionRequired
	}
	return s.store.DeletePreference(ctx, sessionID)
}
