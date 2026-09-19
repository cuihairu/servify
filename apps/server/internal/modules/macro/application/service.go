package application

// 宏/模板管理：管理面 CRUD + 工单应用（宏内容落系统评论）。

import (
	"context"
	"errors"
	"time"

	platformauth "servify/apps/server/internal/platform/auth"

	"servify/apps/server/internal/models"
)

// MacroCreateRequest 创建请求
type MacroCreateRequest struct {
	Name        string `json:"name" binding:"required"`
	Description string `json:"description"`
	Content     string `json:"content" binding:"required"`
	Language    string `json:"language"`
}

// MacroUpdateRequest 更新请求
type MacroUpdateRequest struct {
	Description *string `json:"description"`
	Content     *string `json:"content"`
	Language    *string `json:"language"`
	Active      *bool   `json:"active"`
}

var (
	ErrMacroNotFound  = errors.New("macro not found")
	ErrMacroInactive  = errors.New("macro inactive")
	ErrTicketNotFound = errors.New("ticket not found")
)

type Repository interface {
	List(ctx context.Context) ([]models.Macro, error)
	Create(ctx context.Context, macro *models.Macro) error
	// GetScoped 按主键取宏（上下文租户/工作区过滤）；未命中透传 gorm.ErrRecordNotFound。
	GetScoped(ctx context.Context, id uint) (*models.Macro, error)
	Save(ctx context.Context, macro *models.Macro) error
	Delete(ctx context.Context, id uint) error
	GetTicket(ctx context.Context, id uint) (*models.Ticket, error)
	CreateComment(ctx context.Context, comment *models.TicketComment) error
}

type Service struct {
	repo Repository
}

func NewService(repo Repository) *Service {
	return &Service{repo: repo}
}

func (s *Service) List(ctx context.Context) ([]models.Macro, error) {
	return s.repo.List(ctx)
}

func (s *Service) Create(ctx context.Context, req *MacroCreateRequest) (*models.Macro, error) {
	if req == nil {
		return nil, errors.New("request required")
	}
	macro := &models.Macro{
		Name:        req.Name,
		Description: req.Description,
		Content:     req.Content,
		Language:    defaultLang(req.Language),
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
	macro.TenantID, macro.WorkspaceID = platformauth.TenantIDFromContext(ctx), platformauth.WorkspaceIDFromContext(ctx)
	if err := s.repo.Create(ctx, macro); err != nil {
		return nil, err
	}
	return macro, nil
}

func (s *Service) Update(ctx context.Context, id uint, req *MacroUpdateRequest) (*models.Macro, error) {
	if req == nil {
		return nil, errors.New("request required")
	}
	macro, err := s.repo.GetScoped(ctx, id)
	if err != nil {
		return nil, err
	}
	if req.Description != nil {
		macro.Description = *req.Description
	}
	if req.Content != nil {
		macro.Content = *req.Content
	}
	if req.Language != nil {
		macro.Language = defaultLang(*req.Language)
	}
	if req.Active != nil {
		macro.Active = *req.Active
	}
	macro.UpdatedAt = time.Now()
	if err := s.repo.Save(ctx, macro); err != nil {
		return nil, err
	}
	return macro, nil
}

func (s *Service) Delete(ctx context.Context, id uint) error {
	return s.repo.Delete(ctx, id)
}

// ApplyToTicket 将宏内容落为工单系统评论（跨 Macro/Ticket/TicketComment）。
func (s *Service) ApplyToTicket(ctx context.Context, macroID, ticketID, actorID uint) (*models.TicketComment, error) {
	macro, err := s.repo.GetScoped(ctx, macroID)
	if err != nil {
		return nil, err
	}
	if !macro.Active {
		return nil, ErrMacroInactive
	}
	// GetTicket 未命中已由 infra 映射为 ErrTicketNotFound。
	if _, err := s.repo.GetTicket(ctx, ticketID); err != nil {
		return nil, err
	}
	comment := &models.TicketComment{
		TicketID:  ticketID,
		UserID:    actorID,
		Content:   macro.Content,
		Type:      "system",
		CreatedAt: time.Now(),
	}
	if err := s.repo.CreateComment(ctx, comment); err != nil {
		return nil, err
	}
	return comment, nil
}

func defaultLang(lang string) string {
	if lang == "" {
		return "zh"
	}
	return lang
}
