package memex

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"servify/apps/server/internal/platform/channel"
)

// The fail-closed gate errors are contract-level vocabulary (shared with the
// poll runner and every other platform adapter); they are re-exported here so
// the memex call sites read unchanged.
var (
	ErrIngestionDisabled = channel.ErrIngestionDisabled
	ErrSendDisabled      = channel.ErrSendDisabled
)

// ErrRichSendUnsupported reports an outbound body that carries content parts on
// the TEXT-only surface. The adapter cannot render a part silently: the caller
// must send a TEXT body (contract: no adapter may drop a structured part it
// cannot deliver).
var ErrRichSendUnsupported = errors.New("memex: rich outbound content is not supported by the TEXT surface")

// ErrReplyUnsupported and ErrThreadUnsupported report a reply-chain or topic
// parameter the /bot/send endpoint has no parameter for. The adapter refuses
// rather than silently delivering a top-level message a caller asked to thread.
var (
	ErrReplyUnsupported  = errors.New("memex: reply chain is not supported by the TEXT surface")
	ErrThreadUnsupported = errors.New("memex: thread send is not supported by the TEXT surface")
)

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

// InboundObserver is the shared committed-event seam (channel.InboundObserver,
// re-exported for memex call sites).
type InboundObserver = channel.InboundObserver

// Connector drives one Memex bot installation through the contract's
// accept-before-ACK pipeline.
type Connector struct {
	client     *Client
	caps       *channel.CapabilityStore
	sink       *channel.InboxStore
	translator Translator
	scope      channel.CapabilityScope
	observer   InboundObserver
	payloads   *channel.LocalPayloadStore

	// retry is the contract's outbound attempt policy, shared with every other
	// platform adapter so one budget cannot drift. Memex's /bot/send has no
	// idempotency parameter, so the caller's key is the retry anchor and never
	// leaves the connector.
	retry channel.SendRetryPolicy
}

// Compile-time assertion: the connector satisfies the contract's full
// Connector surface (Capabilities / Receive / Send / Notify), so a caller can
// hold the platform-independent interface without reaching into Memex.
var _ channel.Connector = (*Connector)(nil)

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
		retry: channel.DefaultSendRetryPolicy(),
	}
}

// WithObserver attaches the committed-event observer (the assembly's
// conversation-ingress bridge) and returns the connector for chaining.
func (c *Connector) WithObserver(o InboundObserver) *Connector {
	c.observer = o
	return c
}

// WithPayloadStore attaches the raw-payload retention store (contract:
// encrypted on local storage, 30 days maximum). Without one the connector
// ingests normally and keeps no payload bytes — retention off is a valid,
// observable configuration, not a failure.
func (c *Connector) WithPayloadStore(s *channel.LocalPayloadStore) *Connector {
	c.payloads = s
	return c
}

// attachRawPayload stores the exact update bytes before the inbox verdict, so
// a row never references an object that was not written. A save failure aborts
// the batch: the acceptance would be missing its retention copy, and
// redelivery (no ACK sent) is the recovery path.
func (c *Connector) attachRawPayload(ctx context.Context, u Update, ev channel.ConnectorEvent) (channel.ConnectorEvent, error) {
	if c.payloads == nil || len(u.Raw()) == 0 {
		return ev, nil
	}
	ref, err := c.payloads.Save(ctx, channel.PayloadMeta{
		TenantID:       ev.TenantID,
		WorkspaceID:    ev.WorkspaceID,
		Platform:       ev.Platform,
		InstallationID: ev.InstallationID,
		EventID:        ev.EventID,
		ContentType:    channel.RawPayloadContentType,
	}, u.Raw())
	if err != nil {
		return ev, fmt.Errorf("retain raw payload for %s: %w", ev.EventID, err)
	}
	ev.RawPayloadRef = &ref
	return ev, nil
}

// dropOrphanPayload removes this redelivery's retention copy: a duplicate
// verdict means the surviving row already points at the copy saved by the
// first delivery, so the object just written is an orphan. Best effort — a
// failed delete is swept by retention long before it matters.
func (c *Connector) dropOrphanPayload(ctx context.Context, ev channel.ConnectorEvent) {
	if c.payloads == nil || ev.RawPayloadRef == nil {
		return
	}
	_ = c.payloads.Delete(ctx, *ev.RawPayloadRef)
}

// PollOnce drains one batch of the bot inbox. Every update is translated, its
// raw payload retained, and the event durably accepted by the inbox
// (commit/quarantine/duplicate) before the platform ACK clears it, so a crash
// between accept and ACK yields redelivery that the inbox idempotency key
// absorbs (at-least-once). The first failure aborts without ACKing, leaving
// unacknowledged messages for redelivery.
func (c *Connector) PollOnce(ctx context.Context, longPoll time.Duration) (int, error) {
	return c.drain(ctx, c.sink, longPoll)
}

