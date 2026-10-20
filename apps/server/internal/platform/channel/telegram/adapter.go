package telegram

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"servify/apps/server/internal/platform/channel"
)

// unknownSender marks a quarantined update whose author we cannot identify from
// the shape — the row is evidence for triage, not a forwardable message.
const unknownSender = "unknown"

// Config is the stored installation mapping for one Telegram bot installation.
// It carries the tenant/workspace scope and the configured conversation (the
// installation's Telegram chat id) — the scope always comes from this
// configuration, never from the incoming payload.
//
// Conversation attribution differs from Memex: Telegram updates DO carry the
// chat they came from, so each message is attributed to its own chat rather
// than to the one configured installation. What still never comes from the
// payload is the tenant/workspace scope (contract: scope from the stored
// installation mapping) — an update cannot talk itself into another tenant's
// conversations.
type Config struct {
	Platform       string // adapter key ("telegram"); stamped into every event
	TenantID       string
	WorkspaceID    string
	InstallationID string
	// Conversation is the configured chat: the capability-scope key and the
	// fallback attribution for update shapes that carry no chat.
	Conversation channel.ConversationRef
}

// Translator converts one Telegram update into the contract's normalized
// event. It never invents an event type Telegram did not deliver: a message
// with TEXT is message.created, and every other shape (edited_message,
// callback_query, media-only messages) is the quarantine lane.
type Translator struct {
	Config Config
}

// Event maps one update onto the connector event envelope. OccurredAt comes
// from the platform date (unix seconds) for messages; update shapes without
// their own timestamp are stamped with received time. ReceivedAt is injected
// by the caller.
func (t Translator) Event(receivedAt time.Time, u Update) channel.ConnectorEvent {
	ev := channel.ConnectorEvent{
		Platform:       t.Config.Platform,
		InstallationID: t.Config.InstallationID,
		TenantID:       t.Config.TenantID,
		WorkspaceID:    t.Config.WorkspaceID,
		ReceivedAt:     receivedAt,
	}
	msg := u.Message
	if msg == nil || msg.Chat == nil || msg.Text == "" {
		return t.unknownEvent(receivedAt, u.UpdateID)
	}
	chatID := strconv.FormatInt(msg.Chat.ID, 10)
	msgID := strconv.FormatInt(msg.MessageID, 10)
	sender := unknownSender
	if msg.From != nil {
		sender = strconv.FormatInt(msg.From.ID, 10)
	}
	ev.EventID = msgID
	ev.IdempotencyKey = channel.InboundIdempotencyKey(t.Config.InstallationID, msgID)
	ev.EventType = channel.EventTypeMessageCreated
	ev.OccurredAt = time.Unix(msg.Date, 0)
	ev.Conversation = channel.ConversationRef{
		PlatformID: chatID,
		Kind:       msg.Chat.Type,
		IsGroup:    msg.Chat.Type != "private",
	}
	ev.Sender = channel.SenderRef{PlatformID: sender}
	ev.Message = &channel.MessageContent{PlatformID: msgID, Kind: "text", Text: msg.Text}
	return ev
}

// unknownEvent shapes a non-message (or text-less) update for the quarantine
// lane. No raw payload reference is recorded: the retained raw-payload
// storage surface is not built yet, so the row keeps exactly what identifies
// the update — id, installation, scope — and disposition (dismiss) is the
// available path; message reconstruction stays out of scope for now.
func (t Translator) unknownEvent(receivedAt time.Time, updateID int64) channel.ConnectorEvent {
	id := "update-" + strconv.FormatInt(updateID, 10)
	return channel.ConnectorEvent{
		Platform:       t.Config.Platform,
		InstallationID: t.Config.InstallationID,
		TenantID:       t.Config.TenantID,
		WorkspaceID:    t.Config.WorkspaceID,
		EventID:        id,
		IdempotencyKey: channel.InboundIdempotencyKey(t.Config.InstallationID, id),
		EventType:      channel.EventTypeUnknown,
		OccurredAt:     receivedAt,
		ReceivedAt:     receivedAt,
		Conversation:   t.Config.Conversation,
		Sender:         channel.SenderRef{PlatformID: unknownSender},
	}
}

// DeclaredCapabilities is the honest capability snapshot for the Bot API long
// poll surface: polling, membership-scoped reads (a bot only receives chats it
// is a member of), TEXT-only sends, no edits, deletes, threads or reactions.
// Helper so operators record the grant exactly as observed, never as an
// over-claim (contract: capabilities from the platform's authorization result,
// not an operator's unverified assertion).
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

// Connector drives one Telegram bot installation through the contract's
// accept-before-ACK pipeline. Telegram's ACK is offset progression: the
// connector persists the frontier in the cursor store only after a durable
// inbox verdict, so a crash between acceptance and cursor save redelivers the
// update and the inbox idempotency key absorbs it (at-least-once).
type Connector struct {
	client      *Client
	caps        *channel.CapabilityStore
	sink        *channel.InboxStore
	cursors     *channel.CursorStore
	translator  Translator
	scope       channel.CapabilityScope
	cursorScope channel.CursorScope
	observer    channel.InboundObserver
}

