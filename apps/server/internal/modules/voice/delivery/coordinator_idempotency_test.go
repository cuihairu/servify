package delivery

import (
	"context"
	"testing"

	voiceapp "servify/apps/server/internal/modules/voice/application"
	voiceinfra "servify/apps/server/internal/modules/voice/infra"
	voiceprovidermock "servify/apps/server/internal/modules/voice/provider/mock"
	"servify/apps/server/internal/platform/eventbus"
	"servify/apps/server/internal/platform/voiceprotocol"
)

// capturingBus 记录发布的事件名,用于断言幂等短路不重复出站。
type capturingBus struct {
	events []string
}

func (b *capturingBus) Publish(ctx context.Context, event eventbus.Event) error {
	b.events = append(b.events, event.Name())
	return nil
}

func newIdempotencyCoordinator(bus *capturingBus) (*Coordinator, *voiceinfra.InMemoryRepository) {
	calls := voiceinfra.NewInMemoryRepository()
	recordings := voiceinfra.NewInMemoryRecordingRepository()
	coord := NewCoordinator(
		voiceapp.NewService(calls, bus),
		voiceapp.NewRecordingService(voiceprovidermock.NewRecordingProvider(), recordings, bus),
		voiceapp.NewTranscriptService(voiceprovidermock.NewTranscriptProvider(), voiceinfra.NewInMemoryTranscriptRepository(), bus),
	)
	return coord, calls
}

func countEvents(events []string, name string) int {
	n := 0
	for _, e := range events {
		if e == name {
			n++
		}
	}
	return n
}

// hosted provider 对无响应的 webhook 会重试:同一通电话的重复 invite 必须幂等,
// 不重复建记录、不重发 call.started(否则出站 webhook 也会重复投递)。
func TestCoordinatorDuplicateInviteIsIdempotent(t *testing.T) {
	bus := &capturingBus{}
	coord, calls := newIdempotencyCoordinator(bus)
	ctx := context.Background()

	invite := voiceprotocol.CallEvent{Kind: voiceprotocol.CallEventInvite, CallID: "CA-dup", From: "+100", To: "+200"}
	for i := 0; i < 3; i++ {
		if err := coord.HandleCallEvent(ctx, invite); err != nil {
			t.Fatalf("invite #%d error = %v", i, err)
		}
	}
	if got := countEvents(bus.events, voiceapp.CallStartedEventName); got != 1 {
		t.Fatalf("call.started published %d times, want 1 (events: %v)", got, bus.events)
	}
	if _, ok := calls.GetCall("CA-dup"); !ok {
		t.Fatal("call not persisted")
	}
}

// 已结束的通话再收到 completed 重试:短路且不重发 call.ended;
// 通话不存在时保持报错(provider 会重试,待 invite 落地后自愈)。
func TestCoordinatorDuplicateHangupIsIdempotent(t *testing.T) {
	bus := &capturingBus{}
	coord, _ := newIdempotencyCoordinator(bus)
	ctx := context.Background()

	if err := coord.HandleCallEvent(ctx, voiceprotocol.CallEvent{Kind: voiceprotocol.CallEventInvite, CallID: "CA-end"}); err != nil {
		t.Fatalf("invite error = %v", err)
	}
	end := voiceprotocol.CallEvent{Kind: voiceprotocol.CallEventHangup, CallID: "CA-end"}
	for i := 0; i < 2; i++ {
		if err := coord.HandleCallEvent(ctx, end); err != nil {
			t.Fatalf("hangup #%d error = %v", i, err)
		}
	}
	if got := countEvents(bus.events, voiceapp.CallEndedEventName); got != 1 {
		t.Fatalf("call.ended published %d times, want 1 (events: %v)", got, bus.events)
	}

	if err := coord.HandleCallEvent(ctx, voiceprotocol.CallEvent{Kind: voiceprotocol.CallEventHangup, CallID: "CA-ghost"}); err == nil {
		t.Fatal("hangup for unknown call should error")
	}
}

// 携带 storage_uri 的录音完成回调走 upsert 终态(回调可能先于本方 start 到达)。
func TestCoordinatorRecordingCompletedUpsertsStorageURI(t *testing.T) {
	bus := &capturingBus{}
	coord, _ := newIdempotencyCoordinator(bus)
	ctx := context.Background()

	event := voiceprotocol.MediaEvent{
		Kind:   voiceprotocol.MediaEventRecordingStop,
		CallID: "CA-rec",
		Metadata: map[string]interface{}{
			"recording_id": "RE-rec",
			"storage_uri":  "http://example.com/rec.mp3",
		},
	}
	for i := 0; i < 2; i++ {
		if err := coord.HandleMediaEvent(ctx, event); err != nil {
			t.Fatalf("recording completed #%d error = %v", i, err)
		}
	}
	rec, err := coord.GetRecording(ctx, "RE-rec")
	if err != nil || rec == nil {
		t.Fatalf("GetRecording() = %+v, %v", rec, err)
	}
	if rec.Status != "stopped" || rec.StorageURI != "http://example.com/rec.mp3" {
		t.Fatalf("unexpected recording: %+v", rec)
	}
	if got := countEvents(bus.events, voiceapp.RecordingStoppedEventName); got != 2 {
		t.Fatalf("recording.stopped published %d times, want 2 (每次回调都广播,幂等由 upsert 保证)", got)
	}
}

// 不带 storage_uri 的 recording_stopped 是本方 Stop 指令回执,维持原路径。
func TestCoordinatorRecordingStopWithoutURIKepsLegacyPath(t *testing.T) {
	bus := &capturingBus{}
	coord, _ := newIdempotencyCoordinator(bus)
	ctx := context.Background()

	started, err := coord.StartRecording(ctx, voiceapp.StartRecordingCommand{CallID: "CA-live", Provider: "mock"})
	if err != nil {
		t.Fatalf("StartRecording() error = %v", err)
	}
	if err := coord.HandleMediaEvent(ctx, voiceprotocol.MediaEvent{
		Kind:     voiceprotocol.MediaEventRecordingStop,
		CallID:   "CA-live",
		Metadata: map[string]interface{}{"recording_id": started.ID},
	}); err != nil {
		t.Fatalf("HandleMediaEvent() error = %v", err)
	}
	rec, err := coord.GetRecording(ctx, started.ID)
	if err != nil || rec == nil {
		t.Fatalf("GetRecording() = %+v, %v", rec, err)
	}
	if rec.Status != "stopped" {
		t.Fatalf("recording status = %s, want stopped", rec.Status)
	}
}
