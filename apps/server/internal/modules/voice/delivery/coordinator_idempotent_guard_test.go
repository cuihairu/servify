package delivery

import (
	"context"
	"testing"

	voiceapp "servify/apps/server/internal/modules/voice/application"
	"servify/apps/server/internal/platform/voiceprotocol"
)

// 已达成目标态的重复事件必须幂等短路：不重复执行动作、不重发事件。
func TestCoordinatorDuplicateStateEventsShortCircuit(t *testing.T) {
	bus := &capturingBus{}
	coord, calls := newIdempotencyCoordinator(bus)
	ctx := context.Background()

	if err := coord.HandleCallEvent(ctx, voiceprotocol.CallEvent{Kind: voiceprotocol.CallEventInvite, CallID: "CA-idem"}); err != nil {
		t.Fatalf("invite error = %v", err)
	}

	// answer 重复投递：第二次命中 answered 守卫
	if err := coord.HandleCallEvent(ctx, voiceprotocol.CallEvent{Kind: voiceprotocol.CallEventAnswer, CallID: "CA-idem"}); err != nil {
		t.Fatalf("answer error = %v", err)
	}
	if err := coord.HandleCallEvent(ctx, voiceprotocol.CallEvent{Kind: voiceprotocol.CallEventAnswer, CallID: "CA-idem"}); err != nil {
		t.Fatalf("duplicate answer error = %v", err)
	}

	// hold 重复投递：第二次命中 held 守卫
	if err := coord.HandleCallEvent(ctx, voiceprotocol.CallEvent{Kind: voiceprotocol.CallEventHold, CallID: "CA-idem"}); err != nil {
		t.Fatalf("hold error = %v", err)
	}
	if err := coord.HandleCallEvent(ctx, voiceprotocol.CallEvent{Kind: voiceprotocol.CallEventHold, CallID: "CA-idem"}); err != nil {
		t.Fatalf("duplicate hold error = %v", err)
	}

	// resume 重复投递：第二次命中 answered+ResumedAt 守卫
	if err := coord.HandleCallEvent(ctx, voiceprotocol.CallEvent{Kind: voiceprotocol.CallEventResume, CallID: "CA-idem"}); err != nil {
		t.Fatalf("resume error = %v", err)
	}
	if err := coord.HandleCallEvent(ctx, voiceprotocol.CallEvent{Kind: voiceprotocol.CallEventResume, CallID: "CA-idem"}); err != nil {
		t.Fatalf("duplicate resume error = %v", err)
	}

	// transfer 重复投递：第二次命中 transferred 守卫（合法事件才豁免）
	if err := coord.HandleCallEvent(ctx, voiceprotocol.CallEvent{
		Kind: voiceprotocol.CallEventTransfer, CallID: "CA-idem",
		Metadata: map[string]interface{}{"target_agent_id": 5},
	}); err != nil {
		t.Fatalf("transfer error = %v", err)
	}
	if err := coord.HandleCallEvent(ctx, voiceprotocol.CallEvent{
		Kind: voiceprotocol.CallEventTransfer, CallID: "CA-idem",
		Metadata: map[string]interface{}{"target_agent_id": 5},
	}); err != nil {
		t.Fatalf("duplicate transfer error = %v", err)
	}

	// 每种事件只广播一次（answer 不发事件）
	for name, want := range map[string]int{
		voiceapp.CallStartedEventName: 1,
		voiceapp.CallHeldEventName:    1,
		voiceapp.CallResumedEventName: 1,
		voiceapp.CallTransferredName:  1,
	} {
		if got := countEvents(bus.events, name); got != want {
			t.Fatalf("%s published %d times, want %d (events: %v)", name, got, want, bus.events)
		}
	}

	call, ok := calls.GetCall("CA-idem")
	if !ok {
		t.Fatal("call must be tracked")
	}
	if call.Status != "transferred" {
		t.Fatalf("final status = %s, want transferred", call.Status)
	}
}
