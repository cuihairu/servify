package domain

import "testing"

func TestTriggerEntityFields(t *testing.T) {
	cond := Condition{Field: "ticket.status", Op: "eq", Value: "open"}
	act := Action{Type: "set_priority", Params: map[string]interface{}{"priority": "high"}}
	trig := Trigger{
		ID:         1,
		Name:       "raise",
		Event:      "ticket.updated",
		Conditions: []Condition{cond},
		Actions:    []Action{act},
		Active:     true,
	}
	if trig.ID != 1 || trig.Name != "raise" || trig.Event != "ticket.updated" || !trig.Active {
		t.Fatalf("unexpected trigger: %+v", trig)
	}
	if len(trig.Conditions) != 1 || trig.Conditions[0] != cond {
		t.Fatalf("unexpected conditions: %+v", trig.Conditions)
	}
	if len(trig.Actions) != 1 || trig.Actions[0].Type != "set_priority" || trig.Actions[0].Params["priority"] != "high" {
		t.Fatalf("unexpected actions: %+v", trig.Actions)
	}
}

func TestExecutionEntityFields(t *testing.T) {
	exec := Execution{TriggerID: 1, TicketID: 2, Status: "success", Message: ""}
	if exec.TriggerID != 1 || exec.TicketID != 2 || exec.Status != "success" || exec.Message != "" {
		t.Fatalf("unexpected execution: %+v", exec)
	}
}
