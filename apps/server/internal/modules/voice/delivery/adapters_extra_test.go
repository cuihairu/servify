package delivery

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	voiceapp "servify/apps/server/internal/modules/voice/application"
	voiceinfra "servify/apps/server/internal/modules/voice/infra"
	"servify/apps/server/internal/platform/voiceprotocol"
)

func requireMediaEvent(t *testing.T, event voiceprotocol.MediaEvent, err error, protocol voiceprotocol.Protocol, kind voiceprotocol.MediaEventKind, connectionID string) {
	t.Helper()
	require.NoError(t, err)
	assert.Equal(t, protocol, event.Protocol)
	assert.Equal(t, kind, event.Kind)
	assert.Equal(t, connectionID, event.ConnectionID)
	assert.False(t, event.OccurredAt.IsZero(), "occurred at should default to now when zero")
}

func TestRTPAdapterMetadata(t *testing.T) {
	adapter := NewRTPAdapter()
	assert.Equal(t, "rtp", adapter.Name())
	assert.Equal(t, voiceprotocol.ProtocolRTP, adapter.Protocol())
}

func TestRTPAdapterMapSessionClosed(t *testing.T) {
	adapter := NewRTPAdapter()
	occurredAt := time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC)
	event, err := adapter.MapSessionClosed(context.Background(), PacketMediaPayload{
		CallID:       "call-1",
		ConnectionID: "rtp-conn-1",
		OccurredAt:   occurredAt,
	})
	requireMediaEvent(t, event, err, voiceprotocol.ProtocolRTP, voiceprotocol.MediaEventSessionClosed, "rtp-conn-1")
	assert.Equal(t, "call-1", event.CallID)
	assert.True(t, event.OccurredAt.Equal(occurredAt))
	assert.Equal(t, "rtp-session_closed-rtp-conn-1", event.EventID)
}

func TestRTPAdapterMapTrackEventsFromMaps(t *testing.T) {
	adapter := NewRTPAdapter()
	ctx := context.Background()
	payload := map[string]interface{}{
		"call_id":       "call-1",
		"connection_id": "rtp-conn-1",
	}

	muted, err := adapter.MapTrackMuted(ctx, payload)
	requireMediaEvent(t, muted, err, voiceprotocol.ProtocolRTP, voiceprotocol.MediaEventTrackMuted, "rtp-conn-1")

	unmuted, err := adapter.MapTrackUnmuted(ctx, payload)
	requireMediaEvent(t, unmuted, err, voiceprotocol.ProtocolRTP, voiceprotocol.MediaEventTrackUnmuted, "rtp-conn-1")

	started, err := adapter.MapRecordingStarted(ctx, payload)
	requireMediaEvent(t, started, err, voiceprotocol.ProtocolRTP, voiceprotocol.MediaEventRecordingStart, "rtp-conn-1")

	stopped, err := adapter.MapRecordingStopped(ctx, payload)
	requireMediaEvent(t, stopped, err, voiceprotocol.ProtocolRTP, voiceprotocol.MediaEventRecordingStop, "rtp-conn-1")
}

func TestRTPAdapterMapSessionStartedWithoutOccurredAt(t *testing.T) {
	adapter := NewRTPAdapter()
	event, err := adapter.MapSessionStarted(context.Background(), map[string]interface{}{
		"call_id":         "call-1",
		"conversation_id": "conv-1",
		"connection_id":   "rtp-conn-1",
	})
	requireMediaEvent(t, event, err, voiceprotocol.ProtocolRTP, voiceprotocol.MediaEventSessionStarted, "rtp-conn-1")
	assert.Equal(t, "conv-1", event.ConversationID)
}

func TestSRTPAdapterMetadata(t *testing.T) {
	adapter := NewSRTPAdapter()
	assert.Equal(t, "srtp", adapter.Name())
	assert.Equal(t, voiceprotocol.ProtocolSRTP, adapter.Protocol())
}

func TestSRTPAdapterMapEvents(t *testing.T) {
	adapter := NewSRTPAdapter()
	ctx := context.Background()
	occurredAt := time.Date(2025, 6, 7, 8, 9, 10, 0, time.UTC)
	structPayload := PacketMediaPayload{
		CallID:         "call-s",
		ConversationID: "conv-s",
		ConnectionID:   "srtp-conn-1",
		OccurredAt:     occurredAt,
		Metadata:       map[string]interface{}{"codec": "opus"},
	}
	mapPayload := map[string]interface{}{"call_id": "call-s", "connection_id": "srtp-conn-1"}

	closed, err := adapter.MapSessionClosed(ctx, structPayload)
	requireMediaEvent(t, closed, err, voiceprotocol.ProtocolSRTP, voiceprotocol.MediaEventSessionClosed, "srtp-conn-1")
	assert.Equal(t, "conv-s", closed.ConversationID)
	assert.Equal(t, map[string]interface{}{"codec": "opus"}, closed.Metadata)
	assert.Equal(t, "srtp-session_closed-srtp-conn-1", closed.EventID)

	muted, err := adapter.MapTrackMuted(ctx, mapPayload)
	requireMediaEvent(t, muted, err, voiceprotocol.ProtocolSRTP, voiceprotocol.MediaEventTrackMuted, "srtp-conn-1")

	unmuted, err := adapter.MapTrackUnmuted(ctx, mapPayload)
	requireMediaEvent(t, unmuted, err, voiceprotocol.ProtocolSRTP, voiceprotocol.MediaEventTrackUnmuted, "srtp-conn-1")

	started, err := adapter.MapRecordingStarted(ctx, mapPayload)
	requireMediaEvent(t, started, err, voiceprotocol.ProtocolSRTP, voiceprotocol.MediaEventRecordingStart, "srtp-conn-1")

	stopped, err := adapter.MapRecordingStopped(ctx, mapPayload)
	requireMediaEvent(t, stopped, err, voiceprotocol.ProtocolSRTP, voiceprotocol.MediaEventRecordingStop, "srtp-conn-1")
}

