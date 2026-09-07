package delivery

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	voiceapp "servify/apps/server/internal/modules/voice/application"
	voiceinfra "servify/apps/server/internal/modules/voice/infra"
	voiceprovidermock "servify/apps/server/internal/modules/voice/provider/mock"
	"servify/apps/server/internal/platform/eventbus"
	"servify/apps/server/internal/platform/voiceprotocol"
)

type coordinatorTestBus struct{}

func (b *coordinatorTestBus) Publish(ctx context.Context, event eventbus.Event) error {
	return nil
}

type coordinatorHarness struct {
	coord       *Coordinator
	calls       *voiceinfra.InMemoryRepository
	recordings  *voiceinfra.InMemoryRecordingRepository
	transcripts *voiceinfra.InMemoryTranscriptRepository
}

func newCoordinatorHarness() *coordinatorHarness {
	bus := &coordinatorTestBus{}
	calls := voiceinfra.NewInMemoryRepository()
	recordings := voiceinfra.NewInMemoryRecordingRepository()
	transcripts := voiceinfra.NewInMemoryTranscriptRepository()
	coord := NewCoordinator(
		voiceapp.NewService(calls, bus),
		voiceapp.NewRecordingService(voiceprovidermock.NewRecordingProvider(), recordings, bus),
		voiceapp.NewTranscriptService(voiceprovidermock.NewTranscriptProvider(), transcripts, bus),
	)
	return &coordinatorHarness{coord: coord, calls: calls, recordings: recordings, transcripts: transcripts}
}

func TestCoordinatorStartAnswerEndCall(t *testing.T) {
	h := newCoordinatorHarness()
	ctx := context.Background()

	h.coord.StartCall(ctx, "session-1", "conn-1")
	call, ok := h.calls.GetCall("conn-1")
	require.True(t, ok, "call should be tracked by connection id")
	assert.Equal(t, "started", call.Status)
	assert.Equal(t, "session-1", call.SessionID)

	h.coord.AnswerCall(ctx, "conn-1")
	call, ok = h.calls.GetCall("conn-1")
	require.True(t, ok)
	assert.Equal(t, "answered", call.Status)

	h.coord.EndCall(ctx, "conn-1")
	call, ok = h.calls.GetCall("conn-1")
	require.True(t, ok)
	assert.Equal(t, "ended", call.Status)
}

func TestCoordinatorNilAndPartialSafety(t *testing.T) {
	ctx := context.Background()

	var nilCoord *Coordinator
	nilCoord.StartCall(ctx, "s", "c")
	nilCoord.AnswerCall(ctx, "c")
	nilCoord.EndCall(ctx, "c")
	require.Error(t, nilCoord.HandleCallEvent(ctx, voiceprotocol.CallEvent{Kind: voiceprotocol.CallEventInvite}))
	require.Error(t, nilCoord.HandleMediaEvent(ctx, voiceprotocol.MediaEvent{Kind: voiceprotocol.MediaEventRecordingStart}))

	rec, err := nilCoord.StartRecording(ctx, voiceapp.StartRecordingCommand{})
	require.NoError(t, err)
	assert.Nil(t, rec)
	require.NoError(t, nilCoord.StopRecording(ctx, voiceapp.StopRecordingCommand{}))
	tr, err := nilCoord.AppendTranscript(ctx, voiceapp.AppendTranscriptCommand{})
	require.NoError(t, err)
	assert.Nil(t, tr)
	got, err := nilCoord.GetRecording(ctx, "rec-1")
	require.NoError(t, err)
	assert.Nil(t, got)
	list, err := nilCoord.ListTranscripts(ctx, "c1")
	require.NoError(t, err)
	assert.Nil(t, list)
	all, total, err := nilCoord.ListAllTranscripts(ctx, 1, 10)
	require.NoError(t, err)
	assert.Nil(t, all)
	assert.Zero(t, total)

	partial := &Coordinator{}
	partial.StartCall(ctx, "s", "c")
	partial.AnswerCall(ctx, "c")
	partial.EndCall(ctx, "c")
	require.Error(t, partial.HandleCallEvent(ctx, voiceprotocol.CallEvent{Kind: voiceprotocol.CallEventInvite}))
	require.NoError(t, partial.HandleMediaEvent(ctx, voiceprotocol.MediaEvent{Kind: voiceprotocol.MediaEventRecordingStart, CallID: "c1"}))
	require.NoError(t, partial.HandleMediaEvent(ctx, voiceprotocol.MediaEvent{Kind: voiceprotocol.MediaEventRecordingStop, CallID: "c1"}))
	rec, err = partial.StartRecording(ctx, voiceapp.StartRecordingCommand{})
	require.NoError(t, err)
	assert.Nil(t, rec)
	require.NoError(t, partial.StopRecording(ctx, voiceapp.StopRecordingCommand{}))
	tr, err = partial.AppendTranscript(ctx, voiceapp.AppendTranscriptCommand{})
	require.NoError(t, err)
	assert.Nil(t, tr)
	got, err = partial.GetRecording(ctx, "rec-1")
	require.NoError(t, err)
	assert.Nil(t, got)
	list, err = partial.ListTranscripts(ctx, "c1")
	require.NoError(t, err)
	assert.Nil(t, list)
	all, total, err = partial.ListAllTranscripts(ctx, 1, 10)
	require.NoError(t, err)
	assert.Nil(t, all)
	assert.Zero(t, total)
}

