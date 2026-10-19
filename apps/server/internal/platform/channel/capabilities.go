package channel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
)

// ChatConnectorCapability stores the latest observed Capabilities snapshot for
// one installation and conversation. The contract requires the snapshot to come
// from the platform's authorization result — never an operator's unverified
// claim — so each observation overwrites the previous one and an absent or
// partially unknown snapshot keeps ingestion disabled (fail closed).
type ChatConnectorCapability struct {
	ID                     uint      `gorm:"primaryKey" json:"id"`
	TenantID               string    `gorm:"not null;uniqueIndex:uq_chat_connector_capabilities_scope" json:"tenant_id"`
	WorkspaceID            string    `gorm:"not null;uniqueIndex:uq_chat_connector_capabilities_scope" json:"workspace_id"`
	Platform               string    `gorm:"not null;uniqueIndex:uq_chat_connector_capabilities_scope" json:"platform"`
	InstallationID         string    `gorm:"not null;uniqueIndex:uq_chat_connector_capabilities_scope" json:"installation_id"`
	ConversationPlatformID string    `gorm:"not null;uniqueIndex:uq_chat_connector_capabilities_scope" json:"conversation_platform_id"`
	ReceiveMode            string    `gorm:"not null" json:"receive_mode"`
	MessageScope           string    `gorm:"not null" json:"message_scope"`
	HistoryRead            bool      `gorm:"not null;default:false" json:"history_read"`
	EventTypesJSON         string    `gorm:"not null" json:"event_types_json"`
	Threads                bool      `gorm:"not null;default:false" json:"threads"`
	Replies                bool      `gorm:"not null;default:false" json:"replies"`
	Attachments            bool      `gorm:"not null;default:false" json:"attachments"`
	Edits                  bool      `gorm:"not null;default:false" json:"edits"`
	Deletes                bool      `gorm:"not null;default:false" json:"deletes"`
	Reactions              bool      `gorm:"not null;default:false" json:"reactions"`
	SendText               bool      `gorm:"not null;default:false" json:"send_text"`
	SendRich               bool      `gorm:"not null;default:false" json:"send_rich"`
	RateLimit              string    `gorm:"not null" json:"rate_limit"`
	VerifiedAt             time.Time `json:"verified_at"`
	CreatedAt              time.Time `json:"created_at"`
	UpdatedAt              time.Time `json:"updated_at"`
}

// CapabilityScope keys one grant snapshot: an installation seen in one
// conversation is one grant.
type CapabilityScope struct {
	TenantID               string
	WorkspaceID            string
	Platform               string
	InstallationID         string
	ConversationPlatformID string
}

// CapabilityStore persists the contract's capability snapshots (Phase 1
// storage). It is written by the connector that observes the platform's
// authorization result and read by the ingestion gate through IngestionEnabled.
type CapabilityStore struct {
	db *gorm.DB
}

// NewCapabilityStore wires the capability store to a GORM handle.
func NewCapabilityStore(db *gorm.DB) *CapabilityStore {
	return &CapabilityStore{db: db}
}

// Record persists one observed snapshot, overwriting an earlier observation for
// the same scope so the newest platform grant is always authoritative. Callers
// run this per installation, so the read-first upsert needs no extra locking.
func (s *CapabilityStore) Record(ctx context.Context, scope CapabilityScope, caps Capabilities) error {
	existing, err := s.latestRow(ctx, scope)
	if err != nil {
		return err
	}
	values := map[string]interface{}{
		"receive_mode":     string(caps.ReceiveMode),
		"message_scope":    string(caps.MessageScope),
		"history_read":     caps.HistoryRead,
		"event_types_json": marshalEventTypes(caps.EventTypes),
		"threads":          caps.Threads,
		"replies":          caps.Replies,
		"attachments":      caps.Attachments,
		"edits":            caps.Edits,
		"deletes":          caps.Deletes,
		"reactions":        caps.Reactions,
		"send_text":        caps.SendText,
		"send_rich":        caps.SendRich,
		"rate_limit":       caps.RateLimit,
		"verified_at":      caps.VerifiedAt,
	}
	if existing != nil {
		return s.db.WithContext(ctx).Model(existing).Updates(values).Error
	}
	row := capabilityRow(scope, caps)
	return s.db.WithContext(ctx).Create(&row).Error
}

// Latest returns the newest observed snapshot for a scope, or nil when nothing
// was ever observed. A nil result is the fail-closed default: a zero
// Capabilities reports IngestionEnabled false.
func (s *CapabilityStore) Latest(ctx context.Context, scope CapabilityScope) (*Capabilities, error) {
	row, err := s.latestRow(ctx, scope)
	if err != nil || row == nil {
		return nil, err
	}
	caps := &Capabilities{
		ReceiveMode:  ReceiveMode(row.ReceiveMode),
		MessageScope: MessageScope(row.MessageScope),
		HistoryRead:  row.HistoryRead,
		Threads:      row.Threads,
		Replies:      row.Replies,
		Attachments:  row.Attachments,
		Edits:        row.Edits,
		Deletes:      row.Deletes,
		Reactions:    row.Reactions,
		SendText:     row.SendText,
		SendRich:     row.SendRich,
		RateLimit:    row.RateLimit,
		VerifiedAt:   row.VerifiedAt,
	}
	if err := json.Unmarshal([]byte(row.EventTypesJSON), &caps.EventTypes); err != nil {
		return nil, fmt.Errorf("capability snapshot has corrupt event types: %w", err)
	}
	return caps, nil
}

func (s *CapabilityStore) latestRow(ctx context.Context, scope CapabilityScope) (*ChatConnectorCapability, error) {
	var row ChatConnectorCapability
	err := s.db.WithContext(ctx).
		Where(scopeClause(), scope.TenantID, scope.WorkspaceID, scope.Platform,
			scope.InstallationID, scope.ConversationPlatformID).
		First(&row).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &row, nil
}

func scopeClause() string {
	return "tenant_id = ? AND workspace_id = ? AND platform = ? AND installation_id = ? AND conversation_platform_id = ?"
}

func capabilityRow(scope CapabilityScope, caps Capabilities) ChatConnectorCapability {
	return ChatConnectorCapability{
		TenantID:               scope.TenantID,
		WorkspaceID:            scope.WorkspaceID,
		Platform:               scope.Platform,
		InstallationID:         scope.InstallationID,
		ConversationPlatformID: scope.ConversationPlatformID,
		ReceiveMode:            string(caps.ReceiveMode),
		MessageScope:           string(caps.MessageScope),
		HistoryRead:            caps.HistoryRead,
		EventTypesJSON:         marshalEventTypes(caps.EventTypes),
		Threads:                caps.Threads,
		Replies:                caps.Replies,
		Attachments:            caps.Attachments,
		Edits:                  caps.Edits,
		Deletes:                caps.Deletes,
		Reactions:              caps.Reactions,
		SendText:               caps.SendText,
		SendRich:               caps.SendRich,
		RateLimit:              caps.RateLimit,
		VerifiedAt:             caps.VerifiedAt,
	}
}

// marshalEventTypes renders the event vocabulary list for storage. A
// []EventType is a slice of strings, so json.Marshal cannot fail; the error is
// deliberately discarded rather than written as dead handling code.
func marshalEventTypes(types []EventType) string {
	out, _ := json.Marshal(types)
	return string(out)
}
