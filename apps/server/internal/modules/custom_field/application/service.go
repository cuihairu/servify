package application

// 动态自定义字段（当前仅 ticket 资源）：定义管理 + 校验。

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	platformauth "servify/apps/server/internal/platform/auth"

	"servify/apps/server/internal/models"
)

// CustomFieldCreateRequest 创建请求
type CustomFieldCreateRequest struct {
	Resource   string      `json:"resource"` // default: ticket
	Key        string      `json:"key" binding:"required"`
	Name       string      `json:"name" binding:"required"`
	Type       string      `json:"type" binding:"required"`
	Required   bool        `json:"required"`
	Active     *bool       `json:"active"`
	Options    interface{} `json:"options"`
	Validation interface{} `json:"validation"`
	ShowWhen   interface{} `json:"show_when"`
}

// CustomFieldUpdateRequest 更新请求
type CustomFieldUpdateRequest struct {
	Name       *string     `json:"name"`
	Type       *string     `json:"type"`
	Required   *bool       `json:"required"`
	Active     *bool       `json:"active"`
	Options    interface{} `json:"options"`
	Validation interface{} `json:"validation"`
	ShowWhen   interface{} `json:"show_when"`
}

var ErrCustomFieldNotFound = errors.New("custom field not found")

var customFieldKeyRe = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

type Repository interface {
	List(ctx context.Context, resource string, activeOnly bool) ([]models.CustomField, error)
	// GetScoped 按主键取字段（上下文租户/工作区过滤）；错误原样透传。
	GetScoped(ctx context.Context, id uint) (*models.CustomField, error)
	Create(ctx context.Context, field *models.CustomField) error
	Save(ctx context.Context, field *models.CustomField) error
	Delete(ctx context.Context, id uint) error
}

type Service struct {
	repo Repository
}

func NewService(repo Repository) *Service {
	return &Service{repo: repo}
}

func (s *Service) List(ctx context.Context, resource string, activeOnly bool) ([]models.CustomField, error) {
	if resource == "" {
		resource = "ticket"
	}
	return s.repo.List(ctx, resource, activeOnly)
}

func (s *Service) Get(ctx context.Context, id uint) (*models.CustomField, error) {
	return s.repo.GetScoped(ctx, id)
}

func (s *Service) Create(ctx context.Context, req *CustomFieldCreateRequest) (*models.CustomField, error) {
	if req == nil {
		return nil, errors.New("request required")
	}
	resource := strings.TrimSpace(req.Resource)
	if resource == "" {
		resource = "ticket"
	}
	if resource != "ticket" {
		return nil, fmt.Errorf("unsupported resource: %s", resource)
	}

	key := strings.ToLower(strings.TrimSpace(req.Key))
	if !customFieldKeyRe.MatchString(key) {
		return nil, fmt.Errorf("invalid key: %s (must match %s)", key, customFieldKeyRe.String())
	}
	typ := strings.TrimSpace(req.Type)
	if !isAllowedCustomFieldType(typ) {
		return nil, fmt.Errorf("invalid type: %s", typ)
	}

	optionsJSON, err := marshalOptionalJSON(req.Options)
	if err != nil {
		return nil, fmt.Errorf("invalid options: %w", err)
	}
	validationJSON, err := marshalOptionalJSON(req.Validation)
	if err != nil {
		return nil, fmt.Errorf("invalid validation: %w", err)
	}
	showWhenJSON, err := marshalOptionalJSON(req.ShowWhen)
	if err != nil {
		return nil, fmt.Errorf("invalid show_when: %w", err)
	}

	active := true
	if req.Active != nil {
		active = *req.Active
	}

	now := time.Now()
	field := &models.CustomField{
		TenantID:       platformauth.TenantIDFromContext(ctx),
		WorkspaceID:    platformauth.WorkspaceIDFromContext(ctx),
		Resource:       resource,
		Key:            key,
		Name:           strings.TrimSpace(req.Name),
		Type:           typ,
		Required:       req.Required,
		Active:         active,
		OptionsJSON:    optionsJSON,
		ValidationJSON: validationJSON,
		ShowWhenJSON:   showWhenJSON,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	if strings.TrimSpace(field.Name) == "" {
		return nil, errors.New("name required")
	}

	if err := s.repo.Create(ctx, field); err != nil {
		return nil, err
	}
	return field, nil
}

func (s *Service) Update(ctx context.Context, id uint, req *CustomFieldUpdateRequest) (*models.CustomField, error) {
	if req == nil {
		return nil, errors.New("request required")
	}
	field, err := s.repo.GetScoped(ctx, id)
	if err != nil {
		return nil, err
	}
	if req.Name != nil {
		field.Name = strings.TrimSpace(*req.Name)
	}
	if req.Type != nil {
		typ := strings.TrimSpace(*req.Type)
		if !isAllowedCustomFieldType(typ) {
			return nil, fmt.Errorf("invalid type: %s", typ)
		}
		field.Type = typ
	}
	if req.Required != nil {
		field.Required = *req.Required
	}
	if req.Active != nil {
		field.Active = *req.Active
	}
	if req.Options != nil {
		j, err := marshalOptionalJSON(req.Options)
		if err != nil {
			return nil, fmt.Errorf("invalid options: %w", err)
		}
		field.OptionsJSON = j
	}
	if req.Validation != nil {
		j, err := marshalOptionalJSON(req.Validation)
		if err != nil {
			return nil, fmt.Errorf("invalid validation: %w", err)
		}
		field.ValidationJSON = j
	}
	if req.ShowWhen != nil {
		j, err := marshalOptionalJSON(req.ShowWhen)
		if err != nil {
			return nil, fmt.Errorf("invalid show_when: %w", err)
		}
		field.ShowWhenJSON = j
	}

	field.UpdatedAt = time.Now()
	if err := s.repo.Save(ctx, field); err != nil {
		return nil, err
	}
	return field, nil
}

func (s *Service) Delete(ctx context.Context, id uint) error {
	return s.repo.Delete(ctx, id)
}

func isAllowedCustomFieldType(typ string) bool {
	switch typ {
	case "string", "number", "boolean", "date", "select", "multiselect":
		return true
	default:
		return false
	}
}

func marshalOptionalJSON(v interface{}) (string, error) {
	if v == nil {
		return "", nil
	}
	// allow callers to pass raw JSON string
	if s, ok := v.(string); ok {
		if strings.TrimSpace(s) == "" {
			return "", nil
		}
		var tmp interface{}
		if err := json.Unmarshal([]byte(s), &tmp); err != nil {
			return "", err
		}
		return strings.TrimSpace(s), nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(b), nil
}
