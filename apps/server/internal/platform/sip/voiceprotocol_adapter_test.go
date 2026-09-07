package sip

import (
	"context"
	"testing"
	"time"

	"servify/apps/server/internal/platform/voiceprotocol"
)

func TestVoiceProtocolAdapterNameAndProtocol(t *testing.T) {
	adapter := NewVoiceProtocolAdapter()
	if adapter.Name() != ChannelName {
		t.Fatalf("expected name %q, got %q", ChannelName, adapter.Name())
	}
	if adapter.Protocol() != voiceprotocol.ProtocolSIP {
		t.Fatalf("expected protocol %q, got %q", voiceprotocol.ProtocolSIP, adapter.Protocol())
	}
}

func TestVoiceProtocolAdapterMapMethods(t *testing.T) {
	adapter := NewVoiceProtocolAdapter()
	now := time.Now().UTC().Truncate(time.Second)

	tests := []struct {
		name          string
		call          func(ctx context.Context, payload interface{}) (voiceprotocol.CallEvent, error)
		callID        string
		kind          voiceprotocol.CallEventKind
		eventIDDigits string
	}{
		{
			name:          "invite",
			call:          adapter.MapInvite,
			callID:        "call-invite",
			kind:          voiceprotocol.CallEventInvite,
			eventIDDigits: "invite",
		},
		{
			name:          "answer",
			call:          adapter.MapAnswer,
			callID:        "call-answer",
			kind:          voiceprotocol.CallEventAnswer,
			eventIDDigits: "answer",
		},
		{
			name:          "hold",
			call:          adapter.MapHold,
			callID:        "call-hold",
			kind:          voiceprotocol.CallEventHold,
			eventIDDigits: "hold",
		},
		{
			name:          "resume",
			call:          adapter.MapResume,
			callID:        "call-resume",
			kind:          voiceprotocol.CallEventResume,
			eventIDDigits: "resume",
		},
		{
			name:          "hangup",
			call:          adapter.MapHangup,
			callID:        "call-hangup",
			kind:          voiceprotocol.CallEventHangup,
			eventIDDigits: "hangup",
		},
		{
			name:          "transfer",
			call:          adapter.MapTransfer,
			callID:        "call-transfer",
			kind:          voiceprotocol.CallEventTransfer,
			eventIDDigits: "transfer",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			event, err := tc.call(context.Background(), InboundCall{
				CallID:         tc.callID,
				ConversationID: "conv-1",
				From:           "1001",
				To:             "2001",
				OccurredAt:     now,
				Metadata:       map[string]interface{}{"tenant_id": "t-1"},
			})
			if err != nil {
				t.Fatalf("expected no error, got %v", err)
			}
			if event.Protocol != voiceprotocol.ProtocolSIP {
				t.Fatalf("unexpected protocol: %+v", event)
			}
			if event.Kind != tc.kind {
				t.Fatalf("expected kind %q, got %q", tc.kind, event.Kind)
			}
			if event.EventID != "sip-"+tc.eventIDDigits+"-"+tc.callID {
				t.Fatalf("unexpected event id: %q", event.EventID)
			}
			if event.CallID != tc.callID || event.ConversationID != "conv-1" {
				t.Fatalf("unexpected ids: %+v", event)
			}
			if event.From != "1001" || event.To != "2001" {
				t.Fatalf("unexpected endpoints: %+v", event)
			}
			if !event.OccurredAt.Equal(now) {
				t.Fatalf("expected occurredAt %v, got %v", now, event.OccurredAt)
			}
			if event.Metadata["tenant_id"] != "t-1" {
				t.Fatalf("expected copied metadata, got %+v", event.Metadata)
			}
			if _, ok := event.Metadata["dtmf"]; ok {
				t.Fatalf("unexpected dtmf metadata for non-dtmf event: %+v", event.Metadata)
			}
		})
	}
}