func TestCoordinatorHandleCallEventLifecycle(t *testing.T) {
	h := newCoordinatorHarness()
	ctx := context.Background()

	require.NoError(t, h.coord.HandleCallEvent(ctx, voiceprotocol.CallEvent{
		Kind:           voiceprotocol.CallEventInvite,
		CallID:         "call-a",
		ConversationID: "conv-a",
		ConnectionID:   "conn-a",
	}))
	call, ok := h.calls.GetCall("call-a")
	require.True(t, ok)
	assert.Equal(t, "started", call.Status)
	assert.Equal(t, "conv-a", call.SessionID)

	require.NoError(t, h.coord.HandleCallEvent(ctx, voiceprotocol.CallEvent{Kind: voiceprotocol.CallEventAnswer, CallID: "call-a"}))
	call, _ = h.calls.GetCall("call-a")
	assert.Equal(t, "answered", call.Status)

	require.NoError(t, h.coord.HandleCallEvent(ctx, voiceprotocol.CallEvent{Kind: voiceprotocol.CallEventHold, CallID: "call-a"}))
	call, _ = h.calls.GetCall("call-a")
	assert.Equal(t, "held", call.Status)

	require.NoError(t, h.coord.HandleCallEvent(ctx, voiceprotocol.CallEvent{Kind: voiceprotocol.CallEventResume, CallID: "call-a"}))
	call, _ = h.calls.GetCall("call-a")
	assert.Equal(t, "answered", call.Status)

	require.NoError(t, h.coord.HandleCallEvent(ctx, voiceprotocol.CallEvent{Kind: voiceprotocol.CallEventHangup, CallID: "call-a"}))
	call, _ = h.calls.GetCall("call-a")
	assert.Equal(t, "ended", call.Status)
}

func TestCoordinatorHandleCallEventInviteSessionFallbacks(t *testing.T) {
	h := newCoordinatorHarness()
	ctx := context.Background()

	require.NoError(t, h.coord.HandleCallEvent(ctx, voiceprotocol.CallEvent{
		Kind:         voiceprotocol.CallEventInvite,
		CallID:       "call-b",
		ConnectionID: "conn-b",
	}))
	call, ok := h.calls.GetCall("call-b")
	require.True(t, ok)
	assert.Equal(t, "conn-b", call.SessionID)

	require.NoError(t, h.coord.HandleCallEvent(ctx, voiceprotocol.CallEvent{Kind: voiceprotocol.CallEventInvite, CallID: "call-c"}))
	call, ok = h.calls.GetCall("call-c")
	require.True(t, ok)
	assert.Equal(t, "call-c", call.SessionID)
}

