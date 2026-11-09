package application

// 转接并发与目标校验（todo 转接并发与目标校验 gate）：目标坐席前置校验
// （存在/作用域/可接）、原子 CAS 分配更新、系统消息失败上抛。哨兵语义经
// errors.Is 比对，交付层映射明确 HTTP 结果。

import (
	"context"
	"errors"
	"strings"
	"testing"

	"servify/apps/server/internal/modules/conversation/domain"
	routingapp "servify/apps/server/internal/modules/routing/application"
	"servify/apps/server/internal/platform/auth"
)

type stubTransferValidator struct {
	err                    error
	lastAgentID            uint
	lastTenantID, lastWsID string
	calls                  int
}

func (v *stubTransferValidator) ValidateTransferTarget(ctx context.Context, agentID uint, tenantID, workspaceID string) error {
	v.calls++
	v.lastAgentID = agentID
	v.lastTenantID = tenantID
	v.lastWsID = workspaceID
	return v.err
}

func newTransferConversation() *domain.Conversation {
	fromAgent := uint(7)
	return &domain.Conversation{
		ID:     "conv-transfer",
		Status: domain.ConversationStatusActive,
		Participants: []domain.Participant{
			{ID: "visitor:1", Role: domain.ParticipantRoleCustomer},
			{ID: "agent:7", UserID: &fromAgent, Role: domain.ParticipantRoleAgent},
		},
	}
}

func transferRepo() *stubConversationRepo {
	conv := newTransferConversation()
	return &stubConversationRepo{conversations: map[string]*domain.Conversation{conv.ID: conv}}
}

func TestTransferSuccessUpdatesAgentAtomically(t *testing.T) {
	repo := transferRepo()
	publisher := &stubConversationPublisher{}
	validator := &stubTransferValidator{}
	svc := NewService(repo, publisher).AttachTransferTargetValidator(validator)
	scoped := auth.ContextWithScope(context.Background(), "tenant-1", "ws-1")

	dto, err := svc.Transfer(scoped, "conv-transfer", 9)
	if err != nil {
		t.Fatalf("expected transfer success, got %v", err)
	}
	if validator.calls != 1 || validator.lastAgentID != 9 || validator.lastTenantID != "tenant-1" || validator.lastWsID != "ws-1" {
		t.Fatalf("validator not called with scope context: %+v", validator)
	}

	stored := repo.conversations["conv-transfer"]
	agent := conversationAgentUserID(stored)
	if agent == nil || *agent != 9 {
		t.Fatalf("expected agent participant updated to 9, got %+v", stored.Participants)
	}
	if stored.Status != domain.ConversationStatusTransferred {
		t.Fatalf("expected transferred status, got %s", stored.Status)
	}
	// 返回 DTO 反映原子更新后的最新分配（重读而非入参快照）。
	found := false
	for _, p := range dto.Participants {
		if p.Role == domain.ParticipantRoleAgent && p.UserID != nil && *p.UserID == 9 {
			found = true
		}
	}
	if !found {
		t.Fatalf("dto missing updated agent participant: %+v", dto.Participants)
	}
	// 系统消息 + routing.transfer_completed 事件都要发出。
	messages := repo.messages["conv-transfer"]
	if len(messages) != 1 || messages[0].Kind != domain.MessageKindSystem {
		t.Fatalf("expected one system message, got %+v", messages)
	}
	var routingEvent bool
	for _, e := range publisher.events {
		if e.Name() == routingapp.RoutingTransferCompletedEventName {
			routingEvent = true
		}
	}
	if !routingEvent {
		t.Fatalf("expected routing transfer completed event, got %+v", publisher.events)
	}
}