func TestVoiceProtocolAdapterMapDTMF(t *testing.T) {
	adapter := NewVoiceProtocolAdapter()

	event, err := adapter.MapDTMF(context.Background(), InboundCall{
		CallID: "call-dtmf",
		From:   "1004",
		To:     "ivr",
		DTMF:   "5",
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if event.Kind != voiceprotocol.CallEventDTMF {
		t.Fatalf("expected dtmf kind, got %+v", event)
	}
	if event.EventID != "sip-dtmf-call-dtmf" {
		t.Fatalf("unexpected event id: %q", event.EventID)
	}
	if event.Metadata["digits"] != "5" || event.Metadata["dtmf"] != "5" {
		t.Fatalf("expected dtmf metadata, got %+v", event.Metadata)
	}
	if event.OccurredAt.IsZero() {
		t.Fatalf("expected default occurredAt to be filled, got %+v", event)
	}
}

func TestVoiceProtocolAdapterMapPayloadAsMap(t *testing.T) {
	adapter := NewVoiceProtocolAdapter()
	now := time.Now().UTC().Truncate(time.Second)

	event, err := adapter.MapInvite(context.Background(), map[string]interface{}{
		"call_id":         "call-map",
		"conversation_id": "conv-map",
		"from":            "1005",
		"to":              "2005",
		"occurred_at":     now.Format(time.RFC3339Nano),
		"metadata":        map[string]interface{}{"source": "webhook"},
		"dtmf":            "1",
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if event.CallID != "call-map" || event.ConversationID != "conv-map" {
		t.Fatalf("unexpected ids: %+v", event)
	}
	if event.From != "1005" || event.To != "2005" {
		t.Fatalf("unexpected endpoints: %+v", event)
	}
	if event.Metadata["source"] != "webhook" || event.Metadata["dtmf"] != "1" {
		t.Fatalf("unexpected metadata: %+v", event.Metadata)
	}
}

func TestVoiceProtocolAdapterMapMethodsRejectBadPayload(t *testing.T) {
	adapter := NewVoiceProtocolAdapter()

	mappers := map[string]func(ctx context.Context, payload interface{}) (voiceprotocol.CallEvent, error){
		"invite":   adapter.MapInvite,
		"answer":   adapter.MapAnswer,
		"hold":     adapter.MapHold,
		"resume":   adapter.MapResume,
		"hangup":   adapter.MapHangup,
		"transfer": adapter.MapTransfer,
		"dtmf":     adapter.MapDTMF,
	}

	for name, mapper := range mappers {
		t.Run(name, func(t *testing.T) {
			if _, err := mapper(context.Background(), 42); err == nil {
				t.Fatalf("expected payload type error for %s", name)
			}
		})
	}
}

func TestAsInboundCallMarshalError(t *testing.T) {
	cyclic := map[string]interface{}{}
	cyclic["self"] = cyclic

	if _, err := asInboundCall(cyclic); err == nil {
		t.Fatalf("expected marshal error for cyclic payload")
	}
}

func TestAsInboundCallDecodeError(t *testing.T) {
	bad := map[string]interface{}{"occurred_at": "not-a-timestamp"}

	if _, err := asInboundCall(bad); err == nil {
		t.Fatalf("expected decode error for malformed timestamp")
	}
}

func TestVoiceProtocolAdapterMapInvite(t *testing.T) {
	adapter := NewVoiceProtocolAdapter()

	event, err := adapter.MapInvite(context.Background(), InboundCall{
		CallID:         "call-1",
		ConversationID: "conv-1",
		From:           "1001",
		To:             "2001",
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if event.Protocol != voiceprotocol.ProtocolSIP || event.Kind != voiceprotocol.CallEventInvite {
		t.Fatalf("unexpected event: %+v", event)
	}
}

func TestVoiceProtocolAdapterRejectsUnsupportedPayload(t *testing.T) {
	adapter := NewVoiceProtocolAdapter()

	if _, err := adapter.MapHangup(context.Background(), "invalid"); err == nil {
		t.Fatalf("expected payload type error")
	}
}
