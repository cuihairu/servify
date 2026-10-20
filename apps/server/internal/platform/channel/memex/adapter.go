package memex

import (
	"context"
	"errors"
	"fmt"
	"time"

	"servify/apps/server/internal/platform/channel"
)

// ErrIngestionDisabled reports a missing or not-yet-enabled grant snapshot: the
// connector refuses to pull until an operator records an observed grant for the
// installation (contract fail-closed default).
var ErrIngestionDisabled = errors.New("memex ingestion disabled: no enabled capability snapshot")

// ErrSendDisabled reports an outbound send blocked by a missing SendText grant.
var ErrSendDisabled = errors.New("memex send disabled: capability not granted")

// Config is the stored installation mapping for one Memex bot installation. It
// carries the tenant/workspace scope and the conversation being ingested — the
// scope always comes from this configuration, never from the trusted-by-nobody
// incoming payload (contract: "tenant/workspace scope from the stored
// installation mapping").
//
// Conversation attribution is per-installation (decided 2026-10-20): Memex
// updates carry no group id — their `to` field is the bot itself — so a bot
// that serves several groups maps every update to one configured conversation.
type Config struct {
	Platform       string // adapter key ("memex"); stamped into every event
	TenantID       string
	WorkspaceID    string
	InstallationID string
	Conversation   channel.ConversationRef
}

// Translator converts one Memex update into the contract's normalized event. It
// never invents event types Memex cannot produce: every pull is message.created
// TEXT. (Adapters learn a platform's auth result via CapabilityStore; the
// translator only shapes what the walk actually returned.)
type Translator struct {
	Config Config
}

// Event maps one update onto the connector event envelope. OccurredAt is
// derived from the platform ts_ms, ReceivedAt is injected by the caller.
func (t Translator) Event(receivedAt time.Time, u Update) channel.ConnectorEvent {
	return channel.ConnectorEvent{
		Platform:       t.Config.Platform,
		InstallationID: t.Config.InstallationID,
		TenantID:       t.Config.TenantID,
		WorkspaceID:    t.Config.WorkspaceID,
		EventID:        u.MsgID,
		IdempotencyKey: channel.InboundIdempotencyKey(t.Config.InstallationID, u.MsgID),
		EventType:      channel.EventTypeMessageCreated,
		OccurredAt:     time.UnixMilli(u.TsMS),
		ReceivedAt:     receivedAt,
		Conversation:   t.Config.Conversation,
		Sender:         channel.SenderRef{PlatformID: u.From},
		Message: &channel.MessageContent{
			PlatformID: u.MsgID,
			Kind:       "text",
			Text:       u.Text,
		},
		PlatformExtensions: map[string]string{"to": u.To},
	}
}

// DeclaredCapabilities is the honest capability snapshot for the Memex bot
// surface: polling, membership-scoped reads, TEXT-only sends, no edits,
// deletes, threads or reactions. Helper so operators record the grant exactly
// as observed, never as an over-claim (contract: capabilities from the
// platform's authorization result, not an operator's unverified assertion).
func DeclaredCapabilities(now time.Time) channel.Capabilities {
	return channel.Capabilities{
		ReceiveMode:  channel.ReceiveModePolling,
		MessageScope: channel.MessageScopeAllAuthorizedGroups,
		EventTypes:   []channel.EventType{channel.EventTypeMessageCreated},
		SendText:     true,
		RateLimit:    "",
		VerifiedAt:   now,
	}
}

// InboundObserver sees every freshly committed inbound event — never
// quarantined rows, never duplicates — between the durable inbox acceptance
// and the platform ACK. It is the conversation-module ingress seam: the
// assembly bridges committed events into conversations. An error aborts the
// poll before the platform ACK, but the row is already committed and a
// redelivery is recognized as a duplicate (the observer is not re-fired), so
// a failed forward stays committed-but-unforwarded — the failure must be
// surfaced for operator triage, and the replay tooling re-drives such rows.
type InboundObserver interface {
	InboundCommitted(ctx context.Context, ev channel.ConnectorEvent) error
}

// Connector drives one Memex bot installation through the contract's
// accept-before-ACK pipeline.
type Connector struct {
	client     *Client
	caps       *channel.CapabilityStore
	sink       *channel.InboxStore
	translator Translator
	scope      channel.CapabilityScope
	observer   InboundObserver
}

// NewConnector wires the pull/ACK client to the contract stores. Both stores
// enforce tenant/workspace scoping internally, so ingestion cannot cross
// scopes regardless of what the payload claims.
func NewConnector(client *Client, caps *channel.CapabilityStore, sink *channel.InboxStore, cfg Config) *Connector {
	return &Connector{
		client:     client,
		caps:       caps,
		sink:       sink,
		translator: Translator{Config: cfg},
		scope: channel.CapabilityScope{
			TenantID:               cfg.TenantID,
			WorkspaceID:            cfg.WorkspaceID,
			Platform:               cfg.Platform,
			InstallationID:         cfg.InstallationID,
			ConversationPlatformID: cfg.Conversation.PlatformID,
		},
	}
}

// WithObserver attaches the committed-event observer (the assembly's
// conversation-ingress bridge) and returns the connector for chaining.
func (c *Connector) WithObserver(o InboundObserver) *Connector {
	c.observer = o
	return c
}

// PollOnce drains one batch of the bot inbox. Every update is translated and
// durably accepted by the inbox (commit/quarantine/duplicate) before its
// platform ACK clears it, so a crash between accept and ACK yields redelivery
// that the inbox idempotency key absorbs (at-least-once). The first failure
// aborts without ACKing, leaving unacknowledged messages for redelivery.
func (c *Connector) PollOnce(ctx context.Context, longPoll time.Duration) (int, error) {
	enabled, err := c.ingestionEnabled(ctx)
	if err != nil {
		return 0, err
	}
	if !enabled {
		return 0, ErrIngestionDisabled
	}
	updates, err := c.client.Updates(ctx, int(longPoll/time.Millisecond))
	if err != nil {
		return 0, err
	}
	processed := 0
	for _, u := range updates {
		ev := c.translator.Event(time.Now(), u)
		decision, err := c.sink.Accept(ctx, ev)
		if err != nil {
			return processed, err
		}
		// Forward exactly once per inbox row: only the fresh-commit verdict
		// fires the observer, so a redelivered copy never double-ingests.
		if decision == channel.AckCommit && c.observer != nil {
			if err := c.observer.InboundCommitted(ctx, ev); err != nil {
				return processed, fmt.Errorf("forward %s: %w", u.MsgID, err)
			}
		}
		// Every durable verdict clears the platform copy: commit and
		// quarantine are fresh acceptances, duplicate is a redelivered copy.
		if err := c.client.Ack(ctx, u.MsgID); err != nil {
			return processed, fmt.Errorf("ack %s after %s: %w", u.MsgID, decision, err)
		}
		processed++
	}
	return processed, nil
}

// Notify sends one TEXT message as the bot, gated on the recorded SendText
// capability — the outbound sibling of the ingestion gate.
func (c *Connector) Notify(ctx context.Context, target, text string) (string, error) {
	snapshot, err := c.caps.Latest(ctx, c.scope)
	if err != nil {
		return "", err
	}
	if snapshot == nil || !snapshot.SendText {
		return "", ErrSendDisabled
	}
	return c.client.Send(ctx, target, text)
}

func (c *Connector) ingestionEnabled(ctx context.Context) (bool, error) {
	snapshot, err := c.caps.Latest(ctx, c.scope)
	if err != nil {
		return false, err
	}
	return snapshot != nil && snapshot.IngestionEnabled(), nil
}
