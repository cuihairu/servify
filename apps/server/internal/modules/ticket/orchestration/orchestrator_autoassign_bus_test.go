package orchestration

// 自动派单的事件回路（无 bus 装配不订阅、非 created 事件与零 ID 事件直接
// 吞掉、created 事件触发派单）与 selectAutoAssignee 未装配守卫。

import (
	"context"
	"strings"
	"testing"

	"servify/apps/server/internal/models"
	ticketapp "servify/apps/server/internal/modules/ticket/application"
	"servify/apps/server/internal/platform/eventbus"

	"github.com/sirupsen/logrus"
)

func TestSubscribeCreatedForAutoAssignGuards(t *testing.T) {
	var nilOrchestrator *TicketOrchestrator
	nilOrchestrator.subscribeCreatedForAutoAssign() // nil 接收者不 panic

	// bus 未装配：跳过订阅，不订阅不派单。
	NewTicketOrchestrator(nil, logrus.New(), nil, nil, nil, nil, nil, nil, nil, nil, nil, nil).
		subscribeCreatedForAutoAssign()
}

func TestSubscribeCreatedForAutoAssignDispatchesCreatedEvents(t *testing.T) {
	bus := eventbus.NewInMemoryBus()
	var assigned []uint
	NewTicketOrchestrator(nil, logrus.New(), nil, nil, bus, nil,
		func(ctx context.Context) (*models.Agent, error) { return &models.Agent{UserID: 42}, nil },
		nil, nil, nil,
		func(ctx context.Context, ticketID, agentUserID, operatorID uint) error {
			assigned = append(assigned, ticketID)
			return nil
		},
		nil)
	ctx := context.Background()

	// 非 TicketEvent / TicketID=0：handler 吞掉，不派单。
	if err := bus.Publish(ctx, eventbus.BaseEvent{EventName: "other.event"}); err != nil {
		t.Fatalf("publish non-ticket event: %v", err)
	}
	if err := bus.Publish(ctx, ticketapp.NewTicketEvent(ctx, ticketapp.TicketCreatedEventName, ticketapp.TicketDTO{ID: 0})); err != nil {
		t.Fatalf("publish zero-id event: %v", err)
	}
	if len(assigned) != 0 {
		t.Fatalf("non-created events must not dispatch, got %v", assigned)
	}

	// 合法 created 事件：触发自动派单。
	if err := bus.Publish(ctx, ticketapp.NewTicketEvent(ctx, ticketapp.TicketCreatedEventName, ticketapp.TicketDTO{ID: 7})); err != nil {
		t.Fatalf("publish created event: %v", err)
	}
	if len(assigned) != 1 || assigned[0] != 7 {
		t.Fatalf("created event must auto-assign ticket 7, got %v", assigned)
	}
}

func TestSelectAutoAssigneeWithoutConfigErrors(t *testing.T) {
	orchestrator := &TicketOrchestrator{}
	agent, err := orchestrator.selectAutoAssignee(context.Background())
	if err == nil || !strings.Contains(err.Error(), "no db or findAutoAssignee configured") {
		t.Fatalf("expected configuration error, got agent=%v err=%v", agent, err)
	}
}