// NewConnector wires the pull/send client to the contract stores. Both stores
// enforce tenant/workspace scoping internally, so ingestion cannot cross
// scopes regardless of what the payload claims.
func NewConnector(client *Client, caps *channel.CapabilityStore, sink *channel.InboxStore, cursors *channel.CursorStore, cfg Config) *Connector {
	return &Connector{
		client:     client,
		caps:       caps,
		sink:       sink,
		cursors:    cursors,
		translator: Translator{Config: cfg},
		scope: channel.CapabilityScope{
			TenantID:               cfg.TenantID,
			WorkspaceID:            cfg.WorkspaceID,
			Platform:               cfg.Platform,
			InstallationID:         cfg.InstallationID,
			ConversationPlatformID: cfg.Conversation.PlatformID,
		},
		cursorScope: channel.CursorScope{
			TenantID:       cfg.TenantID,
			WorkspaceID:    cfg.WorkspaceID,
			Platform:       cfg.Platform,
			InstallationID: cfg.InstallationID,
		},
	}
}

// WithObserver attaches the committed-event observer (the assembly's
// conversation-ingress bridge) and returns the connector for chaining.
func (c *Connector) WithObserver(o channel.InboundObserver) *Connector {
	c.observer = o
	return c
}

// PollOnce drains one long-poll batch. Every update is translated and
// durably accepted by the inbox (commit/quarantine/duplicate) before its
// offset frontier advances, so a crash between accept and cursor save yields
// redelivery that the inbox idempotency key absorbs (at-least-once). The
// first failure aborts without advancing, leaving the update for redelivery.
func (c *Connector) PollOnce(ctx context.Context, longPoll time.Duration) (int, error) {
	enabled, err := c.ingestionEnabled(ctx)
	if err != nil {
		return 0, err
	}
	if !enabled {
		return 0, ErrIngestionDisabled
	}
	offset, err := c.offset(ctx)
	if err != nil {
		return 0, err
	}
	updates, err := c.client.GetUpdates(ctx, offset, int((longPoll+time.Second-1)/time.Second))
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
		// fires the observer, so a redelivered copy never double-ingests and
		// a quarantined shape never reaches the conversation module.
		if decision == channel.AckCommit && c.observer != nil {
			if err := c.observer.InboundCommitted(ctx, ev); err != nil {
				return processed, fmt.Errorf("forward %s: %w", ev.EventID, err)
			}
		}
		if err := c.advance(ctx, u.UpdateID); err != nil {
			return processed, fmt.Errorf("cursor after %s: %w", ev.EventID, err)
		}
		processed++
	}
	return processed, nil
}

// offset reads the acknowledged frontier. An empty cursor (first run, or a
// deliberately cleared one) starts at the Bot API's recent default window.
func (c *Connector) offset(ctx context.Context) (int64, error) {
	cur, err := c.cursors.Cursor(ctx, c.cursorScope)
	if err != nil || cur == "" {
		return 0, err
	}
	n, err := strconv.ParseInt(cur, 10, 64)
	if err != nil {
		// A corrupt cursor is a store-integrity failure, not a platform error:
		// retrying cannot fix it, so it is returned unclassified and the
		// runner surfaces it instead of polling forever.
		return 0, fmt.Errorf("stored telegram cursor %q is not an update offset: %w", cur, err)
	}
	return n, nil
}

// advance moves the acknowledged frontier past one update — Telegram's
// acknowledgement. The cursor is written only after the inbox verdict.
func (c *Connector) advance(ctx context.Context, updateID int64) error {
	return c.cursors.Save(ctx, c.cursorScope, strconv.FormatInt(updateID+1, 10))
}

// Notify sends one TEXT message as the bot, gated on the recorded SendText
// capability — the outbound sibling of the ingestion gate. The target is the
// Telegram chat id (integers only: private, group, supergroup and channel ids).
func (c *Connector) Notify(ctx context.Context, target, text string) (string, error) {
	snapshot, err := c.caps.Latest(ctx, c.scope)
	if err != nil {
		return "", err
	}
	if snapshot == nil || !snapshot.SendText {
		return "", ErrSendDisabled
	}
	chatID, err := strconv.ParseInt(strings.TrimSpace(target), 10, 64)
	if err != nil || chatID == 0 {
		return "", fmt.Errorf("telegram chat id %q is not an integer", target)
	}
	id, err := c.client.SendText(ctx, chatID, text)
	if err != nil {
		return "", err
	}
	return strconv.FormatInt(id, 10), nil
}

func (c *Connector) ingestionEnabled(ctx context.Context) (bool, error) {
	snapshot, err := c.caps.Latest(ctx, c.scope)
	if err != nil {
		return false, err
	}
	return snapshot != nil && snapshot.IngestionEnabled(), nil
}
