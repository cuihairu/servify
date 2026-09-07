package sipws

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"servify/apps/server/internal/platform/voiceprotocol"
)

func TestAdapterName(t *testing.T) {
	adapter := NewAdapter()
	assert.Equal(t, "sip-ws", adapter.Name())
}

func TestAdapterProtocol(t *testing.T) {
	adapter := NewAdapter()
	assert.Equal(t, voiceprotocol.ProtocolSIPWebSocket, adapter.Protocol())
}

func TestAdapterMapInvite(t *testing.T) {
	adapter := NewAdapter()
	event, err := adapter.MapInvite(context.Background(), SignalingMessage{
		CallID: "call-1",
		Method: "INVITE",
		From:   "alice",
		To:     "bob",
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if event.Protocol != voiceprotocol.ProtocolSIPWebSocket || event.Kind != voiceprotocol.CallEventInvite {
		t.Fatalf("unexpected event: %+v", event)
	}
}

func TestAdapterMapKinds(t *testing.T) {
	adapter := NewAdapter()
	msg := SignalingMessage{
		CallID:         "call-9",
		ConversationID: "conv-9",
		ConnectionID:   "conn-9",
		From:           "alice",
		To:             "bob",
		Method:         "INFO",
		OccurredAt:     time.Date(2024, 5, 1, 10, 30, 0, 0, time.UTC),
	}

	cases := []struct {
		name     string
		call     func() (voiceprotocol.CallEvent, error)
		kind     voiceprotocol.CallEventKind
		eventIDS string
	}{
		{"invite", func() (voiceprotocol.CallEvent, error) {
			return adapter.MapInvite(context.Background(), msg)
		}, voiceprotocol.CallEventInvite, "sipws-invite-call-9"},
		{"answer", func() (voiceprotocol.CallEvent, error) {
			return adapter.MapAnswer(context.Background(), msg)
		}, voiceprotocol.CallEventAnswer, "sipws-answer-call-9"},
		{"hold", func() (voiceprotocol.CallEvent, error) {
			return adapter.MapHold(context.Background(), msg)
		}, voiceprotocol.CallEventHold, "sipws-hold-call-9"},
		{"resume", func() (voiceprotocol.CallEvent, error) {
			return adapter.MapResume(context.Background(), msg)
		}, voiceprotocol.CallEventResume, "sipws-resume-call-9"},
		{"hangup", func() (voiceprotocol.CallEvent, error) {
			return adapter.MapHangup(context.Background(), msg)
		}, voiceprotocol.CallEventHangup, "sipws-hangup-call-9"},
		{"transfer", func() (voiceprotocol.CallEvent, error) {
			return adapter.MapTransfer(context.Background(), msg)
		}, voiceprotocol.CallEventTransfer, "sipws-transfer-call-9"},
		{"dtmf", func() (voiceprotocol.CallEvent, error) {
			return adapter.MapDTMF(context.Background(), msg)
		}, voiceprotocol.CallEventDTMF, "sipws-dtmf-call-9"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			event, err := tc.call()
			require.NoError(t, err)
			assert.Equal(t, voiceprotocol.ProtocolSIPWebSocket, event.Protocol)
			assert.Equal(t, tc.kind, event.Kind)
			assert.Equal(t, tc.eventIDS, event.EventID)
			assert.Equal(t, msg.CallID, event.CallID)
			assert.Equal(t, msg.ConversationID, event.ConversationID)
			assert.Equal(t, msg.ConnectionID, event.ConnectionID)
			assert.Equal(t, msg.From, event.From)
			assert.Equal(t, msg.To, event.To)
			assert.Equal(t, msg.OccurredAt, event.OccurredAt)
		})
	}
}

func TestAdapterMapErrorPayloads(t *testing.T) {
	adapter := NewAdapter()
	ctx := context.Background()

	calls := map[string]func(interface{}) (voiceprotocol.CallEvent, error){
		"invite":   func(p interface{}) (voiceprotocol.CallEvent, error) { return adapter.MapInvite(ctx, p) },
		"answer":   func(p interface{}) (voiceprotocol.CallEvent, error) { return adapter.MapAnswer(ctx, p) },
		"hold":     func(p interface{}) (voiceprotocol.CallEvent, error) { return adapter.MapHold(ctx, p) },
		"resume":   func(p interface{}) (voiceprotocol.CallEvent, error) { return adapter.MapResume(ctx, p) },
		"hangup":   func(p interface{}) (voiceprotocol.CallEvent, error) { return adapter.MapHangup(ctx, p) },
		"transfer": func(p interface{}) (voiceprotocol.CallEvent, error) { return adapter.MapTransfer(ctx, p) },
		"dtmf":     func(p interface{}) (voiceprotocol.CallEvent, error) { return adapter.MapDTMF(ctx, p) },
	}

	for name, call := range calls {
		t.Run(name, func(t *testing.T) {
			event, err := call(42)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "unsupported sip-ws payload type")
			assert.Equal(t, voiceprotocol.CallEvent{}, event)
		})
	}
}