func TestPacketAdapterRejectsUnsupportedPayload(t *testing.T) {
	adapter := NewRTPAdapter()
	_, err := adapter.MapSessionClosed(context.Background(), 12345)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported packet media payload type")
}

func TestPacketAdapterMarshalError(t *testing.T) {
	adapter := NewRTPAdapter()
	_, err := adapter.MapTrackMuted(context.Background(), map[string]interface{}{
		"call_id": "call-1",
		"bad":     make(chan int),
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "marshal packet media payload")
}

func TestPacketAdapterDecodeError(t *testing.T) {
	adapter := NewRTPAdapter()
	_, err := adapter.MapTrackUnmuted(context.Background(), map[string]interface{}{
		"call_id":     "call-1",
		"occurred_at": "definitely-not-a-timestamp",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "decode packet media payload")
}

func TestWebRTCAdapterMetadata(t *testing.T) {
	adapter := NewWebRTCAdapter(nil)
	assert.Equal(t, "webrtc", adapter.Name())
	assert.Equal(t, voiceprotocol.ProtocolWebRTC, adapter.Protocol())
}

func TestWebRTCAdapterCallLifecycle(t *testing.T) {
	repo := voiceinfra.NewInMemoryRepository()
	adapter := NewWebRTCAdapter(voiceapp.NewService(repo, nil))
	ctx := context.Background()

	adapter.StartCall(ctx, "session-1", "conn-1")
	call, ok := repo.GetCall("conn-1")
	require.True(t, ok)
	assert.Equal(t, "started", call.Status)

	adapter.AnswerCall(ctx, "conn-1")
	call, _ = repo.GetCall("conn-1")
	assert.Equal(t, "answered", call.Status)

	adapter.EndCall(ctx, "conn-1")
	call, _ = repo.GetCall("conn-1")
	assert.Equal(t, "ended", call.Status)
}

func TestWebRTCAdapterCallLifecycleWithoutService(t *testing.T) {
	adapter := NewWebRTCAdapter(nil)
	ctx := context.Background()
	adapter.StartCall(ctx, "session-1", "conn-1")
	adapter.AnswerCall(ctx, "conn-1")
	adapter.EndCall(ctx, "conn-1")
}

func TestWebRTCAdapterMapEvents(t *testing.T) {
	adapter := NewWebRTCAdapter(nil)
	ctx := context.Background()
	structPayload := WebRTCMediaPayload{
		CallID:       "call-w",
		ConnectionID: "peer-1",
	}
	mapPayload := map[string]interface{}{"call_id": "call-w", "connection_id": "peer-1"}

	closed, err := adapter.MapSessionClosed(ctx, structPayload)
	requireMediaEvent(t, closed, err, voiceprotocol.ProtocolWebRTC, voiceprotocol.MediaEventSessionClosed, "peer-1")
	assert.Equal(t, "webrtc-session_closed-peer-1", closed.EventID)

	muted, err := adapter.MapTrackMuted(ctx, mapPayload)
	requireMediaEvent(t, muted, err, voiceprotocol.ProtocolWebRTC, voiceprotocol.MediaEventTrackMuted, "peer-1")

	unmuted, err := adapter.MapTrackUnmuted(ctx, mapPayload)
	requireMediaEvent(t, unmuted, err, voiceprotocol.ProtocolWebRTC, voiceprotocol.MediaEventTrackUnmuted, "peer-1")

	started, err := adapter.MapRecordingStarted(ctx, mapPayload)
	requireMediaEvent(t, started, err, voiceprotocol.ProtocolWebRTC, voiceprotocol.MediaEventRecordingStart, "peer-1")
	assert.Equal(t, "call-w", started.CallID)

	stopped, err := adapter.MapRecordingStopped(ctx, structPayload)
	requireMediaEvent(t, stopped, err, voiceprotocol.ProtocolWebRTC, voiceprotocol.MediaEventRecordingStop, "peer-1")
}

func TestWebRTCAdapterMarshalError(t *testing.T) {
	adapter := NewWebRTCAdapter(nil)
	_, err := adapter.MapSessionClosed(context.Background(), map[string]interface{}{
		"call_id": "call-w",
		"bad":     make(chan int),
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "marshal WebRTC media payload")
}

func TestWebRTCAdapterDecodeError(t *testing.T) {
	adapter := NewWebRTCAdapter(nil)
	_, err := adapter.MapRecordingStopped(context.Background(), map[string]interface{}{
		"call_id":     "call-w",
		"occurred_at": "not-a-timestamp",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "decode WebRTC media payload")
}
