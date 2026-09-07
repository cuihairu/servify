package pstnprovider

import (
	"context"
	"strings"
	"testing"
	"time"

	"servify/apps/server/internal/platform/voiceprotocol"
)

func TestAdapterNameAndProtocol(t *testing.T) {
	adapter := NewAdapter()
	if adapter.Name() != "pstn-provider" {
		t.Fatalf("unexpected name %q", adapter.Name())
	}
	if adapter.Protocol() != voiceprotocol.ProtocolPSTNProvider {
		t.Fatalf("unexpected protocol %v", adapter.Protocol())
	}
}

func TestAdapterMapMethodsHappyPath(t *testing.T) {
	occurredAt := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	adapter := NewAdapter()

	tests := []struct {
		name  string
		call  func() (voiceprotocol.CallEvent, error)
		kind  voiceprotocol.CallEventKind
		dtmf  string
		event string
	}{
		{
			name:  "invite",
			kind:  voiceprotocol.CallEventInvite,
			event: "call.initiated",
			call: func() (voiceprotocol.CallEvent, error) {
				return adapter.MapInvite(context.Background(), WebhookEvent{
					EventID:    "evt-1",
					Provider:   "twilio",
					EventType:  "call.initiated",
					CallID:     "call-1",
					From:       "+1001",
					To:         "+2002",
					OccurredAt: occurredAt,
				})
			},
		},
		{
			name:  "answer",
			kind:  voiceprotocol.CallEventAnswer,
			event: "call.answered",
			call: func() (voiceprotocol.CallEvent, error) {
				return adapter.MapAnswer(context.Background(), WebhookEvent{
					EventID:        "evt-2",
					CallID:         "call-2",
					ConversationID: "conv-2",
					OccurredAt:     occurredAt,
				})
			},
		},
		{
			name:  "hold",
			kind:  voiceprotocol.CallEventHold,
			event: "call.hold",
			call: func() (voiceprotocol.CallEvent, error) {
				return adapter.MapHold(context.Background(), map[string]interface{}{
					"event_id":     "evt-3",
					"call_id":      "call-3",
					"occurred_at":  occurredAt.Format(time.RFC3339Nano),
					"conversation": "ignored",
				})
			},
		},
		{
			name:  "resume",
			kind:  voiceprotocol.CallEventResume,
			event: "call.resume",
			call: func() (voiceprotocol.CallEvent, error) {
				return adapter.MapResume(context.Background(), WebhookEvent{
					EventID:    "evt-4",
					CallID:     "call-4",
					OccurredAt: occurredAt,
				})
			},
		},
		{
			name:  "transfer",
			kind:  voiceprotocol.CallEventTransfer,
			event: "call.transferred",
			call: func() (voiceprotocol.CallEvent, error) {
				return adapter.MapTransfer(context.Background(), WebhookEvent{
					EventID:    "evt-5",
					CallID:     "call-5",
					OccurredAt: occurredAt,
				})
			},
		},
		{
			name:  "dtmf",
			kind:  voiceprotocol.CallEventDTMF,
			event: "call.dtmf",
			dtmf:  "123",
			call: func() (voiceprotocol.CallEvent, error) {
				return adapter.MapDTMF(context.Background(), WebhookEvent{
					EventID:    "evt-6",
					CallID:     "call-6",
					DTMF:       "123",
					OccurredAt: occurredAt,
				})
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			event, err := tc.call()
			if err != nil {
				t.Fatalf("expected no error, got %v", err)
			}
			if event.Kind != tc.kind {
				t.Fatalf("expected kind %v, got %v", tc.kind, event.Kind)
			}
			if event.Protocol != voiceprotocol.ProtocolPSTNProvider {
				t.Fatalf("unexpected protocol %v", event.Protocol)
			}
			if event.EventID == "" {
				t.Fatalf("expected event id to be set")
			}
			if !event.OccurredAt.Equal(occurredAt) {
				t.Fatalf("expected occurred_at %v, got %v", occurredAt, event.OccurredAt)
			}
			if event.Metadata == nil {
				t.Fatalf("expected metadata map to be initialized")
			}
			if tc.dtmf != "" && event.Metadata["digits"] != tc.dtmf {
				t.Fatalf("expected digits metadata %q, got %v", tc.dtmf, event.Metadata["digits"])
			}
		})
	}
}

