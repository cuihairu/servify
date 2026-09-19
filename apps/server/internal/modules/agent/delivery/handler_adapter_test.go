package delivery

// HandlerServiceAdapter 的行为分支覆盖：错误传播、吞错语义、nil logger、
// skills 解析。主路径集成由 handlers 层 agent_handler 测试覆盖。

import (
	"context"
	"errors"
	"testing"
	"time"

	agentapp "servify/apps/server/internal/modules/agent/application"
	agentdomain "servify/apps/server/internal/modules/agent/domain"

	"github.com/sirupsen/logrus"

	"servify/apps/server/internal/models"
)

// statsFailingRepo 让 GetStats 失败以覆盖错误传播分支。
type statsFailingRepo struct {
	maintenanceTestRepo
	statsErr error
}

func (r *statsFailingRepo) GetStats(ctx context.Context, agentUserID *uint) (*agentapp.AgentStatsDTO, error) {
	return nil, r.statsErr
}

func TestHandlerServiceAdapter_GetAgentStatsError(t *testing.T) {
	module := agentapp.NewService(&statsFailingRepo{statsErr: errors.New("boom: stats")}, &maintenanceTestRegistry{items: map[uint]agentapp.AgentRuntimeDTO{}})
	adapter := NewHandlerServiceAdapter(module, logrus.New())

	if _, err := adapter.GetAgentStats(context.Background(), nil); err == nil || err.Error() != "boom: stats" {
		t.Fatalf("expected stats error propagation, got %v", err)
	}
}

func TestHandlerServiceAdapter_ApplySessionTransferSwallowsError(t *testing.T) {
	// 空 registry + repo miss：ApplySessionTransfer 必须吞掉 module 错误只记日志，
	// 且 nil ctx 回退为 Background 不 panic。
	module := agentapp.NewService(&maintenanceTestRepo{}, &maintenanceTestRegistry{items: map[uint]agentapp.AgentRuntimeDTO{}})
	adapter := NewHandlerServiceAdapter(module, logrus.New())

	from := uint(1)
	adapter.ApplySessionTransfer(nil, "sess-x", &from, 42)
}

func TestHandlerServiceAdapter_GetOnlineAgentMiss(t *testing.T) {
	repo := &maintenanceTestRepo{} // profile/model nil → ErrRecordNotFound
	module := agentapp.NewService(repo, &maintenanceTestRegistry{items: map[uint]agentapp.AgentRuntimeDTO{}})
	adapter := NewHandlerServiceAdapter(module, logrus.New())

	if info, ok := adapter.GetOnlineAgent(context.Background(), 4242); ok || info != nil {
		t.Fatalf("expected miss, got ok=%v info=%v", ok, info)
	}
}

func TestHandlerServiceAdapter_NilLogger(t *testing.T) {
	module := agentapp.NewService(&maintenanceTestRepo{}, &maintenanceTestRegistry{items: map[uint]agentapp.AgentRuntimeDTO{}})
	if adapter := NewHandlerServiceAdapter(module, nil); adapter == nil || adapter.logger == nil {
		t.Fatal("expected default logger for nil logger")
	}
	if m := NewRuntimeMaintenance(nil, module); m == nil || m.logger == nil {
		t.Fatal("expected default logger for nil maintenance logger")
	}
}

func TestHandlerServiceAdapter_ParseSkills(t *testing.T) {
	got := parseSkills(" tech , , voice ")
	if len(got) != 2 || got[0] != "tech" || got[1] != "voice" {
		t.Fatalf("unexpected parsed skills: %v", got)
	}
	if empty := parseSkills(""); len(empty) != 0 {
		t.Fatalf("expected empty skills, got %v", empty)
	}
}

// onlineFixtureRepo/Registry：预置一名在线坐席，驱动 adapter 各转发方法的成功分支。
func onlineFixture() (agentapp.Repository, agentapp.RuntimeRegistry) {
	repo := &maintenanceTestRepo{
		profile: &agentdomain.AgentProfile{UserID: 7, MaxChatConcurrency: 3},
		model:   &models.Agent{UserID: 7, Status: "online"},
	}
	registry := &maintenanceTestRegistry{items: map[uint]agentapp.AgentRuntimeDTO{
		7: {UserID: 7, Status: "online", LastActivity: time.Now()},
	}}
	return repo, registry
}

