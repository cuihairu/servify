package application

import (
	"context"
	"fmt"
	"testing"

	"servify/apps/server/internal/platform/eventbus"
)

type stubRepo struct {
	call *CallDTO
}

func (s *stubRepo) StartCall(ctx context.Context, cmd StartCallCommand) (*CallDTO, error) {
	s.call = &CallDTO{ID: cmd.CallID, SessionID: cmd.SessionID, Status: "started"}
	return s.call, nil
}
func (s *stubRepo) AnswerCall(ctx context.Context, cmd AnswerCallCommand) (*CallDTO, error) {
	s.call.Status = "answered"
	return s.call, nil
}
func (s *stubRepo) HoldCall(ctx context.Context, cmd HoldCallCommand) (*CallDTO, error) {
	s.call.Status = "held"
	return s.call, nil
}
func (s *stubRepo) ResumeCall(ctx context.Context, cmd ResumeCallCommand) (*CallDTO, error) {
	s.call.Status = "answered"
	return s.call, nil
}
func (s *stubRepo) EndCall(ctx context.Context, cmd EndCallCommand) (*CallDTO, error) {
	s.call.Status = "ended"
	return s.call, nil
}
func (s *stubRepo) TransferCall(ctx context.Context, cmd TransferCallCommand) (*CallDTO, error) {
	s.call.Status = "transferred"
	s.call.TransferToAgent = &cmd.ToAgentID
	return s.call, nil
}
func (s *stubRepo) FindByID(ctx context.Context, callID string) (*CallDTO, error) {
	if s.call == nil || s.call.ID != callID {
		return nil, fmt.Errorf("call not found")
	}
	return s.call, nil
}

type stubBus struct{ events []eventbus.Event }

func (s *stubBus) Publish(ctx context.Context, event eventbus.Event) error {
	s.events = append(s.events, event)
	return nil
}

func TestStartAndEndCallPublishesEvents(t *testing.T) {
	repo := &stubRepo{}
	bus := &stubBus{}
	svc := NewService(repo, bus)
	call, err := svc.StartCall(context.Background(), StartCallCommand{CallID: "c1", SessionID: "s1"})
	if err != nil {
		t.Fatalf("StartCall() error = %v", err)
	}
	if call.Status != "started" {
		t.Fatalf("unexpected status: %s", call.Status)
	}
	if _, err := svc.EndCall(context.Background(), EndCallCommand{CallID: "c1"}); err != nil {
		t.Fatalf("EndCall() error = %v", err)
	}
	if len(bus.events) != 2 || bus.events[0].Name() != CallStartedEventName || bus.events[1].Name() != CallEndedEventName {
		t.Fatalf("unexpected events: %+v", bus.events)
	}
}

func TestHoldResumeTransferPublishEvents(t *testing.T) {
	repo := &stubRepo{}
	bus := &stubBus{}
	svc := NewService(repo, bus)
	if _, err := svc.StartCall(context.Background(), StartCallCommand{CallID: "c2", SessionID: "s2"}); err != nil {
		t.Fatalf("StartCall() error = %v", err)
	}
	if _, err := svc.HoldCall(context.Background(), HoldCallCommand{CallID: "c2"}); err != nil {
		t.Fatalf("HoldCall() error = %v", err)
	}
	if _, err := svc.ResumeCall(context.Background(), ResumeCallCommand{CallID: "c2"}); err != nil {
		t.Fatalf("ResumeCall() error = %v", err)
	}
	if _, err := svc.TransferCall(context.Background(), TransferCallCommand{CallID: "c2", ToAgentID: 7}); err != nil {
		t.Fatalf("TransferCall() error = %v", err)
	}
	if len(bus.events) != 4 {
		t.Fatalf("unexpected event count: %+v", bus.events)
	}
	if bus.events[1].Name() != CallHeldEventName || bus.events[2].Name() != CallResumedEventName || bus.events[3].Name() != CallTransferredName {
		t.Fatalf("unexpected events: %+v", bus.events)
	}
}