// Capabilities observes the latest recorded platform grant for the
// installation this connector is bound to. It is the contract's live-grant
// face: the connector returns whatever the capability store holds for its own
// scope, so an unrecorded or revoked grant surfaces as the fail-closed zero
// snapshot (IngestionEnabled false) rather than an operator's assertion. An
// installationID that is not this connector's own is a caller error — one
// connector observes exactly one installation.
func (c *Connector) Capabilities(ctx context.Context, installationID string) (channel.Capabilities, error) {
	if installationID != c.scope.InstallationID {
		return channel.Capabilities{}, fmt.Errorf("memex: capability listener bound to installation %q cannot observe %q",
			c.scope.InstallationID, installationID)
	}
	snapshot, err := c.caps.Latest(ctx, c.scope)
	if err != nil {
		return channel.Capabilities{}, err
	}
	if snapshot == nil {
		return channel.Capabilities{}, nil
	}
	return *snapshot, nil
}

// Receive drains one long-poll batch through the caller-supplied sink at the
// contract's default long-poll budget. It is the Connector interface's
// single-drain face over the same accept-before-ACK pipeline as PollOnce, so a
// caller holding the platform-independent surface gets the identical durable
// behavior without reconfiguring the connector's stores.
func (c *Connector) Receive(ctx context.Context, sink channel.InboundSink) error {
	_, err := c.drain(ctx, sink, time.Duration(channel.DefaultLongPollMS)*time.Millisecond)
	return err
}

// drain is the shared accept-before-ACK pipeline. Every update is translated,
// its raw payload retained, and the event durably accepted by the sink
// (commit/quarantine/duplicate) before the platform ACK clears it, so a crash
// between accept and ACK yields redelivery that the inbox idempotency key
// absorbs (at-least-once). The first failure aborts without ACKing, leaving
// unacknowledged messages for redelivery.
func (c *Connector) drain(ctx context.Context, sink channel.InboundSink, longPoll time.Duration) (int, error) {
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
		ev, err := c.attachRawPayload(ctx, u, ev)
		if err != nil {
			return processed, err
		}
		decision, err := sink.Accept(ctx, ev)
		if err != nil {
			return processed, err
		}
		if decision == channel.AckDuplicate {
			c.dropOrphanPayload(ctx, ev)
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

// Send delivers one TEXT message to the request's conversation. Memex's
// /bot/send has no thread or reply parameter, so a request carrying either is
// refused before it ever reaches the platform rather than silently delivered
// as a top-level message; the idempotency key is the caller's stable retry
// anchor. ContentParts is accepted for interface conformance but this TEXT
// surface renders only Text — a richer body is rejected by the capability
// gate before it reaches the platform.
func (c *Connector) Send(ctx context.Context, req channel.SendRequest) (channel.SendResult, error) {
	if err := req.Validate(); err != nil {
		return channel.SendResult{}, err
	}
	if req.ThreadID != "" {
		return channel.SendResult{}, ErrThreadUnsupported
	}
	if req.ReplyToMessageID != "" {
		return channel.SendResult{}, ErrReplyUnsupported
	}
	return c.deliver(ctx, strings.TrimSpace(req.ConversationID), req.Content, req.IdempotencyKey, req.Deadline)
}

// Notify sends one TEXT operator/system announcement as the bot, gated on the
// recorded SendText capability — the outbound sibling of the ingestion gate.
// Target is the explicit recipient scope (a "group:<id>" conversation or an
// account name) and Reason is the audit trail entry for why the announcement
// ran. The adapter never broadens the audience implicitly.
func (c *Connector) Notify(ctx context.Context, req channel.NotifyRequest) (channel.SendResult, error) {
	if err := req.Validate(); err != nil {
		return channel.SendResult{}, err
	}
	return c.deliver(ctx, strings.TrimSpace(req.Target), req.Content, req.IdempotencyKey, req.Deadline)
}

// deliver runs the capability-gated send with the caller's retry anchor and
// deadline under the contract's shared outbound budget: permanent and invalid
// refusals are rejected, server-class 429/5xx refusals are retried, and a
// transport failure (the request may have been delivered) is unknown without
// a resend — the caller keeps the same idempotency key for any
// operator-approved reconciliation.
func (c *Connector) deliver(ctx context.Context, target string, content channel.SendContent, idempotencyKey string, deadline time.Time) (channel.SendResult, error) {
	snapshot, err := c.caps.Latest(ctx, c.scope)
	if err != nil {
		return channel.SendResult{}, err
	}
	if snapshot == nil || !snapshot.SendText {
		return channel.SendResult{}, ErrSendDisabled
	}
	if len(content.ContentParts) != 0 {
		return channel.SendResult{}, ErrRichSendUnsupported
	}
	if !deadline.IsZero() {
		var cancel context.CancelFunc
		ctx, cancel = context.WithDeadline(ctx, deadline)
		defer cancel()
	}
	msgID, err := channel.RetrySend(ctx, c.retry, idempotencyKey, func(ctx context.Context) (string, error) {
		return c.client.Send(ctx, target, content.Text)
	})
	if err != nil {
		return channel.OutboundSendResult(err), err
	}
	return channel.SendResult{
		PlatformMessageID: msgID,
		AcceptedAt:        time.Now(),
		Status:            channel.SendResultAccepted,
	}, nil
}

func (c *Connector) ingestionEnabled(ctx context.Context) (bool, error) {
	snapshot, err := c.caps.Latest(ctx, c.scope)
	if err != nil {
		return false, err
	}
	return snapshot != nil && snapshot.IngestionEnabled(), nil
}