func TestHandlerServiceAdapter_AllMethods(t *testing.T) {
	repo, registry := onlineFixture()
	adapter := NewHandlerServiceAdapter(agentapp.NewService(repo, registry), logrus.New())
	ctx := context.Background()

	if _, err := adapter.CreateAgent(ctx, &AgentCreateRequest{UserID: 8, Skills: " tech , voice "}); err == nil {
		t.Fatal("expected create error for unseeded user")
	}
	if agent, err := adapter.GetAgentByUserID(ctx, 7); err != nil || agent == nil {
		t.Fatalf("GetAgentByUserID: %v %+v", err, agent)
	}
	if _, err := adapter.GetAgentByUserID(ctx, 99); err == nil {
		t.Fatal("expected miss for unseeded user")
	}
	if agents, err := adapter.ListAgents(ctx, 10); err != nil {
		t.Fatalf("ListAgents: %v", err)
	} else if agents != nil {
		t.Fatalf("expected nil list from stub repo, got %v", agents)
	}
	if err := adapter.AgentGoOnline(ctx, 7); err != nil {
		t.Fatalf("AgentGoOnline: %v", err)
	}
	if err := adapter.AgentGoOnline(ctx, 99); err == nil {
		t.Fatal("expected online error for unseeded user")
	}
	if err := adapter.AgentGoOffline(ctx, 7); err != nil {
		t.Fatalf("AgentGoOffline: %v", err)
	}
	// GoOffline 幂等：未注册坐席也不报错。
	if err := adapter.AgentGoOffline(ctx, 99); err != nil {
		t.Fatalf("AgentGoOffline must be idempotent: %v", err)
	}
	if err := adapter.UpdateAgentStatus(ctx, 7, "busy"); err != nil {
		t.Fatalf("UpdateAgentStatus: %v", err)
	}
	// UpdateStatus 对未注册坐席同样幂等不报错。
	if err := adapter.UpdateAgentStatus(ctx, 99, "busy"); err != nil {
		t.Fatalf("UpdateAgentStatus must be idempotent: %v", err)
	}
	if _, err := adapter.RevokeAgentTokens(ctx, 7); err != nil {
		t.Fatalf("RevokeAgentTokens: %v", err)
	}
	if err := adapter.AssignSessionToAgent(ctx, "sess-a", 7); err != nil {
		t.Fatalf("AssignSessionToAgent: %v", err)
	}
	// ReleaseSession 对未持有会话幂等不报错。
	if err := adapter.ReleaseSessionFromAgent(ctx, "sess-a", 7); err != nil {
		t.Fatalf("ReleaseSessionFromAgent must be idempotent: %v", err)
	}
	info, err := adapter.FindAvailableAgent(ctx, nil, "")
	if err != nil || info == nil || info.UserID != 7 {
		t.Fatalf("FindAvailableAgent: %v %+v", err, info)
	}
	// SelectAgent 直转发：结果语义归 module（已由 application 包测试覆盖）。
	_, _ = adapter.SelectAgent(ctx, SelectionRequest{})
	if online := adapter.GetOnlineAgents(ctx); len(online) != 1 || online[0].UserID != 7 {
		t.Fatalf("unexpected online agents: %+v", online)
	}
	if stats, err := adapter.GetAgentStats(ctx, nil); err != nil || stats == nil {
		t.Fatalf("GetAgentStats: %v %+v", err, stats)
	}

	// 组管理成功路径（stub repo 恒成功）。
	if groups, err := adapter.ListAgentGroups(ctx); err != nil || groups != nil {
		t.Fatalf("ListAgentGroups: %v %v", groups, err)
	}
	// stub repo 的 GetAgentGroup 恒返回 record not found——转发即达，语义正确。
	if _, err := adapter.GetAgentGroup(ctx, 1); err == nil {
		t.Fatal("expected not found from stub repo")
	}
	named := &models.AgentGroup{ID: 1, Name: "g"}
	if err := adapter.CreateAgentGroup(ctx, named); err != nil {
		t.Fatalf("CreateAgentGroup: %v", err)
	}
	if err := adapter.UpdateAgentGroup(ctx, named); err != nil {
		t.Fatalf("UpdateAgentGroup: %v", err)
	}
	if err := adapter.DeleteAgentGroup(ctx, 1); err != nil {
		t.Fatalf("DeleteAgentGroup: %v", err)
	}
	if err := adapter.ReplaceGroupMembers(ctx, 1, []uint{7}); err != nil {
		t.Fatalf("ReplaceGroupMembers: %v", err)
	}
	if members, err := adapter.ListGroupMembers(ctx, 1); err != nil || members != nil {
		t.Fatalf("ListGroupMembers: %v %v", members, err)
	}
}