func TestTransferTargetValidationFailuresSurface(t *testing.T) {
	cases := []struct {
		name string
		err  error
	}{
		{"not found", ErrTransferTargetNotFound},
		{"out of scope", ErrTransferTargetOutOfScope},
		{"unavailable", ErrTransferTargetUnavailable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := transferRepo()
			publisher := &stubConversationPublisher{}
			svc := NewService(repo, publisher).AttachTransferTargetValidator(&stubTransferValidator{err: tc.err})
			_, err := svc.Transfer(context.Background(), "conv-transfer", 9)
			if !errors.Is(err, tc.err) {
				t.Fatalf("expected %v, got %v", tc.err, err)
			}
			// 校验失败必须发生在任何写路径之前：分配、状态、系统消息零改动。
			stored := repo.conversations["conv-transfer"]
			if stored.Status != domain.ConversationStatusActive {
				t.Fatalf("status must stay active on validation failure, got %s", stored.Status)
			}
			if agent := conversationAgentUserID(stored); agent == nil || *agent != 7 {
				t.Fatalf("agent participant must stay on 7, got %+v", stored.Participants)
			}
			if len(repo.messages["conv-transfer"]) != 0 {
				t.Fatalf("no system message expected on validation failure, got %+v", repo.messages["conv-transfer"])
			}
			if len(publisher.events) != 0 {
				t.Fatalf("no events expected on validation failure, got %+v", publisher.events)
			}
		})
	}
}

func TestTransferWithoutValidatorKeepsLegacyPath(t *testing.T) {
	repo := transferRepo()
	svc := NewService(repo, &stubConversationPublisher{})
	if _, err := svc.Transfer(context.Background(), "conv-transfer", 9); err != nil {
		t.Fatalf("expected legacy transfer without validator, got %v", err)
	}
	if agent := conversationAgentUserID(repo.conversations["conv-transfer"]); agent == nil || *agent != 9 {
		t.Fatalf("expected agent updated to 9, got %+v", repo.conversations["conv-transfer"].Participants)
	}
}

func TestTransferConcurrentConflictReturnsSentinel(t *testing.T) {
	repo := &scriptedRepo{conversations: map[string]*domain.Conversation{"conv-transfer": newTransferConversation()}}
	repo.transferAtomicMiss = true
	publisher := &stubConversationPublisher{}
	svc := NewService(repo, publisher)

	_, err := svc.Transfer(context.Background(), "conv-transfer", 9)
	if !errors.Is(err, ErrConcurrentTransfer) {
		t.Fatalf("expected ErrConcurrentTransfer, got %v", err)
	}
	// 败者不得留下系统消息或事件：时间线与实际分配保持一致。
	if len(repo.messages) != 0 {
		t.Fatalf("loser must not append system message, got %+v", repo.messages)
	}
	if len(publisher.events) != 0 {
		t.Fatalf("loser must not publish events, got %+v", publisher.events)
	}
}

func TestTransferAtomicErrorPropagates(t *testing.T) {
	repo := &scriptedRepo{conversations: map[string]*domain.Conversation{"conv-transfer": newTransferConversation()}}
	repo.transferAtomicErr = errors.New("db down")
	svc := NewService(repo, &stubConversationPublisher{})

	if _, err := svc.Transfer(context.Background(), "conv-transfer", 9); err == nil || !strings.Contains(err.Error(), "db down") {
		t.Fatalf("expected atomic error propagated, got %v", err)
	}
}

func TestTransferSystemMessageErrorPropagates(t *testing.T) {
	repo := &scriptedRepo{conversations: map[string]*domain.Conversation{"conv-transfer": newTransferConversation()}}
	repo.appendErr = errors.New("append down")
	publisher := &stubConversationPublisher{}
	svc := NewService(repo, publisher)

	_, err := svc.Transfer(context.Background(), "conv-transfer", 9)
	if err == nil || !strings.Contains(err.Error(), "append down") {
		t.Fatalf("expected system message error propagated, got %v", err)
	}
	if len(publisher.events) != 0 {
		t.Fatalf("transfer event must not publish when system message fails, got %+v", publisher.events)
	}
}
