package channel

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ChatConnectorInbox pipes the contract's Phase 1 inbox storage
// (docs/chat-platform-integration-contract.md): one acceptance record per
// (tenant, workspace, idempotency key) event key, source references for the
// conversation/sender/message that produced it, and a quarantine lane for
// unknown event types. It is deliberately scoped to tenant and workspace so no
// query can surface another scope's rows.
type ChatConnectorInbox struct {
	ID                     uint   `gorm:"primaryKey" json:"id"`
	TenantID               string `gorm:"not null;uniqueIndex:uq_chat_connector_inboxes_scope_key" json:"tenant_id"`
	WorkspaceID            string `gorm:"not null;uniqueIndex:uq_chat_connector_inboxes_scope_key" json:"workspace_id"`
	IdempotencyKey         string `gorm:"not null;uniqueIndex:uq_chat_connector_inboxes_scope_key" json:"idempotency_key"`
	Platform               string `gorm:"not null" json:"platform"`
	InstallationID         string `gorm:"index;not null" json:"installation_id"`
	EventID                string `gorm:"not null" json:"event_id"`
	EventType              string `gorm:"not null" json:"event_type"`
	Status                 string `gorm:"not null" json:"status"` // committed | quarantined | dismissed
	ConversationPlatformID string `gorm:"not null" json:"conversation_platform_id"`
	SenderPlatformID       string `gorm:"not null" json:"sender_platform_id"`
	MessagePlatformID      string `gorm:"not null" json:"message_platform_id"`
	// MessageText is a bounded normalized content snapshot of the message that
	// produced the event, kept for triage review (contract: source references
	// include a bounded content snapshot). It is retained for both committed and
	// quarantined rows, so an unauthorized conversation still has review
	// evidence even when raw-payload retention is disabled.
	MessageText string `json:"message_text"`
	RawPayloadStore        string `json:"raw_payload_store"`
	RawPayloadObjectID     string `json:"raw_payload_object_id"`
	// RawPayloadSHA256 is the plaintext digest of the retained object: the
	// object is sealed with AES-256-GCM, so only the digest can be verified
	// from the row alone (swapping the object for a different one is caught).
	RawPayloadSHA256 string    `json:"raw_payload_sha256"`
	OccurredAt       time.Time `json:"occurred_at"`
	ReceivedAt       time.Time `json:"received_at"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
	// ForwardedAt is when the committed event reached the normal flow. NULL
	// marks the replay lane: the row is durable but its conversation ingest
	// never settled. Never NULL for a quarantined row — nothing was ever
	// going to be forwarded.
	ForwardedAt *time.Time `json:"forwarded_at"`
}

const (
	// InboxStatusCommitted marks an event accepted into normal processing.
	InboxStatusCommitted = "committed"
	// InboxStatusQuarantined marks an unknown event held for triage.
	InboxStatusQuarantined = "quarantined"
	// InboxStatusDismissed marks a quarantined event dispositioned by an
	// operator: it left the triage lane but stays as evidence (the row is
	// never deleted).
	InboxStatusDismissed = "dismissed"
)

// maxMessageTextSnapshotRunes bounds the normalized content snapshot retained
// on an inbox row. The contract requires a bounded content snapshot for review;
// the full platform bytes (if retained) remain the raw-payload object, so this
// column is never the archive.
const maxMessageTextSnapshotRunes = 4096

// boundedTextSnapshot returns a rune-bounded copy of the message's normalized
// text so a quarantined row keeps review evidence even when raw-payload
// retention is disabled. A nil message (unknown shape with no body) yields an
// empty snapshot.
func boundedTextSnapshot(msg *MessageContent) string {
	if msg == nil {
		return ""
	}
	runes := []rune(msg.Text)
	if len(runes) <= maxMessageTextSnapshotRunes {
		return msg.Text
	}
	return string(runes[:maxMessageTextSnapshotRunes])
}

// ErrInboxNotQuarantined is returned when the row exists in the requested
// scope but is not in the quarantine lane (committed, or already dismissed).
var ErrInboxNotQuarantined = errors.New("channel: inbox row is not quarantined")

// ErrInboxNotCommitted is returned when the row exists in the requested scope
// but never entered the normal flow (quarantined or dismissed): replay only
// applies to committed rows.
var ErrInboxNotCommitted = errors.New("channel: inbox row is not committed")

// The replay sentinels distinguish why a committed row cannot be driven back
// through the normal flow. They carry no payload content (contract: redacted
// operational surfaces) and map to operator-facing states, not crashes.
var (
	// ErrReplayPayloadUnavailable: the row kept no retained object (retention
	// off when it was accepted) or the object is gone/corrupt (expired sweep,
	// early deletion, integrity failure).
	ErrReplayPayloadUnavailable = errors.New("channel: retained raw payload unavailable for replay")
	// ErrReplaySourceGone: the installation row the event's scope and
	// conversation mapping came from no longer exists.
	ErrReplaySourceGone = errors.New("channel: connector installation behind the inbox row is gone")
	// ErrReplayMalformed: the retained bytes no longer decode as the row's
	// platform (format drift between acceptance and replay).
	ErrReplayMalformed = errors.New("channel: retained raw payload no longer decodes as the row's platform")
	// ErrReplayMismatch: the retranslated event disagrees with its inbox row
	// (idempotency key or scope) — the object does not belong to the row.
	ErrReplayMismatch = errors.New("channel: retranslated event does not match its inbox row")
	// ErrReplayUnsupportedPlatform: the row's platform has no replay
	// translator (a committed row from an adapter this runtime cannot rebuild).
	ErrReplayUnsupportedPlatform = errors.New("channel: inbox row platform has no replay translator")
)

// InboxStore is the durable realization of the contract's InboundSink seam
// (Accept). It is the single writer for the connector inbox; connectors obtain
// an ACK verdict only after a row is durably present or a duplicate is
// recognized.
type InboxStore struct {
	db *gorm.DB
}

// NewInboxStore wires the inbox store to a GORM handle.
func NewInboxStore(db *gorm.DB) *InboxStore {
	return &InboxStore{db: db}
}

// Accept persists one normalized event before any platform ACK. A validation
// failure rejects the event (caller maps it to the platform's no-retry action);
// an accepted message event lands committed, an unknown event type lands
// quarantined, and a recognized duplicate is a successful no-op. The
// idempotency unique index — not driver-specific error translation — decides
// duplicate-versus-new, so the verdict holds on both postgres and sqlite.
func (s *InboxStore) Accept(ctx context.Context, ev ConnectorEvent) (AckDecision, error) {
	if err := ev.Validate(); err != nil {
		return "", err
	}
	status := InboxStatusCommitted
	decision := AckCommit
	if ev.QuarantineRequired() {
		status = InboxStatusQuarantined
		decision = AckQuarantine
	}
	messagePlatform := ""
	messageText := ""
	if ev.Message != nil {
		messagePlatform = ev.Message.PlatformID
		messageText = boundedTextSnapshot(ev.Message)
	}
	row := ChatConnectorInbox{
		TenantID:               ev.TenantID,
		WorkspaceID:            ev.WorkspaceID,
		IdempotencyKey:         ev.IdempotencyKey,
		Platform:               ev.Platform,
		InstallationID:         ev.InstallationID,
		EventID:                ev.EventID,
		EventType:              string(ev.EventType),
		Status:                 status,
		ConversationPlatformID: ev.Conversation.PlatformID,
		SenderPlatformID:       ev.Sender.PlatformID,
		MessagePlatformID:      messagePlatform,
		MessageText:            messageText,
		OccurredAt:             ev.OccurredAt,
		ReceivedAt:             ev.ReceivedAt,
	}
	if ev.RawPayloadRef != nil {
		row.RawPayloadStore = ev.RawPayloadRef.Store
		row.RawPayloadObjectID = ev.RawPayloadRef.ObjectID
		row.RawPayloadSHA256 = ev.RawPayloadRef.SHA256
	}
	res := s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{
			{Name: "tenant_id"},
			{Name: "workspace_id"},
			{Name: "idempotency_key"},
		},
		DoNothing: true,
	}).Create(&row)
	if res.Error != nil {
		return "", res.Error
	}
	if res.RowsAffected == 0 {
		return AckDuplicate, nil
	}
	return decision, nil
}

// ListQuarantined returns the quarantine lane for one tenant and workspace,
// newest first. The scope filter is part of every query, so cross-scope reads
// are structurally impossible (contract: access controls on source references).
func (s *InboxStore) ListQuarantined(ctx context.Context, tenantID, workspaceID string, limit int) ([]ChatConnectorInbox, error) {
	if limit <= 0 {
		limit = 50
	}
	var rows []ChatConnectorInbox
	err := s.db.WithContext(ctx).
		Where("tenant_id = ? AND workspace_id = ? AND status = ?", tenantID, workspaceID, InboxStatusQuarantined).
		Order("id DESC").
		Limit(limit).
		Find(&rows).Error
	if err != nil {
		return nil, err
	}
	return rows, nil
}

// Dismiss moves one quarantined row of the given scope to dismissed (the
// dead-letter triage disposition: the event stays stored as evidence but
// leaves the lane). The transition is one-way from quarantined; the scope
// filter is part of the statement, so another scope's row reads as missing.
func (s *InboxStore) Dismiss(ctx context.Context, tenantID, workspaceID string, id uint) error {
	res := s.db.WithContext(ctx).
		Model(&ChatConnectorInbox{}).
		Where("id = ? AND tenant_id = ? AND workspace_id = ? AND status = ?", id, tenantID, workspaceID, InboxStatusQuarantined).
		Updates(map[string]interface{}{"status": InboxStatusDismissed})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 1 {
		return nil
	}
	var row ChatConnectorInbox
	err := s.db.WithContext(ctx).
		Where("id = ? AND tenant_id = ? AND workspace_id = ?", id, tenantID, workspaceID).
		First(&row).Error
	if err != nil {
		return err // gorm.ErrRecordNotFound covers unknown and cross-scope ids
	}
	return ErrInboxNotQuarantined
}

// CommittedByID returns one row of the given scope that entered the normal
// flow. Unknown ids, foreign scopes, and quarantined/dismissed rows error out;
// the replay surface refuses to touch anything but committed rows.
func (s *InboxStore) CommittedByID(ctx context.Context, tenantID, workspaceID string, id uint) (*ChatConnectorInbox, error) {
	var row ChatConnectorInbox
	err := s.db.WithContext(ctx).
		Where("id = ? AND tenant_id = ? AND workspace_id = ?", id, tenantID, workspaceID).
		First(&row).Error
	if err != nil {
		return nil, err // gorm.ErrRecordNotFound covers unknown and cross-scope ids
	}
	if row.Status != InboxStatusCommitted {
		return nil, ErrInboxNotCommitted
	}
	return &row, nil
}

// ListUnforwarded returns the replay lane for one tenant and workspace:
// committed rows whose normal-flow delivery never settled, oldest first so a
// replay drains in arrival order. The scope filter is part of every query.
func (s *InboxStore) ListUnforwarded(ctx context.Context, tenantID, workspaceID string, limit int) ([]ChatConnectorInbox, error) {
	if limit <= 0 {
		limit = 50
	}
	var rows []ChatConnectorInbox
	err := s.db.WithContext(ctx).
		Where("tenant_id = ? AND workspace_id = ? AND status = ? AND forwarded_at IS NULL", tenantID, workspaceID, InboxStatusCommitted).
		Order("id ASC").
		Limit(limit).
		Find(&rows).Error
	if err != nil {
		return nil, err
	}
	return rows, nil
}

// MarkForwardedByKey stamps the delivery time onto the committed row the
// event's idempotency key names. It is the normal-flow forwarder's bookkeeping
// (live and replay share it); a missing row is a silent no-op because the
// caller's event can only come from that row.
func (s *InboxStore) MarkForwardedByKey(ctx context.Context, tenantID, workspaceID, idempotencyKey string, at time.Time) error {
	return s.db.WithContext(ctx).
		Model(&ChatConnectorInbox{}).
		Where("tenant_id = ? AND workspace_id = ? AND idempotency_key = ?", tenantID, workspaceID, idempotencyKey).
		Update("forwarded_at", at).Error
}