// groupFailingRepo 让全部组方法失败，覆盖错误分支。
type groupFailingRepo struct {
	maintenanceTestRepo
}

func (r *groupFailingRepo) ListAgentGroups(ctx context.Context) ([]models.AgentGroup, error) {
	return nil, errGroupBoom
}

func (r *groupFailingRepo) GetAgentGroup(ctx context.Context, id uint) (*models.AgentGroup, error) {
	return nil, errGroupBoom
}

func (r *groupFailingRepo) CreateAgentGroup(ctx context.Context, group *models.AgentGroup) error {
	return errGroupBoom
}

func (r *groupFailingRepo) UpdateAgentGroup(ctx context.Context, group *models.AgentGroup) error {
	return errGroupBoom
}

func (r *groupFailingRepo) DeleteAgentGroup(ctx context.Context, id uint) error {
	return errGroupBoom
}

func (r *groupFailingRepo) ReplaceGroupMembers(ctx context.Context, groupID uint, agentUserIDs []uint) error {
	return errGroupBoom
}

func (r *groupFailingRepo) ListGroupMemberIDs(ctx context.Context, groupID uint) ([]uint, error) {
	return nil, errGroupBoom
}

var errGroupBoom = errors.New("boom: group")

func TestHandlerServiceAdapter_GroupErrors(t *testing.T) {
	adapter := NewHandlerServiceAdapter(agentapp.NewService(&groupFailingRepo{}, &maintenanceTestRegistry{items: map[uint]agentapp.AgentRuntimeDTO{}}), logrus.New())
	ctx := context.Background()

	if _, err := adapter.ListAgentGroups(ctx); err == nil {
		t.Fatal("expected list groups error")
	}
	if _, err := adapter.GetAgentGroup(ctx, 1); err == nil {
		t.Fatal("expected get group error")
	}
	if err := adapter.CreateAgentGroup(ctx, &models.AgentGroup{}); err == nil {
		t.Fatal("expected create group error")
	}
	if err := adapter.UpdateAgentGroup(ctx, &models.AgentGroup{}); err == nil {
		t.Fatal("expected update group error")
	}
	if err := adapter.DeleteAgentGroup(ctx, 1); err == nil {
		t.Fatal("expected delete group error")
	}
	if err := adapter.ReplaceGroupMembers(ctx, 1, nil); err == nil {
		t.Fatal("expected replace members error")
	}
	if _, err := adapter.ListGroupMembers(ctx, 1); err == nil {
		t.Fatal("expected list members error")
	}
}

// ---- 专用失败桩：逐一驱动各转发方法的错误分支 ----

// presenceFailingRepo 让状态更新失败（GoOffline 错误分支）。
type presenceFailingRepo struct{ maintenanceTestRepo }

func (r *presenceFailingRepo) UpdatePresenceStatus(ctx context.Context, userID uint, status agentdomain.PresenceStatus) error {
	return errGroupBoom
}

// releaseFailingRepo 让会话释放失败（ReleaseSessionFromAgent 错误分支）。
type releaseFailingRepo struct{ maintenanceTestRepo }

