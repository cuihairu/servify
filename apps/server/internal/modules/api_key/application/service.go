package application

// 开放平台 API Key 签发与吊销：管理面 CRUD + 明文密钥一次性返回。

import (
	"context"
	"errors"
	"strings"
	"time"

	platformauth "servify/apps/server/internal/platform/auth"

	"servify/apps/server/internal/models"
)

// hookGenerateAPIKey 供测试注入生成失败。
var hookGenerateAPIKey = platformauth.GenerateAPIKey

// APIKeyCreateRequest 签发请求；Scopes 空为默认只读面。
type APIKeyCreateRequest struct {
	Name        string     `json:"name" binding:"required"`
	WorkspaceID string     `json:"workspace_id"`
	Scopes      string     `json:"scopes"`
	ExpiresAt   *time.Time `json:"expires_at"`
}

var ErrAPIKeyNotFound = errors.New("api key not found")

type Repository interface {
	List(ctx context.Context) ([]models.APIKey, error)
	Create(ctx context.Context, row *models.APIKey) error
	Get(ctx context.Context, id uint) (*models.APIKey, error)
	Revoke(ctx context.Context, id uint, revokedAt time.Time) error
	Delete(ctx context.Context, id uint) error
}

type Service struct {
	repo Repository
}

func NewService(repo Repository) *Service {
	return &Service{repo: repo}
}

func (s *Service) List(ctx context.Context) ([]models.APIKey, error) {
	return s.repo.List(ctx)
}

// Create 签发新密钥；明文仅在本次响应中返回一次。
func (s *Service) Create(ctx context.Context, req *APIKeyCreateRequest, createdBy string) (*models.APIKey, string, error) {
	if req == nil || strings.TrimSpace(req.Name) == "" {
		return nil, "", errors.New("name required")
	}
	if req.ExpiresAt != nil && !req.ExpiresAt.After(time.Now()) {
		return nil, "", errors.New("expires_at must be in the future")
	}
	plaintext, prefix, hash, err := hookGenerateAPIKey()
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
	if err := s.repo.Create(ctx, row); err != nil {
		return nil, "", err
	}
	return row, plaintext, nil
}

// Revoke 吊销密钥（即时生效：解析器每次请求都查库）。
func (s *Service) Revoke(ctx context.Context, id uint) (*models.APIKey, error) {
	row, err := s.repo.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if row.RevokedAt != nil {
		return row, nil
	}
	now := time.Now()
	if err := s.repo.Revoke(ctx, id, now); err != nil {
		return nil, err
	}
	return s.repo.Get(ctx, id)
}

func (s *Service) Delete(ctx context.Context, id uint) error {
	return s.repo.Delete(ctx, id)
}
