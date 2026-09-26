// Package infra 提供翻译模块的 GORM 持久化（pg/sqlite 双轨）。
// 读取/写入面按 ctx 的租户/工作区收紧（与 assist/macro 等模块同口径）：
// scope 为空 = 不过滤（本地 dev/存量链路兼容）；跨 scope 命中与不存在
// 同语义（不回显存在性）。
package infra

import (
	"context"
	"errors"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	translationapp "servify/apps/server/internal/modules/translation/application"
	translationdomain "servify/apps/server/internal/modules/translation/domain"
	platformauth "servify/apps/server/internal/platform/auth"
)

// GormPreferenceRepository 会话翻译语言偏好仓储（(session, viewer_role)
// 每组合至多一行）。
type GormPreferenceRepository struct {
	db *gorm.DB
}

// NewGormPreferenceRepository 构造偏好仓储。
func NewGormPreferenceRepository(db *gorm.DB) *GormPreferenceRepository {
	return &GormPreferenceRepository{db: db}
}

// applyScopeFilter 按请求 scope 收紧偏好查询。
func applyScopeFilter(tx *gorm.DB, ctx context.Context) *gorm.DB {
	if tenantID := platformauth.TenantIDFromContext(ctx); tenantID != "" {
		tx = tx.Where("tenant_id = ?", tenantID)
	}
	if workspaceID := platformauth.WorkspaceIDFromContext(ctx); workspaceID != "" {
		tx = tx.Where("workspace_id = ?", workspaceID)
	}
	return tx
}

// UpsertPreference 按 (会话, viewer 角色) upsert 目标语言。先做 scoped 读
// 命中即原行更新（保 id/created_at、不动他租户行）；未命中走 OnConflict
// DoNothing 的 Create（pg/sqlite 双方言可移植，不依赖驱动的唯一冲突错误
// 翻译），随后 scoped 复查：行仍不可见 = 归属他 scope（跨租户撞唯一索引），
// 返回冲突错误；可见 = 本 scope 并发建行，补一次语言覆盖。
func (r *GormPreferenceRepository) UpsertPreference(ctx context.Context, pref *translationdomain.TranslationLanguagePreference) error {
	existing, err := r.GetPreference(ctx, pref.ConversationSessionID, pref.ViewerRole)
	if err != nil {
		return err
	}
	if existing != nil {
		return r.db.WithContext(ctx).Model(existing).
			Updates(map[string]interface{}{"target_lang": pref.TargetLang}).Error
	}
	if err := r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{
			{Name: "conversation_session_id"},
			{Name: "viewer_role"},
		},
		DoNothing: true,
	}).Create(pref).Error; err != nil {
		return err
	}
	raced, err := r.GetPreference(ctx, pref.ConversationSessionID, pref.ViewerRole)
	if err != nil {
		return err
	}
	if raced == nil {
		return translationapp.ErrTranslationPrefConflict
	}
	if raced.TargetLang != pref.TargetLang {
		return r.db.WithContext(ctx).Model(raced).
			Updates(map[string]interface{}{"target_lang": pref.TargetLang}).Error
	}
	return nil
}

// GetPreference 返回会话指定读向的偏好；无偏好返回 (nil, nil)（不是错误）。
func (r *GormPreferenceRepository) GetPreference(ctx context.Context, sessionID, viewerRole string) (*translationdomain.TranslationLanguagePreference, error) {
	var pref translationdomain.TranslationLanguagePreference
	err := applyScopeFilter(r.db.WithContext(ctx), ctx).
		Where("conversation_session_id = ? AND viewer_role = ?", sessionID, viewerRole).
		First(&pref).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &pref, nil
}

// DeletePreference 删除会话指定读向的偏好（幂等：无行也成功，不回显存在性）。
func (r *GormPreferenceRepository) DeletePreference(ctx context.Context, sessionID, viewerRole string) error {
	return applyScopeFilter(r.db.WithContext(ctx), ctx).
		Where("conversation_session_id = ? AND viewer_role = ?", sessionID, viewerRole).
		Delete(&translationdomain.TranslationLanguagePreference{}).Error
}
