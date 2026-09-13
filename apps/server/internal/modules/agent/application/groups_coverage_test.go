package application

import (
	"context"
	"errors"
	"testing"

	"servify/apps/server/internal/models"
	agentdomain "servify/apps/server/internal/modules/agent/domain"

	"gorm.io/gorm"
)

// groupsStubRepo 组合 stubRepo 并注入组操作错误，覆盖应用层错误映射分支。
type groupsStubRepo struct {
	*stubRepo

	groups         []models.AgentGroup
	getGroupErr    error
	createErr      error
	updateErr      error
	deleteErr      error
	replaceErr     error
	memberIDsErr   error
	enabledErr     error
	lastMemberIDs  []uint
	listMemberIDs  []uint
	enabledMembers []uint
}

func (g *groupsStubRepo) ListAgentGroups(ctx context.Context) ([]models.AgentGroup, error) {
	return g.groups, nil
}

func (g *groupsStubRepo) GetAgentGroup(ctx context.Context, id uint) (*models.AgentGroup, error) {
	if g.getGroupErr != nil {
		return nil, g.getGroupErr
	}
	return g.stubRepo.GetAgentGroup(ctx, id)
}

func (g *groupsStubRepo) CreateAgentGroup(ctx context.Context, group *models.AgentGroup) error {
	if g.createErr != nil {
		return g.createErr
	}
	return g.stubRepo.CreateAgentGroup(ctx, group)
}

func (g *groupsStubRepo) UpdateAgentGroup(ctx context.Context, group *models.AgentGroup) error {
	if g.updateErr != nil {
		return g.updateErr
	}
	return g.stubRepo.UpdateAgentGroup(ctx, group)
}

func (g *groupsStubRepo) DeleteAgentGroup(ctx context.Context, id uint) error {
	if g.deleteErr != nil {
		return g.deleteErr
	}
	return g.stubRepo.DeleteAgentGroup(ctx, id)
}

func (g *groupsStubRepo) ReplaceGroupMembers(ctx context.Context, groupID uint, agentUserIDs []uint) error {
	if g.replaceErr != nil {
		return g.replaceErr
	}
	g.lastMemberIDs = append([]uint(nil), agentUserIDs...)
	return nil
}

func (g *groupsStubRepo) ListGroupMemberIDs(ctx context.Context, groupID uint) ([]uint, error) {
	if g.memberIDsErr != nil {
		return nil, g.memberIDsErr
	}
	return g.listMemberIDs, nil
}

func (g *groupsStubRepo) ListEnabledGroupMemberIDs(ctx context.Context, groupID uint) ([]uint, error) {
	if g.enabledErr != nil {
		return nil, g.enabledErr
	}
	return g.enabledMembers, nil
}

func newGroupsService(repo *groupsStubRepo) *Service {
	return NewService(repo, newStubRegistry())
}

func TestServiceListAgentGroups(t *testing.T) {
	repo := &groupsStubRepo{
		stubRepo: &stubRepo{},
		groups: []models.AgentGroup{
			{ID: 1, Name: "billing", Enabled: true},
			{ID: 2, Name: "vip", Enabled: false},
		},
	}
	svc := newGroupsService(repo)

	got, err := svc.ListAgentGroups(context.Background())
	if err != nil {
		t.Fatalf("ListAgentGroups() error = %v", err)
	}
	if len(got) != 2 || got[0].Name != "billing" || got[1].Name != "vip" {
		t.Fatalf("unexpected groups: %+v", got)
	}
}

func TestServiceGetAgentGroup(t *testing.T) {
	repo := &groupsStubRepo{stubRepo: &stubRepo{}, listMemberIDs: []uint{3, 4}}
	repo.group = &models.AgentGroup{ID: 2, Name: "vip"}
	svc := newGroupsService(repo)
	ctx := context.Background()

	if _, err := svc.GetAgentGroup(ctx, 0); !errors.Is(err, ErrGroupNotFound) {
		t.Fatalf("id=0 want ErrGroupNotFound, got %v", err)
	}
	got, err := svc.GetAgentGroup(ctx, 2)
	if err != nil || got.Name != "vip" {
		t.Fatalf("GetAgentGroup() = %+v, %v", got, err)
	}
	repo.getGroupErr = errors.New("get boom")
	if _, err := svc.GetAgentGroup(ctx, 2); err == nil || err.Error() != "get boom" {
		t.Fatalf("want raw get error, got %v", err)
	}
}

func TestServiceCreateAgentGroup(t *testing.T) {
	svc := newGroupsService(&groupsStubRepo{stubRepo: &stubRepo{}})
	ctx := context.Background()

	if err := svc.CreateAgentGroup(ctx, nil); !errors.Is(err, ErrGroupNameRequired) {
		t.Fatalf("nil group want ErrGroupNameRequired, got %v", err)
	}
	if err := svc.CreateAgentGroup(ctx, &models.AgentGroup{Name: "   "}); !errors.Is(err, ErrGroupNameRequired) {
		t.Fatalf("blank name want ErrGroupNameRequired, got %v", err)
	}
	if err := svc.CreateAgentGroup(ctx, &models.AgentGroup{Name: "billing"}); err != nil {
		t.Fatalf("CreateAgentGroup() error = %v", err)
	}

	dup := &groupsStubRepo{stubRepo: &stubRepo{}, createErr: gorm.ErrDuplicatedKey}
	if err := newGroupsService(dup).CreateAgentGroup(ctx, &models.AgentGroup{Name: "billing"}); !errors.Is(err, ErrGroupDuplicate) {
		t.Fatalf("duplicate want ErrGroupDuplicate, got %v", err)
	}
	boom := &groupsStubRepo{stubRepo: &stubRepo{}, createErr: errors.New("insert boom")}
	if err := newGroupsService(boom).CreateAgentGroup(ctx, &models.AgentGroup{Name: "billing"}); err == nil || err.Error() != "insert boom" {
		t.Fatalf("want raw create error, got %v", err)
	}
}

