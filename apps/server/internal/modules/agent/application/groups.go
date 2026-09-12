package application

import (
	"context"
	"errors"
	"strings"

	"servify/apps/server/internal/models"

	"gorm.io/gorm"
)

// ErrGroupNameRequired 组名为空。
var ErrGroupNameRequired = errors.New("agent group name required")

// ErrGroupNotFound 组不存在或非法 ID。
var ErrGroupNotFound = errors.New("agent group not found")

// ErrGroupDuplicate 同名组已存在（租户+工作区内唯一）。
var ErrGroupDuplicate = errors.New("agent group name already exists")

// 组管理（坐席组 CRUD 的应用层入口，管理面 handler 经 delivery 契约调用）。

func (s *Service) ListAgentGroups(ctx context.Context) ([]models.AgentGroup, error) {
	return s.repo.ListAgentGroups(ctx)
}

func (s *Service) GetAgentGroup(ctx context.Context, id uint) (*models.AgentGroup, error) {
	if id == 0 {
		return nil, ErrGroupNotFound
	}
	return s.repo.GetAgentGroup(ctx, id)
}

func (s *Service) CreateAgentGroup(ctx context.Context, group *models.AgentGroup) error {
	if group == nil || strings.TrimSpace(group.Name) == "" {
		return ErrGroupNameRequired
	}
	if err := s.repo.CreateAgentGroup(ctx, group); err != nil {
		if errors.Is(err, gorm.ErrDuplicatedKey) {
			return ErrGroupDuplicate
		}
		return err
	}
	return nil
}

func (s *Service) UpdateAgentGroup(ctx context.Context, group *models.AgentGroup) error {
	if group == nil || group.ID == 0 {
		return ErrGroupNotFound
	}
	if err := s.repo.UpdateAgentGroup(ctx, group); err != nil {
		if errors.Is(err, gorm.ErrDuplicatedKey) {
			return ErrGroupDuplicate
		}
		return err
	}
	return nil
}

func (s *Service) DeleteAgentGroup(ctx context.Context, id uint) error {
	if id == 0 {
		return ErrGroupNotFound
	}
	return s.repo.DeleteAgentGroup(ctx, id)
}

func (s *Service) ReplaceGroupMembers(ctx context.Context, groupID uint, agentUserIDs []uint) error {
	if groupID == 0 {
		return ErrGroupNotFound
	}
	return s.repo.ReplaceGroupMembers(ctx, groupID, agentUserIDs)
}

// ListGroupMembers 组成员读取（含禁用组——管理面需要完整视图；选坐席走 ListEnabledGroupMemberIDs）。
func (s *Service) ListGroupMembers(ctx context.Context, groupID uint) ([]uint, error) {
	if groupID == 0 {
		return nil, ErrGroupNotFound
	}
	return s.repo.ListGroupMemberIDs(ctx, groupID)
}
