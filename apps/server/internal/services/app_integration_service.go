package services

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"servify/apps/server/internal/models"

	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

// AppIntegrationService 管理应用市场集成
type AppIntegrationService struct {
	db     *gorm.DB
	logger *logrus.Logger
}

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

// NewAppIntegrationService 初始化服务
func NewAppIntegrationService(db *gorm.DB, logger *logrus.Logger) *AppIntegrationService {
	if logger == nil {
		logger = logrus.New()
	}
	return &AppIntegrationService{db: db, logger: logger}
}

// List 返回集成列表
func (s *AppIntegrationService) List(ctx context.Context, req *AppIntegrationListRequest) ([]*AppIntegration, int64, error) {
	if req.Page < 1 {
		req.Page = 1
	}
	if req.PageSize < 1 || req.PageSize > 100 {
		req.PageSize = 20
	}

	query := applyScopeFilter(s.db.WithContext(ctx).Model(&models.AppIntegration{}), ctx)
	if req.Category != "" {
		query = query.Where("category = ?", req.Category)
	}
	if req.Search != "" {
		term := "%" + req.Search + "%"
		query = query.Where("LOWER(name) LIKE LOWER(?) OR LOWER(vendor) LIKE LOWER(?) OR LOWER(summary) LIKE LOWER(?)", term, term, term)
	}
	if len(req.Status) == 1 {
		if req.Status[0] == "enabled" {
			query = query.Where("enabled = ?", true)
		} else if req.Status[0] == "disabled" {
			query = query.Where("enabled = ?", false)
		}
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("failed to count integrations: %w", err)
	}

	if req.PageSize > 0 {
		offset := (req.Page - 1) * req.PageSize
		query = query.Offset(offset).Limit(req.PageSize)
	}

	var list []models.AppIntegration
	if err := query.Order("created_at DESC").Find(&list).Error; err != nil {
		return nil, 0, fmt.Errorf("failed to list integrations: %w", err)
	}

	return mapIntegrations(list), total, nil
}

// Create 新增集成
func (s *AppIntegrationService) Create(ctx context.Context, req *AppIntegrationCreateRequest) (*AppIntegration, error) {
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

	var exists int64
	if err := applyScopeFilter(s.db.WithContext(ctx).Model(&models.AppIntegration{}), ctx).Where("slug = ?", slug).Count(&exists).Error; err != nil {
		return nil, fmt.Errorf("failed to check slug: %w", err)
	}
	if exists > 0 {
		return nil, fmt.Errorf("integration slug already exists")
	}

	tenantID, workspaceID := tenantAndWorkspace(ctx)
	model := &models.AppIntegration{
		TenantID:       tenantID,
		WorkspaceID:    workspaceID,
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

	if err := s.db.WithContext(ctx).Create(model).Error; err != nil {
		return nil, fmt.Errorf("failed to create integration: %w", err)
	}

	return mapIntegration(*model), nil
}

// Update 编辑集成
func (s *AppIntegrationService) Update(ctx context.Context, id uint, req *AppIntegrationUpdateRequest) (*AppIntegration, error) {
	if req == nil {
		return nil, fmt.Errorf("request required")
	}

	var model models.AppIntegration
	if err := applyScopeFilter(s.db.WithContext(ctx), ctx).First(&model, id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, fmt.Errorf("integration not found")
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

	if err := s.db.WithContext(ctx).Save(&model).Error; err != nil {
		return nil, fmt.Errorf("failed to update integration: %w", err)
	}

	return mapIntegration(model), nil
}

// Delete 删除集成
func (s *AppIntegrationService) Delete(ctx context.Context, id uint) error {
	result := applyScopeFilter(s.db.WithContext(ctx), ctx).Delete(&models.AppIntegration{}, id)
	if result.Error != nil {
		return fmt.Errorf("failed to delete integration: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("integration not found")
	}
	return nil
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
