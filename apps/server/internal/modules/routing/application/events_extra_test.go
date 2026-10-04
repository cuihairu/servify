package application

// 覆盖补充：BufferPublisher 攒后发契约（sqlite 事务内事件缓冲，见
// delivery/session_transfer_adapter.go）与评分审计落库失败路径。

import (
	"context"
	"errors"
	"testing"

	"servify/apps/server/internal/modules/routing/domain"
)

func TestBufferPublisherDefersEventsUntilDrained(t *testing.T) {
	buf := NewBufferPublisher(&stubRoutingPublisher{})
	if buf == nil {
		t.Fatal("constructor returned nil")
	}
	if err := buf.Publish(context.Background(), NewRoutingEvent(RoutingAgentAssignedEventName, "conv-1", nil)); err != nil {
		t.Fatalf("buffered publish must not fail: %v", err)
	}
	if err := buf.Publish(context.Background(), NewRoutingEvent(RoutingTransferCompletedEventName, "conv-1", nil)); err != nil {
		t.Fatalf("buffered publish must not fail: %v", err)
	}
	drained := buf.Events()
	if len(drained) != 2 || drained[0].Name() != RoutingAgentAssignedEventName {
		t.Fatalf("drained = %+v", drained)
	}
	// 取后清空：第二次取为空（一次性语义）。
	if again := buf.Events(); len(again) != 0 {
		t.Fatalf("second drain = %+v want empty", again)
	}
}

func TestBufferPublisherNilTargetStillBuffers(t *testing.T) {
	buf := NewBufferPublisher(nil)
	if err := buf.Publish(context.Background(), NewRoutingEvent("x", "conv-2", nil)); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if len(buf.Events()) != 1 {
		t.Fatal("nil target 仍应入队，由事务拥有者决定去向")
	}
}

// auditFailRepo CreateAssignment 成功但评分审计落库失败——驱动
// AssignAgent 的 persist scoring audit 错误包装分支。
type auditFailRepo struct {
	stubRoutingRepo
}

func (a *auditFailRepo) CreateRoutingAssignment(context.Context, *domain.RoutingAssignment) error {
	return errors.New("audit down")
}

func TestAssignAgentWrapsScoringAuditError(t *testing.T) {
	svc := NewService(&auditFailRepo{}, nil)
	scoring := &ScoringDetail{Strategy: "default_weighted_v1", TotalScore: 0.8}
	_, err := svc.AssignAgent(context.Background(), AssignAgentCommand{
		SessionID: "conv-1", AgentID: 2, Scoring: scoring,
	})
	if err == nil || errors.Unwrap(err) == nil || err.Error() != "persist scoring audit: audit down" {
		t.Fatalf("err = %v want wrapped audit error", err)
	}
}

func TestListRoutingAssignmentsRepoErrorPropagates(t *testing.T) {
	svc := NewService(&stubRoutingRepo{err: errors.New("repo down")}, nil)
	if _, err := svc.ListRoutingAssignments(context.Background(), "conv-1", 10); err == nil {
		t.Fatal("repo error must propagate")
	}
}

func TestWithScorerNilReceiverChainsNil(t *testing.T) {
	if (*Service)(nil).WithScorer(NewDefaultScorer(DefaultWeightSet)) != nil {
		t.Fatal("nil receiver must chain nil")
	}
}
