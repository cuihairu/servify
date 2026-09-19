package application

// app_integration 模块应用层：应用市场集成管理（自 services/app_integration_service.go 迁入）。

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"servify/apps/server/internal/models"
	platformauth "servify/apps/server/internal/platform/auth"
)

// ErrIntegrationNotFound 集成不存在（消息保持原字符串）。
var ErrIntegrationNotFound = errors.New("integration not found")

// AppIntegration 定义返回给 API 的结构
type AppIntegration struct {
	ID             uint                   `json:"id"`
	Name           string                 `json:"name"`
	Slug           string                 `json:"slug"`
	Vendor         string                 `json:"vendor"`
	Category       string                 `json:"category"`
	Summary        string                 `json:"summary"`
	IconURL        string                 `json:"icon_url"`
	Capabilities   []string               `json:"capabilities"`
	ConfigSchema   map[string]interface{} `json:"config_schema,omitempty"`
	IFrameURL      string                 `json:"iframe_url"`
	Enabled        bool                   `json:"enabled"`
	LastSyncStatus string                 `json:"last_sync_status"`
	CreatedAt      time.Time              `json:"created_at"`
	UpdatedAt      time.Time              `json:"updated_at"`
}

// AppIntegrationListRequest 查询条件
type AppIntegrationListRequest struct {
	Page     int      `form:"page,default=1"`
	PageSize int      `form:"page_size,default=20"`
	Category string   `form:"category"`
	Search   string   `form:"search"`
	Status   []string `form:"status"`
}

// AppIntegrationCreateRequest 创建请求
type AppIntegrationCreateRequest struct {
	Name         string                 `json:"name" binding:"required"`
	Slug         string                 `json:"slug"`
	Vendor       string                 `json:"vendor"`
	Category     string                 `json:"category"`
	Summary      string                 `json:"summary"`
	IconURL      string                 `json:"icon_url"`
	Capabilities []string               `json:"capabilities"`
	ConfigSchema map[string]interface{} `json:"config_schema"`
	IFrameURL    string                 `json:"iframe_url" binding:"required"`
	Enabled      *bool                  `json:"enabled"`
}

// AppIntegrationUpdateRequest 更新请求
type AppIntegrationUpdateRequest struct {
	Name         *string                `json:"name"`
	Vendor       *string                `json:"vendor"`
	Category     *string                `json:"category"`
	Summary      *string                `json:"summary"`
	IconURL      *string                `json:"icon_url"`
	Capabilities []string               `json:"capabilities"`
	ConfigSchema map[string]interface{} `json:"config_schema"`
	IFrameURL    *string                `json:"iframe_url"`
	Enabled      *bool                  `json:"enabled"`
}

// Repository 集成持久化契约。
type Repository interface {
	CountBySlug(ctx context.Context, slug string) (int64, error)
	CountIntegrations(ctx context.Context, req *AppIntegrationListRequest) (int64, error)
	ListIntegrations(ctx context.Context, req *AppIntegrationListRequest, offset, limit int) ([]models.AppIntegration, error)
	GetIntegration(ctx context.Context, id uint) (*models.AppIntegration, error)
	CreateIntegration(ctx context.Context, model *models.AppIntegration) error
	SaveIntegration(ctx context.Context, model *models.AppIntegration) error
	DeleteIntegration(ctx context.Context, id uint) error
}

// AppIntegrationService 管理应用市场集成
type Service struct {
	repo Repository
}

// NewService 初始化服务
func NewService(repo Repository) *Service {
	return &Service{repo: repo}
}

// List 返回集成列表
func (s *Service) List(ctx context.Context, req *AppIntegrationListRequest) ([]*AppIntegration, int64, error) {
	if req.Page < 1 {
		req.Page = 1
	}
	if req.PageSize < 1 || req.PageSize > 100 {
		req.PageSize = 20
	}

	total, err := s.repo.CountIntegrations(ctx, req)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to count integrations: %w", err)
	}

	offset := (req.Page - 1) * req.PageSize
	list, err := s.repo.ListIntegrations(ctx, req, offset, req.PageSize)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to list integrations: %w", err)
	}

	return mapIntegrations(list), total, nil
}