func (r *releaseFailingRepo) ReleaseSession(ctx context.Context, sessionID string, agentUserID uint) error {
	return errGroupBoom
}

// chatLoadFailingRepo 让负载回写失败（ApplySessionTransfer 的吞错分支）。
type chatLoadFailingRepo struct{ maintenanceTestRepo }

func (r *chatLoadFailingRepo) UpdateChatLoad(ctx context.Context, userID uint, currentLoad int) error {
	return errGroupBoom
}

// revokeFailingRepo 让令牌吊销失败（RevokeAgentTokens 错误分支）。
type revokeFailingRepo struct{ maintenanceTestRepo }

func (r *revokeFailingRepo) RevokeUserTokens(ctx context.Context, userID uint, revokeAt time.Time) (int, error) {
	return 0, errGroupBoom
}

func TestHandlerServiceAdapter_ErrorBranches(t *testing.T) {
	ctx := context.Background()

	// UpdateStatus：非法状态串在 module 侧解析失败。
	repo, registry := onlineFixture()
	adapter := NewHandlerServiceAdapter(agentapp.NewService(repo, registry), logrus.New())
	if err := adapter.UpdateAgentStatus(ctx, 7, "bogus"); err == nil {
		t.Fatal("expected invalid status error")
	}

	// GoOffline：repo 状态更新失败。
	presenceAdapter := NewHandlerServiceAdapter(agentapp.NewService(&presenceFailingRepo{}, &maintenanceTestRegistry{items: map[uint]agentapp.AgentRuntimeDTO{}}), logrus.New())
	if err := presenceAdapter.AgentGoOffline(ctx, 7); err == nil {
		t.Fatal("expected presence update error")
	}

	// AssignSession：坐席不存在（runtime miss + lookup miss）。
	if err := adapter.AssignSessionToAgent(ctx, "sess-x", 99); err == nil {
		t.Fatal("expected assign error for unseeded agent")
	}

	// RevokeAgentTokens：repo 吊销失败。
	revokeAdapter := NewHandlerServiceAdapter(agentapp.NewService(&revokeFailingRepo{}, &maintenanceTestRegistry{items: map[uint]agentapp.AgentRuntimeDTO{}}), logrus.New())
	if _, err := revokeAdapter.RevokeAgentTokens(ctx, 7); err == nil {
		t.Fatal("expected revoke repo error")
	}

	// ReleaseSession：repo 释放失败。
	releaseAdapter := NewHandlerServiceAdapter(agentapp.NewService(&releaseFailingRepo{}, &maintenanceTestRegistry{items: map[uint]agentapp.AgentRuntimeDTO{}}), logrus.New())
	if err := releaseAdapter.ReleaseSessionFromAgent(ctx, "sess-x", 7); err == nil {
		t.Fatal("expected release repo error")
	}

	// FindAvailableAgent：空池错误。
	emptyAdapter := NewHandlerServiceAdapter(agentapp.NewService(&maintenanceTestRepo{}, &maintenanceTestRegistry{items: map[uint]agentapp.AgentRuntimeDTO{}}), logrus.New())
	if _, err := emptyAdapter.FindAvailableAgent(ctx, nil, ""); err == nil {
		t.Fatal("expected no-agent error on empty pool")
	}

	// GetOnlineAgent 成功路径。
	if info, ok := adapter.GetOnlineAgent(ctx, 7); !ok || info == nil || info.UserID != 7 {
		t.Fatalf("expected online agent 7, got ok=%v info=%+v", ok, info)
	}

	// ApplySessionTransfer：registry 命中 + 负载回写失败 → 只记日志不 panic。
	loadAdapter := NewHandlerServiceAdapter(agentapp.NewService(&chatLoadFailingRepo{}, &maintenanceTestRegistry{items: map[uint]agentapp.AgentRuntimeDTO{
		7: {UserID: 7, Status: "online", CurrentChatLoad: 1},
	}}), logrus.New())
	from := uint(7)
	loadAdapter.ApplySessionTransfer(ctx, "sess-t", &from, 7)
}