func TestAdapterMapInviteFromMapPayload(t *testing.T) {
	adapter := NewAdapter()
	event, err := adapter.MapInvite(context.Background(), map[string]interface{}{
		"provider":        "twilio",
		"event_id":        "evt-map",
		"event_type":      "call.initiated",
		"call_id":         "call-map",
		"conversation_id": "conv-map",
		"from":            "+1001",
		"to":              "+2002",
		"metadata": map[string]interface{}{
			"region": "us-east-1",
		},
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if event.CallID != "call-map" || event.ConversationID != "conv-map" {
		t.Fatalf("unexpected event: %+v", event)
	}
	if event.From != "+1001" || event.To != "+2002" {
		t.Fatalf("unexpected from/to: %+v", event)
	}
	if event.Metadata["provider"] != "twilio" || event.Metadata["event_type"] != "call.initiated" {
		t.Fatalf("unexpected metadata: %+v", event.Metadata)
	}
	if event.Metadata["region"] != "us-east-1" {
		t.Fatalf("expected original metadata preserved, got %+v", event.Metadata)
	}
}

func TestToCallEventDefaults(t *testing.T) {
	before := time.Now()
	event := toCallEvent(WebhookEvent{CallID: "call-x"}, voiceprotocol.CallEventInvite)
	if event.EventID != "pstn-invite-call-x" {
		t.Fatalf("unexpected generated event id %q", event.EventID)
	}
	if event.OccurredAt.Before(before) {
		t.Fatalf("expected occurred_at defaulted to now, got %v", event.OccurredAt)
	}
	if _, ok := event.Metadata["provider"]; ok {
		t.Fatalf("expected no provider metadata, got %+v", event.Metadata)
	}
	if _, ok := event.Metadata["event_type"]; ok {
		t.Fatalf("expected no event_type metadata, got %+v", event.Metadata)
	}
	if len(event.Metadata) != 0 {
		t.Fatalf("expected empty metadata, got %+v", event.Metadata)
	}
}

func TestToCallEventDoesNotMutateSourceMetadata(t *testing.T) {
	src := WebhookEvent{
		CallID:      "call-y",
		EventID:     "evt-y",
		Metadata:    map[string]interface{}{"k": "v"},
		EventType:   "call.hangup",
		OccurredAt:  time.Now(),
	}
	event := toCallEvent(src, voiceprotocol.CallEventHangup)
	event.Metadata["k2"] = "v2"
	if _, ok := src.Metadata["k2"]; ok {
		t.Fatalf("expected source metadata to remain untouched")
	}
	if event.EventID != "evt-y" {
		t.Fatalf("expected event id preserved, got %q", event.EventID)
	}
}

func TestAsWebhookUnsupportedPayload(t *testing.T) {
	adapter := NewAdapter()
	if _, err := adapter.MapInvite(context.Background(), "not-a-webhook"); err == nil {
		t.Fatalf("expected payload type error")
	} else if !strings.Contains(err.Error(), "unsupported pstn payload type") {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := adapter.MapAnswer(context.Background(), 42); err == nil {
		t.Fatalf("expected payload type error for int payload")
	}
}

func TestAsWebhookUnmarshalError(t *testing.T) {
	adapter := NewAdapter()
	if _, err := adapter.MapHold(context.Background(), map[string]interface{}{
		"occurred_at": 12345,
	}); err == nil {
		t.Fatalf("expected decode error")
	} else if !strings.Contains(err.Error(), "decode pstn payload") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestAsWebhookMarshalError(t *testing.T) {
	adapter := NewAdapter()
	if _, err := adapter.MapResume(context.Background(), map[string]interface{}{
		"bad": make(chan int),
	}); err == nil {
		t.Fatalf("expected marshal error")
	} else if !strings.Contains(err.Error(), "marshal pstn payload") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestAdapterMapErrorsAcrossMethods(t *testing.T) {
	adapter := NewAdapter()
	calls := []func(interface{}) (voiceprotocol.CallEvent, error){
		func(p interface{}) (voiceprotocol.CallEvent, error) {
			return adapter.MapInvite(context.Background(), p)
		},
		func(p interface{}) (voiceprotocol.CallEvent, error) {
			return adapter.MapAnswer(context.Background(), p)
		},
		func(p interface{}) (voiceprotocol.CallEvent, error) {
			return adapter.MapHold(context.Background(), p)
		},
		func(p interface{}) (voiceprotocol.CallEvent, error) {
			return adapter.MapResume(context.Background(), p)
		},
		func(p interface{}) (voiceprotocol.CallEvent, error) {
			return adapter.MapHangup(context.Background(), p)
		},
		func(p interface{}) (voiceprotocol.CallEvent, error) {
			return adapter.MapTransfer(context.Background(), p)
		},
		func(p interface{}) (voiceprotocol.CallEvent, error) {
			return adapter.MapDTMF(context.Background(), p)
		},
	}
	for i, call := range calls {
		if _, err := call(nil); err == nil {
			t.Fatalf("method %d: expected error for nil payload", i)
		}
	}
}