func TestAsMessageFromSignalingMessage(t *testing.T) {
	msg := SignalingMessage{CallID: "call-2", From: "alice", To: "bob"}
	got, err := asMessage(msg)
	require.NoError(t, err)
	assert.Equal(t, msg, got)
}

func TestAsMessageFromMap(t *testing.T) {
	payload := map[string]interface{}{
		"call_id":         "call-3",
		"conversation_id": "conv-3",
		"connection_id":   "conn-3",
		"from":            "alice",
		"to":              "bob",
		"method":          "INVITE",
		"dtmf":            "1",
		"occurred_at":     "2024-05-01T10:30:00Z",
		"metadata":        map[string]interface{}{"custom": "value"},
	}
	got, err := asMessage(payload)
	require.NoError(t, err)
	assert.Equal(t, "call-3", got.CallID)
	assert.Equal(t, "conv-3", got.ConversationID)
	assert.Equal(t, "conn-3", got.ConnectionID)
	assert.Equal(t, "alice", got.From)
	assert.Equal(t, "bob", got.To)
	assert.Equal(t, "INVITE", got.Method)
	assert.Equal(t, "1", got.DTMF)
	assert.Equal(t, time.Date(2024, 5, 1, 10, 30, 0, 0, time.UTC), got.OccurredAt)
	assert.Equal(t, map[string]interface{}{"custom": "value"}, got.Metadata)
}

func TestAsMessageFromMapMarshalError(t *testing.T) {
	payload := map[string]interface{}{"bad": make(chan int)}
	_, err := asMessage(payload)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "marshal sip-ws payload")
}

func TestAsMessageFromMapDecodeError(t *testing.T) {
	payload := map[string]interface{}{"occurred_at": 12345}
	_, err := asMessage(payload)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "decode sip-ws payload")
}

func TestAsMessageUnsupportedType(t *testing.T) {
	_, err := asMessage("not-a-message")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported sip-ws payload type string")
}

func TestToCallEventDefaults(t *testing.T) {
	before := time.Now()
	event := toCallEvent(SignalingMessage{CallID: "call-4"}, voiceprotocol.CallEventInvite)
	after := time.Now()

	assert.False(t, event.OccurredAt.IsZero())
	assert.True(t, !event.OccurredAt.Before(before))
	assert.True(t, !event.OccurredAt.After(after))
	assert.Equal(t, "sipws-invite-call-4", event.EventID)
	assert.Equal(t, voiceprotocol.ProtocolSIPWebSocket, event.Protocol)
	assert.Equal(t, voiceprotocol.CallEventInvite, event.Kind)
	assert.Empty(t, event.Metadata)
}

func TestToCallEventMetadataAndMethod(t *testing.T) {
	msg := SignalingMessage{
		CallID:     "call-5",
		Method:     "INVITE",
		OccurredAt: time.Date(2024, 5, 1, 10, 30, 0, 0, time.UTC),
		Metadata:   map[string]interface{}{"reason": "test", "count": 2},
	}
	event := toCallEvent(msg, voiceprotocol.CallEventHangup)

	assert.Equal(t, msg.OccurredAt, event.OccurredAt)
	assert.Equal(t, map[string]interface{}{"reason": "test", "count": 2, "method": "INVITE"}, event.Metadata)
}

func TestToCallEventEmptyMethodOmitted(t *testing.T) {
	msg := SignalingMessage{
		CallID:   "call-6",
		Metadata: map[string]interface{}{"key": "value"},
	}
	event := toCallEvent(msg, voiceprotocol.CallEventTransfer)
	assert.Equal(t, map[string]interface{}{"key": "value"}, event.Metadata)
}

func TestAdapterMapDTMFDigits(t *testing.T) {
	adapter := NewAdapter()
	event, err := adapter.MapDTMF(context.Background(), SignalingMessage{
		CallID: "call-7",
		DTMF:   "123#",
	})
	require.NoError(t, err)
	assert.Equal(t, voiceprotocol.CallEventDTMF, event.Kind)
	assert.Equal(t, "123#", event.Metadata["digits"])
}

func TestAdapterMapAnswerFromMapPayload(t *testing.T) {
	adapter := NewAdapter()
	event, err := adapter.MapAnswer(context.Background(), map[string]interface{}{
		"call_id": "call-8",
		"from":    "alice",
		"to":      "bob",
		"method":  "200 OK",
	})
	require.NoError(t, err)
	assert.Equal(t, voiceprotocol.CallEventAnswer, event.Kind)
	assert.Equal(t, "call-8", event.CallID)
	assert.Equal(t, "alice", event.From)
	assert.Equal(t, "bob", event.To)
	assert.Equal(t, "200 OK", event.Metadata["method"])
}
