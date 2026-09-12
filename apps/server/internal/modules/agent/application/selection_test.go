package application

import (
	"context"
	"errors"
	"testing"

	"servify/apps/server/internal/models"
	agentdomain "servify/apps/server/internal/modules/agent/domain"
)

func uintPtr(v uint) *uint { return &v }

func onlineAgent(userID uint, load, max int) AgentRuntimeDTO {
	return AgentRuntimeDTO{
		UserID:             userID,
		Status:             string(agentdomain.PresenceStatusOnline),
		CurrentChatLoad:    load,
		MaxChatConcurrency: max,
		Rating:             5,
	}
}

func TestSelectAgentGlobalWhenNoGroupNoAffinity(t *testing.T) {
	repo := &stubRepo{runtimes: []AgentRuntimeDTO{onlineAgent(1, 0, 5), onlineAgent(2, 3, 5)}}
	svc := NewService(repo, nil)

	result, err := svc.SelectAgent(context.Background(), SelectionRequest{})
	if err != nil {
		t.Fatalf("select: %v", err)
	}
	if result.Source != SelectionSourceGlobal {
		t.Fatalf("expected global source, got %s", result.Source)
	}
	if result.Agent.UserID != 1 {
		t.Fatalf("lower-load agent expected, got %d", result.Agent.UserID)
	}
}

func TestSelectAgentAffinityHardWins(t *testing.T) {
	// agent 2 负载更高（score 更低），但亲和硬命中必须选它
	repo := &stubRepo{
		runtimes:             []AgentRuntimeDTO{onlineAgent(1, 0, 5), onlineAgent(2, 4, 5)},
		lastAgentForCustomer: uintPtr(2),
	}
	svc := NewService(repo, nil)

	result, err := svc.SelectAgent(context.Background(), SelectionRequest{
		Affinity: &AffinityHint{CustomerUserID: 100},
	})
	if err != nil {
		t.Fatalf("select: %v", err)
	}
	if result.Source != SelectionSourceAffinity || result.Agent.UserID != 2 {
		t.Fatalf("affinity hit expected, got source=%s agent=%d", result.Source, result.Agent.UserID)
	}
}

func TestSelectAgentAffinityFallsThroughWhenBusyOrOffline(t *testing.T) {
	// 池里另备可用坐席 9：亲和降级后全局池必须能接住
	for _, tc := range []struct {
		name string
		repo *stubRepo
	}{
		{"busy", &stubRepo{runtimes: []AgentRuntimeDTO{onlineAgent(2, 5, 5), onlineAgent(9, 0, 5)}, lastAgentForCustomer: uintPtr(2)}},
		{"offline", &stubRepo{runtimes: []AgentRuntimeDTO{{UserID: 2, Status: string(agentdomain.PresenceStatusOffline), MaxChatConcurrency: 5}, onlineAgent(9, 0, 5)}, lastAgentForCustomer: uintPtr(2)}},
		{"no-history", &stubRepo{runtimes: []AgentRuntimeDTO{onlineAgent(2, 0, 5)}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc := NewService(tc.repo, nil)
			result, err := svc.SelectAgent(context.Background(), SelectionRequest{
				Affinity: &AffinityHint{CustomerUserID: 100},
			})
			if err != nil {
				t.Fatalf("select: %v", err)
			}
			if result.Source != SelectionSourceGlobal {
				t.Fatalf("expected global fallback, got %s", result.Source)
			}
		})
	}
}

func TestSelectAgentGroupPicksBestMember(t *testing.T) {
	repo := &stubRepo{
		runtimes:     []AgentRuntimeDTO{onlineAgent(1, 0, 5), onlineAgent(2, 0, 5), onlineAgent(3, 0, 5)},
		group:        &models.AgentGroup{ID: 10, Enabled: true, OverflowPolicy: "none"},
		groupMembers: []uint{2, 3},
	}
	svc := NewService(repo, nil)

	gid := uint(10)
	result, err := svc.SelectAgent(context.Background(), SelectionRequest{GroupID: &gid})
	if err != nil {
		t.Fatalf("select: %v", err)
	}
	if result.Source != SelectionSourceGroup || result.Agent.UserID != 2 {
		t.Fatalf("group member 2 expected, got source=%s agent=%d", result.Source, result.Agent.UserID)
	}
}

func TestSelectAgentGroupOverflowPolicies(t *testing.T) {
	gid := uint(10)
	// overflow=global：组内无人 → 落全局池
	repo := &stubRepo{
		runtimes:     []AgentRuntimeDTO{onlineAgent(9, 0, 5)},
		group:        &models.AgentGroup{ID: 10, Enabled: true, OverflowPolicy: "global"},
		groupMembers: []uint{2}, // 2 不在 runtime 池
	}
	svc := NewService(repo, nil)
	result, err := svc.SelectAgent(context.Background(), SelectionRequest{GroupID: &gid})
	if err != nil {
		t.Fatalf("select: %v", err)
	}
	if result.Source != SelectionSourceGlobal || result.Agent.UserID != 9 {
		t.Fatalf("global overflow expected, got source=%s agent=%d", result.Source, result.Agent.UserID)
	}

	// overflow=none：组内无人 → ErrGroupUnavailable
	repo.group.OverflowPolicy = "none"
	_, err = svc.SelectAgent(context.Background(), SelectionRequest{GroupID: &gid})
	if !errors.Is(err, ErrGroupUnavailable) {
		t.Fatalf("want ErrGroupUnavailable, got %v", err)
	}
}

func TestSelectAgentGroupMissingOrDisabledIsUnavailable(t *testing.T) {
	gid := uint(10)
	// 组不存在：ListEnabledGroupMemberIDs 返回空 → 不可用（不静默改道）
	repo := &stubRepo{runtimes: []AgentRuntimeDTO{onlineAgent(9, 0, 5)}}
	svc := NewService(repo, nil)
	if _, err := svc.SelectAgent(context.Background(), SelectionRequest{GroupID: &gid}); !errors.Is(err, ErrGroupUnavailable) {
		t.Fatalf("missing group: want ErrGroupUnavailable, got %v", err)
	}

	// 组禁用：同样不可用
	disabled := &stubRepo{
		runtimes:     []AgentRuntimeDTO{onlineAgent(9, 0, 5)},
		group:        &models.AgentGroup{ID: 10, Enabled: false, OverflowPolicy: "global"},
		groupMembers: nil,
	}
	svc = NewService(disabled, nil)
	if _, err := svc.SelectAgent(context.Background(), SelectionRequest{GroupID: &gid}); !errors.Is(err, ErrGroupUnavailable) {
		t.Fatalf("disabled group: want ErrGroupUnavailable, got %v", err)
	}
}

func TestFindAvailableAgentRemainsGlobalShell(t *testing.T) {
	repo := &stubRepo{runtimes: []AgentRuntimeDTO{onlineAgent(1, 0, 5)}}
	svc := NewService(repo, nil)

	agent, err := svc.FindAvailableAgent(context.Background(), nil, "")
	if err != nil || agent == nil || agent.UserID != 1 {
		t.Fatalf("shell must keep old behavior: agent=%+v err=%v", agent, err)
	}

	// 空池：错误信息保持与旧行为一致
	emptySvc := NewService(&stubRepo{}, nil)
	if _, err := emptySvc.FindAvailableAgent(context.Background(), nil, ""); err == nil || err.Error() != "no available agent found" {
		t.Fatalf("empty pool error expected, got %v", err)
	}
}
