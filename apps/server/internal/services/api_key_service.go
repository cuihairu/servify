package services

import (
	"context"
	"errors"
	"strings"
	"time"

	"servify/apps/server/internal/models"
	platformauth "servify/apps/server/internal/platform/auth"

	"gorm.io/gorm"
)

// APIKeyService 管理开放平台 API Key 的签发与吊销。
//
//nolint:revive
type APIKeyService struct {
	db *gorm.DB
}

func NewAPIKeyService(db *gorm.DB) *APIKeyService { return &APIKeyService{db: db} }

// APIKeyCreateRequest 签发请求；Scopes 空为默认只读面。
type APIKeyCreateRequest struct {
	Name        string     `json:"name" binding:"required"`
	WorkspaceID string     `json:"workspace_id"`
	Scopes      string     `json:"scopes"`
	ExpiresAt   *time.Time `json:"expires_at"`
}

var ErrAPIKeyNotFound = errors.New("api key not found")

func (s *APIKeyService) List(ctx context.Context) ([]models.APIKey, error) {
	var keys []models.APIKey
	if err := applyScopeFilter(s.db.WithContext(ctx), ctx).Order("created_at DESC, id DESC").Find(&keys).Error; err != nil {
		return nil, err
	}
	return keys, nil
}

// Create 签发新密钥；明文仅在本次响应中返回一次。
func (s *APIKeyService) Create(ctx context.Context, req *APIKeyCreateRequest, createdBy string) (*models.APIKey, string, error) {
	if req == nil || strings.TrimSpace(req.Name) == "" {
		return nil, "", errors.New("name required")
	}
	if req.ExpiresAt != nil && !req.ExpiresAt.After(time.Now()) {
		return nil, "", errors.New("expires_at must be in the future")
	}
	plaintext, prefix, hash, err := platformauth.GenerateAPIKey()
	if err != nil {
		return nil, "", err
	}
	row := &models.APIKey{
		Name:        strings.TrimSpace(req.Name),
		Prefix:      prefix,
		KeyHash:     hash,
		WorkspaceID: req.WorkspaceID,
		Scopes:      strings.TrimSpace(req.Scopes),
		CreatedBy:   createdBy,
		ExpiresAt:   req.ExpiresAt,
	}
	if err := s.db.WithContext(ctx).Create(row).Error; err != nil {
		return nil, "", err
	}
	return row, plaintext, nil
}

// Revoke 吊销密钥（即时生效：解析器每次请求都查库）。
func (s *APIKeyService) Revoke(ctx context.Context, id uint) (*models.APIKey, error) {
	row, err := s.get(ctx, id)
	if err != nil {
		return nil, err
	}
	if row.RevokedAt != nil {
		return row, nil
	}
	now := time.Now()
	if err := s.db.WithContext(ctx).Model(&models.APIKey{}).Where("id = ?", id).
		Updates(map[string]interface{}{"revoked_at": now, "updated_at": now}).Error; err != nil {
		return nil, err
	}
	return s.get(ctx, id)
}

func (s *APIKeyService) Delete(ctx context.Context, id uint) error {
	res := s.db.WithContext(ctx).Where("id = ?", id).Delete(&models.APIKey{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrAPIKeyNotFound
	}
	return nil
}

func (s *APIKeyService) get(ctx context.Context, id uint) (*models.APIKey, error) {
	var row models.APIKey
	if err := s.db.WithContext(ctx).First(&row, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrAPIKeyNotFound
		}
		return nil, err
	}
	return &row, nil
}
