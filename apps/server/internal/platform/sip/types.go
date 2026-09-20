package sip

import "time"

type CallEventKind string

// ChannelName is the normalized channel identifier for SIP events.
const ChannelName = "sip"

const (
	CallEventInvite CallEventKind = "invite"
	CallEventHangup CallEventKind = "hangup"
	CallEventDTMF   CallEventKind = "dtmf"
)

type InboundCall struct {
	CallID         string                 `json:"call_id"`
	ConversationID string                 `json:"conversation_id,omitempty"`
	From           string                 `json:"from"`
	To             string                 `json:"to"`
	Event          CallEventKind          `json:"event"`
	DTMF           string                 `json:"dtmf,omitempty"`
	OccurredAt     time.Time              `json:"occurred_at"`
	Metadata       map[string]interface{} `json:"metadata,omitempty"`
}
