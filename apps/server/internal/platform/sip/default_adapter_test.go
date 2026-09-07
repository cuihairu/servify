package sip

import (
	"context"
	"testing"
	"time"

	channelplatform "servify/apps/server/internal/platform/channel"
)

func TestDefaultAdapterMapInvite(t *testing.T) {
	adapter := NewDefaultAdapter()
	now := time.Now().UTC()

	event, err := adapter.MapInvite(context.Background(), InboundCall{
		CallID:         "call-1",
		ConversationID: "conv-1",
		From:           "1001",
		To:             "2001",
		Event:          CallEventInvite,
		OccurredAt:     now,
		Metadata:       map[string]interface{}{"tenant_id": "t-1"},
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if event.Channel != ChannelName || event.Kind != channelplatform.EventKindCall {
		t.Fatalf("unexpected event: %+v", event)
	}
	if event.Payload["event"] != CallEventInvite {
		t.Fatalf("expected invite payload, got %+v", event.Payload)
	}
}

func TestDefaultAdapterName(t *testing.T) {
	adapter := NewDefaultAdapter()
	if adapter.Name() != ChannelName {
		t.Fatalf("expected name %q, got %q", ChannelName, adapter.Name())
	}
}

func TestDefaultAdapterMapHangup(t *testing.T) {
	adapter := NewDefaultAdapter()
	now := time.Now().UTC().Truncate(time.Second)

	event, err := adapter.MapHangup(context.Background(), InboundCall{
		CallID:         "call-hangup",
		ConversationID: "conv-9",
		From:           "1003",
		To:             "2003",
		Event:          CallEventHangup,
		OccurredAt:     now,
		Metadata:       map[string]interface{}{"reason": "bye"},
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if event.EventID != "sip-hangup-call-hangup" {
		t.Fatalf("unexpected event id: %q", event.EventID)
	}
	if event.ConversationID != "conv-9" {
		t.Fatalf("unexpected conversation id: %q", event.ConversationID)
	}
	if event.ActorID != "1003" {
		t.Fatalf("unexpected actor id: %q", event.ActorID)
	}
	if !event.OccurredAt.Equal(now) {
		t.Fatalf("expected occurredAt %v, got %v", now, event.OccurredAt)
	}
	if event.Payload["event"] != CallEventHangup {
		t.Fatalf("expected hangup payload, got %+v", event.Payload)
	}
	if event.Payload["metadata"] == nil {
		t.Fatalf("expected metadata payload, got %+v", event.Payload)
	}
	if event.Payload["from"] != "1003" || event.Payload["to"] != "2003" {
		t.Fatalf("unexpected payload addressing: %+v", event.Payload)
	}
}

func TestDefaultAdapterMapDTMF(t *testing.T) {
	adapter := NewDefaultAdapter()

	event, err := adapter.MapDTMF(context.Background(), InboundCall{
		CallID: "call-2",
		From:   "1002",
		To:     "ivr",
		DTMF:   "9",
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if event.Payload["digits"] != "9" {
		t.Fatalf("expected dtmf digits, got %+v", event.Payload)
	}
	if event.ActorID != "1002" {
		t.Fatalf("expected actor id to be caller, got %+v", event)
	}
}