func TestServiceUpdateAgentGroup(t *testing.T) {
	repo := &groupsStubRepo{stubRepo: &stubRepo{}}
	svc := newGroupsService(repo)
	ctx := context.Background()

	if err := svc.UpdateAgentGroup(ctx, nil); !errors.Is(err, ErrGroupNotFound) {
		t.Fatalf("nil group want ErrGroupNotFound, got %v", err)
	}
	if err := svc.UpdateAgentGroup(ctx, &models.AgentGroup{Name: "x"}); !errors.Is(err, ErrGroupNotFound) {
		t.Fatalf("id=0 want ErrGroupNotFound, got %v", err)
	}
	if err := svc.UpdateAgentGroup(ctx, &models.AgentGroup{ID: 1, Name: "x"}); err != nil {
		t.Fatalf("UpdateAgentGroup() error = %v", err)
	}

	dup := &groupsStubRepo{stubRepo: &stubRepo{}, updateErr: gorm.ErrDuplicatedKey}
	if err := newGroupsService(dup).UpdateAgentGroup(ctx, &models.AgentGroup{ID: 1, Name: "x"}); !errors.Is(err, ErrGroupDuplicate) {
		t.Fatalf("duplicate want ErrGroupDuplicate, got %v", err)
	}
	boom := &groupsStubRepo{stubRepo: &stubRepo{}, updateErr: errors.New("update boom")}
	if err := newGroupsService(boom).UpdateAgentGroup(ctx, &models.AgentGroup{ID: 1, Name: "x"}); err == nil || err.Error() != "update boom" {
		t.Fatalf("want raw update error, got %v", err)
	}
}

func TestServiceDeleteAgentGroup(t *testing.T) {
	repo := &groupsStubRepo{stubRepo: &stubRepo{}}
	svc := newGroupsService(repo)
	ctx := context.Background()

	if err := svc.DeleteAgentGroup(ctx, 0); !errors.Is(err, ErrGroupNotFound) {
		t.Fatalf("id=0 want ErrGroupNotFound, got %v", err)
	}
	if err := svc.DeleteAgentGroup(ctx, 1); err != nil {
		t.Fatalf("DeleteAgentGroup() error = %v", err)
	}
	repo.deleteErr = errors.New("delete boom")
	if err := svc.DeleteAgentGroup(ctx, 1); err == nil || err.Error() != "delete boom" {
		t.Fatalf("want raw delete error, got %v", err)
	}
}

func TestServiceReplaceGroupMembers(t *testing.T) {
	repo := &groupsStubRepo{stubRepo: &stubRepo{}}
	svc := newGroupsService(repo)
	ctx := context.Background()

	if err := svc.ReplaceGroupMembers(ctx, 0, []uint{1}); !errors.Is(err, ErrGroupNotFound) {
		t.Fatalf("groupID=0 want ErrGroupNotFound, got %v", err)
	}
	if err := svc.ReplaceGroupMembers(ctx, 1, []uint{1, 2}); err != nil {
		t.Fatalf("ReplaceGroupMembers() error = %v", err)
	}
	if len(repo.lastMemberIDs) != 2 {
		t.Fatalf("member ids not forwarded: %+v", repo.lastMemberIDs)
	}
	repo.replaceErr = errors.New("replace boom")
	if err := svc.ReplaceGroupMembers(ctx, 1, nil); err == nil || err.Error() != "replace boom" {
		t.Fatalf("want raw replace error, got %v", err)
	}
}

func TestServiceListGroupMembers(t *testing.T) {
	repo := &groupsStubRepo{stubRepo: &stubRepo{}, listMemberIDs: []uint{3, 4}}
	svc := newGroupsService(repo)
	ctx := context.Background()

	if _, err := svc.ListGroupMembers(ctx, 0); !errors.Is(err, ErrGroupNotFound) {
		t.Fatalf("groupID=0 want ErrGroupNotFound, got %v", err)
	}
	got, err := svc.ListGroupMembers(ctx, 1)
	if err != nil || len(got) != 2 || got[0] != 3 {
		t.Fatalf("ListGroupMembers() = %+v, %v", got, err)
	}
	repo.memberIDsErr = errors.New("members boom")
	if _, err := svc.ListGroupMembers(ctx, 1); err == nil || err.Error() != "members boom" {
		t.Fatalf("want raw members error, got %v", err)
	}
}

// 组内无人且组查询失败：必须显式报组不可用，而不是静默降级全局池。
func TestSelectAgentGroupUnavailableWhenGroupLookupFails(t *testing.T) {
	repo := &stubRepo{
		runtimes: []AgentRuntimeDTO{{
			UserID:             9,
			Status:             string(agentdomain.PresenceStatusOnline),
			MaxChatConcurrency: 2,
		}},
		groupMembers: []uint{5}, // 候选池里没有 5
		group:        nil,       // GetAgentGroup 返回 not found
	}
	svc := NewService(repo, newStubRegistry())

	groupID := uint(1)
	_, err := svc.SelectAgent(context.Background(), SelectionRequest{GroupID: &groupID})
	if !errors.Is(err, ErrGroupUnavailable) {
		t.Fatalf("want ErrGroupUnavailable, got %v", err)
	}
}
