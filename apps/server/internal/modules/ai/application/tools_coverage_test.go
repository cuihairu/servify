package application

import (
	"context"
	"testing"
)

// 覆盖 Execute 的两个前置拦截分支：ToolPolicy 未启用、目标工具不在白名单。
func TestToolExecutorExecutePolicyGateBranches(t *testing.T) {
	registry := NewToolRegistry()
	registry.Register(NewTicketLookupTool(stubTicketLookup{}))
	executor := NewToolExecutor(registry, func(req AIRequest, tool Tool) error { return nil })

	t.Run("disabled policy", func(t *testing.T) {
		_, err := executor.Execute(context.Background(), AIRequest{
			ToolPolicy: ToolPolicy{Enabled: false, AllowedTools: []string{"ticket_lookup"}},
		}, "ticket_lookup", nil)
		if err == nil || err.Error() != "tool execution is disabled" {
			t.Fatalf("expected disabled error, got %v", err)
		}
	})

	t.Run("tool not in allowlist", func(t *testing.T) {
		_, err := executor.Execute(context.Background(), AIRequest{
			ToolPolicy: ToolPolicy{Enabled: true, AllowedTools: []string{"customer_lookup"}},
		}, "ticket_lookup", nil)
		if err == nil || err.Error() != "tool ticket_lookup is not allowed" {
			t.Fatalf("expected not-allowed error, got %v", err)
		}
	})
}