func TestCoordinatorHandleCallEventTransfer(t *testing.T) {
	h := newCoordinatorHarness()
	ctx := context.Background()

	require.NoError(t, h.coord.HandleCallEvent(ctx, voiceprotocol.CallEvent{
		Kind:     voiceprotocol.CallEventInvite,
		CallID:   "call-t",
		Metadata: nil,
	}))
	require.NoError(t, h.coord.HandleCallEvent(ctx, voiceprotocol.CallEvent{
		Kind:     voiceprotocol.CallEventTransfer,
		CallID:   "call-t",
		Metadata: map[string]interface{}{"target_agent_id": float64(9)},
	}))
	call, ok := h.calls.GetCall("call-t")
	require.True(t, ok)
	assert.Equal(t, "transferred", call.Status)
	require.NotNil(t, call.TransferToAgent)
	assert.EqualValues(t, 9, *call.TransferToAgent)

	err := h.coord.HandleCallEvent(ctx, voiceprotocol.CallEvent{Kind: voiceprotocol.CallEventTransfer, CallID: "call-t"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "target_agent_id")
}

func TestCoordinatorHandleCallEventDTMFAndUnknown(t *testing.T) {
	h := newCoordinatorHarness()
	ctx := context.Background()

	require.NoError(t, h.coord.HandleCallEvent(ctx, voiceprotocol.CallEvent{Kind: voiceprotocol.CallEventDTMF, CallID: "call-d"}))

	err := h.coord.HandleCallEvent(ctx, voiceprotocol.CallEvent{Kind: voiceprotocol.CallEventKind("bogus"), CallID: "call-d"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported call event kind")
}

func TestCoordinatorHandleMediaEventRecording(t *testing.T) {
	h := newCoordinatorHarness()
	ctx := context.Background()

	require.NoError(t, h.coord.HandleMediaEvent(ctx, voiceprotocol.MediaEvent{
		Kind:     voiceprotocol.MediaEventRecordingStart,
		Protocol: voiceprotocol.ProtocolRTP,
		CallID:   "call-a",
	}))
	recording, err := h.recordings.FindByID(ctx, "rec-1")
	require.NoError(t, err)
	assert.Equal(t, "call-a", recording.CallID)
	assert.Equal(t, "recording", recording.Status)
	assert.Equal(t, "rtp", recording.Provider)

	require.NoError(t, h.coord.HandleMediaEvent(ctx, voiceprotocol.MediaEvent{
		Kind:     voiceprotocol.MediaEventRecordingStop,
		CallID:   "call-a",
		Metadata: map[string]interface{}{"recording_id": "rec-1"},
	}))
	recording, err = h.recordings.FindByID(ctx, "rec-1")
	require.NoError(t, err)
	assert.Equal(t, "stopped", recording.Status)

	// Stop events without a recording id are ignored.
	require.NoError(t, h.coord.HandleMediaEvent(ctx, voiceprotocol.MediaEvent{
		Kind:     voiceprotocol.MediaEventRecordingStart,
		Protocol: voiceprotocol.ProtocolWebRTC,
		CallID:   "call-a",
	}))
	recording, err = h.recordings.FindByID(ctx, "rec-2")
	require.NoError(t, err)
	assert.Equal(t, "recording", recording.Status)
	require.NoError(t, h.coord.HandleMediaEvent(ctx, voiceprotocol.MediaEvent{
		Kind:     voiceprotocol.MediaEventRecordingStop,
		CallID:   "call-a",
		Metadata: map[string]interface{}{"recording_id": 42},
	}))
	recording, err = h.recordings.FindByID(ctx, "rec-2")
	require.NoError(t, err)
	assert.Equal(t, "recording", recording.Status)

	// Unrelated media kinds are ignored.
	require.NoError(t, h.coord.HandleMediaEvent(ctx, voiceprotocol.MediaEvent{
		Kind:   voiceprotocol.MediaEventTrackMuted,
		CallID: "call-a",
	}))
}

func TestCoordinatorRecordingAndTranscriptQueries(t *testing.T) {
	h := newCoordinatorHarness()
	ctx := context.Background()

	rec, err := h.coord.StartRecording(ctx, voiceapp.StartRecordingCommand{CallID: "call-q", Provider: "mock"})
	require.NoError(t, err)
	require.NotNil(t, rec)

	got, err := h.coord.GetRecording(ctx, rec.ID)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, rec.ID, got.ID)

	_, err = h.coord.GetRecording(ctx, "missing-rec")
	require.Error(t, err)

	require.NoError(t, h.coord.StopRecording(ctx, voiceapp.StopRecordingCommand{RecordingID: rec.ID}))
	stopped, err := h.coord.GetRecording(ctx, rec.ID)
	require.NoError(t, err)
	assert.Equal(t, "stopped", stopped.Status)

	tr1, err := h.coord.AppendTranscript(ctx, voiceapp.AppendTranscriptCommand{CallID: "call-q", Content: "one", Language: "en"})
	require.NoError(t, err)
	assert.Equal(t, "one", tr1.Content)
	_, err = h.coord.AppendTranscript(ctx, voiceapp.AppendTranscriptCommand{CallID: "call-q", Content: "two", Language: "en"})
	require.NoError(t, err)

	list, err := h.coord.ListTranscripts(ctx, "call-q")
	require.NoError(t, err)
	assert.Len(t, list, 2)

	all, total, err := h.coord.ListAllTranscripts(ctx, 1, 10)
	require.NoError(t, err)
	assert.EqualValues(t, 2, total)
	assert.Len(t, all, 2)

	page, total, err := h.coord.ListAllTranscripts(ctx, 2, 1)
	require.NoError(t, err)
	assert.EqualValues(t, 2, total)
	assert.Len(t, page, 1)
}

func TestFirstNonEmpty(t *testing.T) {
	assert.Equal(t, "a", firstNonEmpty("", "a", "b"))
	assert.Equal(t, "a", firstNonEmpty("a", "", "b"))
	assert.Equal(t, "", firstNonEmpty())
	assert.Equal(t, "", firstNonEmpty("", ""))
}

func TestTargetAgentIDFromMetadata(t *testing.T) {
	cases := []struct {
		name     string
		metadata map[string]interface{}
		want     uint
		wantErr  bool
	}{
		{name: "empty metadata", metadata: map[string]interface{}{}, wantErr: true},
		{name: "nil metadata", metadata: nil, wantErr: true},
		{name: "missing key", metadata: map[string]interface{}{"other": 1}, wantErr: true},
		{name: "uint value", metadata: map[string]interface{}{"target_agent_id": uint(3)}, want: 3},
		{name: "positive int", metadata: map[string]interface{}{"target_agent_id": 4}, want: 4},
		{name: "negative int", metadata: map[string]interface{}{"target_agent_id": -4}, wantErr: true},
		{name: "positive int64", metadata: map[string]interface{}{"target_agent_id": int64(5)}, want: 5},
		{name: "negative int64", metadata: map[string]interface{}{"target_agent_id": int64(-5)}, wantErr: true},
		{name: "positive float", metadata: map[string]interface{}{"target_agent_id": 6.0}, want: 6},
		{name: "negative float", metadata: map[string]interface{}{"target_agent_id": -6.5}, wantErr: true},
		{name: "numeric string", metadata: map[string]interface{}{"target_agent_id": "7"}, want: 7},
		{name: "invalid string", metadata: map[string]interface{}{"target_agent_id": "seven"}, wantErr: true},
		{name: "unsupported type", metadata: map[string]interface{}{"target_agent_id": true}, wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := targetAgentIDFromMetadata(tc.metadata)
			if tc.wantErr {
				require.Error(t, err)
				assert.Zero(t, got)
				return
			}
			require.NoError(t, err)
			assert.EqualValues(t, tc.want, got)
		})
	}
}
