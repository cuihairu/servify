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
	ID                     uint      `gorm:"primaryKey" json:"id"`
	TenantID               string    `gorm:"not null;uniqueIndex:uq_chat_connector_inboxes_scope_key" json:"tenant_id"`
	WorkspaceID            string    `gorm:"not null;uniqueIndex:uq_chat_connector_inboxes_scope_key" json:"workspace_id"`
	IdempotencyKey         string    `gorm:"not null;uniqueIndex:uq_chat_connector_inboxes_scope_key" json:"idempotency_key"`
	Platform               string    `gorm:"not null" json:"platform"`
	InstallationID         string    `gorm:"index;not null" json:"installation_id"`
	EventID                string    `gorm:"not null" json:"event_id"`
	EventType              string    `gorm:"not null" json:"event_type"`
	Status                 string    `gorm:"not null" json:"status"` // committed | quarantined | dismissed
	ConversationPlatformID string    `gorm:"not null" json:"conversation_platform_id"`
	SenderPlatformID       string    `gorm:"not null" json:"sender_platform_id"`
	MessagePlatformID      string    `gorm:"not null" json:"message_platform_id"`
	RawPayloadStore        string    `json:"raw_payload_store"`
	RawPayloadObjectID     string    `json:"raw_payload_object_id"`
	OccurredAt             time.Time `json:"occurred_at"`
	ReceivedAt             time.Time `json:"received_at"`
	CreatedAt              time.Time `json:"created_at"`
	UpdatedAt              time.Time `json:"updated_at"`
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

// ErrInboxNotQuarantined is returned when the row exists in the requested
// scope but is not in the quarantine lane (committed, or already dismissed).
var ErrInboxNotQuarantined = errors.New("channel: inbox row is not quarantined")

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
	if ev.Message != nil {
		messagePlatform = ev.Message.PlatformID
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
		OccurredAt:             ev.OccurredAt,
		ReceivedAt:             ev.ReceivedAt,
	}
	if ev.RawPayloadRef != nil {
		row.RawPayloadStore = ev.RawPayloadRef.Store
		row.RawPayloadObjectID = ev.RawPayloadRef.ObjectID
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