// Create 新增集成
func (s *Service) Create(ctx context.Context, req *AppIntegrationCreateRequest) (*AppIntegration, error) {
	if req == nil {
		return nil, fmt.Errorf("request required")
	}
	slug := normalizeSlug(req.Slug)
	if slug == "" {
		slug = normalizeSlug(req.Name)
	}
	if slug == "" {
		return nil, fmt.Errorf("slug required")
	}

	exists, err := s.repo.CountBySlug(ctx, slug)
	if err != nil {
		return nil, fmt.Errorf("failed to check slug: %w", err)
	}
	if exists > 0 {
		return nil, fmt.Errorf("integration slug already exists")
	}

	model := &models.AppIntegration{
		TenantID:       platformauth.TenantIDFromContext(ctx),
		WorkspaceID:    platformauth.WorkspaceIDFromContext(ctx),
		Name:           req.Name,
		Slug:           slug,
		Vendor:         req.Vendor,
		Category:       req.Category,
		Summary:        req.Summary,
		IconURL:        req.IconURL,
		IFrameURL:      req.IFrameURL,
		Enabled:        req.Enabled == nil || *req.Enabled,
		Capabilities:   encodeJSON(req.Capabilities),
		ConfigSchema:   encodeJSON(req.ConfigSchema),
		LastSyncStatus: "never",
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}

	if err := s.repo.CreateIntegration(ctx, model); err != nil {
		return nil, fmt.Errorf("failed to create integration: %w", err)
	}

	return mapIntegration(*model), nil
}

// Update 编辑集成
func (s *Service) Update(ctx context.Context, id uint, req *AppIntegrationUpdateRequest) (*AppIntegration, error) {
	if req == nil {
		return nil, fmt.Errorf("request required")
	}

	model, err := s.repo.GetIntegration(ctx, id)
	if err != nil {
		if errors.Is(err, ErrIntegrationNotFound) {
			return nil, err
		}
		return nil, fmt.Errorf("failed to load integration: %w", err)
	}

	if req.Name != nil {
		model.Name = *req.Name
	}
	if req.Vendor != nil {
		model.Vendor = *req.Vendor
	}
	if req.Category != nil {
		model.Category = *req.Category
	}
	if req.Summary != nil {
		model.Summary = *req.Summary
	}
	if req.IconURL != nil {
		model.IconURL = *req.IconURL
	}
	if req.Capabilities != nil {
		model.Capabilities = encodeJSON(req.Capabilities)
	}
	if req.ConfigSchema != nil {
		model.ConfigSchema = encodeJSON(req.ConfigSchema)
	}
	if req.IFrameURL != nil {
		model.IFrameURL = *req.IFrameURL
	}
	if req.Enabled != nil {
		model.Enabled = *req.Enabled
	}
	model.UpdatedAt = time.Now()

	if err := s.repo.SaveIntegration(ctx, model); err != nil {
		return nil, fmt.Errorf("failed to update integration: %w", err)
	}

	return mapIntegration(*model), nil
}

// Delete 删除集成
func (s *Service) Delete(ctx context.Context, id uint) error {
	return s.repo.DeleteIntegration(ctx, id)
}

func mapIntegrations(list []models.AppIntegration) []*AppIntegration {
	result := make([]*AppIntegration, 0, len(list))
	for _, item := range list {
		result = append(result, mapIntegration(item))
	}
	return result
}

func mapIntegration(item models.AppIntegration) *AppIntegration {
	return &AppIntegration{
		ID:             item.ID,
		Name:           item.Name,
		Slug:           item.Slug,
		Vendor:         item.Vendor,
		Category:       item.Category,
		Summary:        item.Summary,
		IconURL:        item.IconURL,
		Capabilities:   decodeStringArray(item.Capabilities),
		ConfigSchema:   decodeObject(item.ConfigSchema),
		IFrameURL:      item.IFrameURL,
		Enabled:        item.Enabled,
		LastSyncStatus: item.LastSyncStatus,
		CreatedAt:      item.CreatedAt,
		UpdatedAt:      item.UpdatedAt,
	}
}

func encodeJSON(v interface{}) string {
	if v == nil {
		return ""
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(raw)
}

func decodeStringArray(raw string) []string {
	if raw == "" {
		return nil
	}
	var arr []string
	if err := json.Unmarshal([]byte(raw), &arr); err != nil {
		return nil
	}
	return arr
}

func decodeObject(raw string) map[string]interface{} {
	if raw == "" {
		return nil
	}
	var obj map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &obj); err != nil {
		return nil
	}
	return obj
}

var slugPattern = regexp.MustCompile(`[^a-z0-9\-]+`)

func normalizeSlug(s string) string {
	s = strings.TrimSpace(strings.ToLower(s))
	if s == "" {
		return ""
	}
	s = slugPattern.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	return s
}
