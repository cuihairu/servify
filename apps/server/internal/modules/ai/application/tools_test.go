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

type stubCustomerLookupPort struct {
	calls []uint
}

func (s *stubCustomerLookupPort) GetCustomerSummary(ctx context.Context, customerID uint) (map[string]interface{}, error) {
	s.calls = append(s.calls, customerID)
	return map[string]interface{}{"customer_id": customerID}, nil
}

type stubTicketLookupPort struct {
	calls []uint
}

func (s *stubTicketLookupPort) GetTicketSummary(ctx context.Context, ticketID uint) (map[string]interface{}, error) {
	s.calls = append(s.calls, ticketID)
	return map[string]interface{}{"ticket_id": ticketID}, nil
}

func TestCustomerLookupToolVariants(t *testing.T) {
	ctx := context.Background()
	tool := NewCustomerLookupTool(&stubCustomerLookupPort{})

	if tool.Name() != "customer_lookup" || tool.Description() == "" || tool.Schema() == nil {
		t.Fatalf("unexpected tool metadata: %s", tool.Name())
	}

	summary, err := tool.Execute(ctx, map[string]interface{}{"customer_id": float64(7)})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if summary["customer_id"] != uint(7) {
		t.Fatalf("summary = %v", summary)
	}

	if _, err := NewCustomerLookupTool(nil).Execute(ctx, map[string]interface{}{"customer_id": 1}); err == nil {
		t.Fatal("nil port should fail")
	}
	if _, err := tool.Execute(ctx, map[string]interface{}{}); err == nil {
		t.Fatal("missing customer_id should fail")
	}
}

func TestGetUintInputVariants(t *testing.T) {
	cases := []struct {
		input map[string]interface{}
		want  uint
		ok    bool
	}{
		{map[string]interface{}{"id": 3}, 3, true},
		{map[string]interface{}{"id": int32(4)}, 4, true},
		{map[string]interface{}{"id": int64(5)}, 5, true},
		{map[string]interface{}{"id": float64(6)}, 6, true},
		{map[string]interface{}{"id": "7"}, 7, true},
		{map[string]interface{}{"id": "not-a-number"}, 0, false},
		{map[string]interface{}{"id": []int{1}}, 0, false},
	}
	for _, tc := range cases {
		got, err := getUintInput(tc.input, "id")
		if (err == nil) != tc.ok || got != tc.want {
			t.Fatalf("getUintInput(%v) = (%d, %v), want (%d, ok=%v)", tc.input, got, err, tc.want, tc.ok)
		}
	}
	if _, err := getUintInput(map[string]interface{}{}, "id"); err == nil {
		t.Fatal("missing key should fail")
	}
}

func TestTicketLookupToolVariants(t *testing.T) {
	ctx := context.Background()
	port := &stubTicketLookupPort{}
	tool := NewTicketLookupTool(port)

	if tool.Name() != "ticket_lookup" || tool.Description() == "" || tool.Schema() == nil {
		t.Fatalf("unexpected tool metadata: %s", tool.Name())
	}

	if _, err := tool.Execute(ctx, map[string]interface{}{"ticket_id": "12"}); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if len(port.calls) != 1 || port.calls[0] != 12 {
		t.Fatalf("port calls = %v", port.calls)
	}
	if _, err := NewTicketLookupTool(nil).Execute(ctx, map[string]interface{}{"ticket_id": 1}); err == nil {
		t.Fatal("nil port should fail")
	}
	if _, err := tool.Execute(ctx, map[string]interface{}{"ticket_id": true}); err == nil {
		t.Fatal("invalid ticket_id should fail")
	}
}

func TestHandoffToolMetadataAndErrors(t *testing.T) {
	ctx := context.Background()
	tool := NewHandoffTool(&stubHandoff{})

	if tool.Name() != "handoff" || tool.Description() == "" || tool.Schema() == nil {
		t.Fatalf("unexpected tool metadata: %s", tool.Name())
	}
	if _, err := NewHandoffTool(nil).Execute(ctx, map[string]interface{}{"conversation_id": "c1"}); err == nil {
		t.Fatal("nil port should fail")
	}
	if _, err := tool.Execute(ctx, map[string]interface{}{}); err == nil {
		t.Fatal("missing conversation_id should fail")
	}
	if _, err := tool.Execute(ctx, map[string]interface{}{"conversation_id": 42}); err == nil {
		t.Fatal("non-string conversation_id should fail")
	}
}

func TestToolRegistryRegisterInvalidTool(t *testing.T) {
	r := NewToolRegistry()
	r.Register(nil)
	if _, ok := r.Get("missing"); ok {
		t.Fatal("nil tool should not be registered")
	}
	if tools := r.List(); len(tools) != 0 {
		t.Fatalf("expected empty registry, got %d", len(tools))
	}

	exec := NewToolExecutor(nil, nil)
	if defs := exec.Definitions(); len(defs) != 0 {
		t.Fatalf("expected no definitions, got %d", len(defs))
	}
}

func TestToolExecutorUnknownTool(t *testing.T) {
	exec := NewToolExecutor(nil, nil)
	if _, err := exec.Execute(context.Background(), AIRequest{ToolPolicy: ToolPolicy{Enabled: true}}, "nope", nil); err == nil {
		t.Fatal("unknown tool should fail")
	}
}

func TestToolExecutorPermissionCheckerError(t *testing.T) {
	registry := NewToolRegistry()
	registry.Register(NewHandoffTool(&stubHandoff{}))
	exec := NewToolExecutor(registry, func(req AIRequest, tool Tool) error {
		return fmt.Errorf("denied by policy")
	})
	_, err := exec.Execute(context.Background(), AIRequest{ToolPolicy: ToolPolicy{Enabled: true}}, "handoff", map[string]interface{}{"conversation_id": "c1"})
	if err == nil || err.Error() != "denied by policy" {
		t.Fatalf("expected permission denial, got %v", err)
	}
}
