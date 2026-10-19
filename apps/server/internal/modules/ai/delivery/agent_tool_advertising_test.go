package delivery

import (
	"context"
	"testing"

	aimodule "servify/apps/server/internal/modules/ai/application"
)

// 已定口径 2026-10-11：三工具保持死路（Handoff 与 PROTOCOL「转接由用户显式发起」
// 相抵、两个 Lookup port 收裸 id 无租户作用域）。装配层 port 未接线时不得把必败
// 工具广告给 agent 循环：ToolPolicy enabled 时 Definitions() 全量注入 LLM，
// 模型一调用即报 "port is not configured" 并烧掉 step 配额。
func TestOrchestratedServiceSkipsUnwiredAgentTools(t *testing.T) {
	svc := NewOrchestratedEnhancedAIService(NewAIService("", ""), nil, nil, "", nil)
	names := map[string]bool{}
	for _, def := range svc.toolExecutor.Definitions() {
		names[def.Name] = true
	}
	for _, dead := range []string{"customer_lookup", "ticket_lookup", "handoff"} {
		if names[dead] {
			t.Errorf("port 未接线的 %s 不应注册进 agent 循环：调用必报 port is not configured，广告只会烧 step 配额", dead)
		}
	}
}

// 接线即恢复：装配处传入非 nil port 时三工具重新注册，且经 executor 可真实执行。
func TestOrchestratedServiceWiresAgentToolsWhenPortsProvided(t *testing.T) {
	svc := NewOrchestratedEnhancedAIService(NewAIService("", ""), nil, nil, "", nil, AgentToolPorts{
		CustomerLookup: stubAgentCustomerLookup{},
		TicketLookup:   stubAgentTicketLookup{},
		Handoff:        stubAgentHandoff{},
	})
	names := map[string]bool{}
	for _, def := range svc.toolExecutor.Definitions() {
		names[def.Name] = true
	}
	for _, want := range []string{"customer_lookup", "ticket_lookup", "handoff"} {
		if !names[want] {
			t.Errorf("传入 port 后 %s 应注册进 agent 循环", want)
		}
	}
	if _, err := svc.toolExecutor.Execute(context.Background(), aimodule.AIRequest{
		ToolPolicy: aimodule.ToolPolicy{Enabled: true},
	}, "customer_lookup", map[string]interface{}{"customer_id": 7}); err != nil {
		t.Fatalf("接线后 customer_lookup 应可执行: %v", err)
	}
	if _, err := svc.toolExecutor.Execute(context.Background(), aimodule.AIRequest{
		ToolPolicy: aimodule.ToolPolicy{Enabled: true},
	}, "ticket_lookup", map[string]interface{}{"ticket_id": 9}); err != nil {
		t.Fatalf("接线后 ticket_lookup 应可执行: %v", err)
	}
	if _, err := svc.toolExecutor.Execute(context.Background(), aimodule.AIRequest{
		ToolPolicy: aimodule.ToolPolicy{Enabled: true},
	}, "handoff", map[string]interface{}{"conversation_id": "c1"}); err != nil {
		t.Fatalf("接线后 handoff 应可执行: %v", err)
	}
}

type stubAgentCustomerLookup struct{}

func (stubAgentCustomerLookup) GetCustomerSummary(ctx context.Context, customerID uint) (map[string]interface{}, error) {
	return map[string]interface{}{"id": customerID}, nil
}

type stubAgentTicketLookup struct{}

func (stubAgentTicketLookup) GetTicketSummary(ctx context.Context, ticketID uint) (map[string]interface{}, error) {
	return map[string]interface{}{"id": ticketID}, nil
}

type stubAgentHandoff struct{}

func (stubAgentHandoff) RequestHandoff(ctx context.Context, conversationID string, reason string) (map[string]interface{}, error) {
	return map[string]interface{}{"conversation_id": conversationID}, nil
}
