package application

import (
	"context"
	"fmt"
	"testing"
)

type stubTicketLookup struct{}

func (stubTicketLookup) GetTicketSummary(ctx context.Context, ticketID uint) (map[string]interface{}, error) {
	return map[string]interface{}{"ticket_id": ticketID, "status": "open"}, nil
}

type stubCustomerLookup struct{}

func (stubCustomerLookup) GetCustomerSummary(ctx context.Context, customerID uint) (map[string]interface{}, error) {
	return map[string]interface{}{"customer_id": customerID, "name": "Alice"}, nil
}

type stubHandoff struct{}

func (stubHandoff) RequestHandoff(ctx context.Context, conversationID string, reason string) (map[string]interface{}, error) {
	return map[string]interface{}{"conversation_id": conversationID, "reason": reason, "requested": true}, nil
}

func TestToolExecutorExecuteAllowedTool(t *testing.T) {
	registry := NewToolRegistry()
	registry.Register(NewTicketLookupTool(stubTicketLookup{}))
	executor := NewToolExecutor(registry, func(req AIRequest, tool Tool) error { return nil })

	resp, err := executor.Execute(context.Background(), AIRequest{
		ToolPolicy: ToolPolicy{
			Enabled:      true,
			AllowedTools: []string{"ticket_lookup"},
		},
	}, "ticket_lookup", map[string]interface{}{"ticket_id": 42})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if resp["ticket_id"] != uint(42) {
		t.Fatalf("unexpected response: %+v", resp)
	}
}

func TestToolExecutorExecutePermissionDenied(t *testing.T) {
	registry := NewToolRegistry()
	registry.Register(NewCustomerLookupTool(stubCustomerLookup{}))
	executor := NewToolExecutor(registry, func(req AIRequest, tool Tool) error {
		return fmt.Errorf("denied")
	})

	_, err := executor.Execute(context.Background(), AIRequest{
		ToolPolicy: ToolPolicy{
			Enabled:      true,
			AllowedTools: []string{"customer_lookup"},
		},
	}, "customer_lookup", map[string]interface{}{"customer_id": 7})
	if err == nil || err.Error() != "denied" {
		t.Fatalf("expected denied error, got %v", err)
	}
}

func TestHandoffToolExecute(t *testing.T) {
	tool := NewHandoffTool(stubHandoff{})
	resp, err := tool.Execute(context.Background(), map[string]interface{}{
		"conversation_id": "c1",
		"reason":          "user requested human",
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if resp["requested"] != true {
		t.Fatalf("unexpected response: %+v", resp)
	}
}

func TestToolExecutorDefinitions(t *testing.T) {
	registry := NewToolRegistry()
	registry.Register(NewTicketLookupTool(stubTicketLookup{}))
	registry.Register(NewCustomerLookupTool(stubCustomerLookup{}))
	executor := NewToolExecutor(registry, nil)

	defs := executor.Definitions()
	if len(defs) != 2 {
		t.Fatalf("expected 2 tool definitions, got %d", len(defs))
	}
	names := make(map[string]bool)
	for _, d := range defs {
		names[d.Name] = true
		if d.Description == "" {
			t.Fatalf("tool %q has empty description", d.Name)
		}
		if d.InputSchema == nil {
			t.Fatalf("tool %q has nil input schema", d.Name)
		}
	}
	if !names["ticket_lookup"] {
		t.Fatal("expected ticket_lookup in definitions")
	}
	if !names["customer_lookup"] {
		t.Fatal("expected customer_lookup in definitions")
	}
}
