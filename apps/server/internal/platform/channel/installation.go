package channel

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
)

// ChatConnectorInstallation is the stored installation mapping one adapter
// installation to a Servify scope (contract: "tenant/workspace scope must be
// established from the stored installation mapping, not trusted from incoming
// payload"). It never holds a credential — the token's environment variable
// NAME does, so credentials stay supplied at runtime and the row stays
// pasteable into logs and support bundles.
//
// One installation maps to one conversation: Memex updates carry no group id
// (their `to` field is the bot itself), so a bot serving several groups needs
// several installations (decided 2026-10-20).
type ChatConnectorInstallation struct {
	ID             uint   `gorm:"primaryKey" json:"id"`
	TenantID       string `gorm:"not null;uniqueIndex:uq_chat_connector_installations_scope" json:"tenant_id"`
	WorkspaceID    string `gorm:"not null;uniqueIndex:uq_chat_connector_installations_scope" json:"workspace_id"`
	Platform       string `gorm:"not null;uniqueIndex:uq_chat_connector_installations_scope" json:"platform"`
	InstallationID string `gorm:"not null;uniqueIndex:uq_chat_connector_installations_scope" json:"installation_id"`

	ConversationPlatformID string `gorm:"not null" json:"conversation_platform_id"`
	ConversationKind       string `gorm:"not null" json:"conversation_kind"`
	ConversationIsGroup    bool   `gorm:"not null" json:"conversation_is_group"`

	// UpdatesEndpoint is this installation's bot API base URL.
	UpdatesEndpoint string `gorm:"not null" json:"updates_endpoint"`
	// TokenEnvVar names the environment variable holding the bot token.
	TokenEnvVar string `gorm:"not null" json:"token_env_var"`
	// LongPollMS is the per-pull server-side long poll budget; 0 would mean
	// immediate drain, i.e. a hot loop, so it is rejected by Validate.
	LongPollMS int `gorm:"not null;default:25000" json:"long_poll_ms"`
	// Enabled fails closed: a stored row starts inert until an operator
	// turns it on.
	Enabled bool `gorm:"not null;default:false" json:"enabled"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// longPollCapMS is the shared long-poll ceiling for stored installations
// (memex 30s server cap; telegram long polls in seconds up to 50, so a single
// 30ms-based column serves both adapters).
const longPollCapMS = 30000

// DefaultLongPollMS is the long-poll budget the stored column defaults to, and
// what an omitted long_poll_ms in a write request gets. It is exported once
// here so the storage default and the HTTP write surface cannot drift apart.
const DefaultLongPollMS = 25000

// InstallationScope keys one stored installation.
type InstallationScope struct {
	TenantID       string
	WorkspaceID    string
	Platform       string
	InstallationID string
}

// Validate keeps malformed or unsafe wiring out of the table: every identity
// and scope field must be present, and the long poll budget must stay inside
// the platform's range with a positive lower bound.
func (i ChatConnectorInstallation) Validate() error {
	if i.TenantID == "" {
		return errors.New("installation tenant id is required")
	}
	if i.WorkspaceID == "" {
		return errors.New("installation workspace id is required")
	}
	if i.Platform == "" {
		return errors.New("installation platform is required")
	}
	if i.InstallationID == "" {
		return errors.New("installation id is required")
	}
	if i.ConversationPlatformID == "" {
		return errors.New("installation conversation id is required")
	}
	if i.ConversationKind == "" {
		return errors.New("installation conversation kind is required")
	}
	if i.UpdatesEndpoint == "" {
		return errors.New("installation updates endpoint is required")
	}
	if i.TokenEnvVar == "" {
		return errors.New("installation token env var is required")
	}
	if i.LongPollMS <= 0 || i.LongPollMS > longPollCapMS {
		return fmt.Errorf("installation long poll %dms must be within 1..%dms",
			i.LongPollMS, longPollCapMS)
	}
	return nil
}

// InstallationStore persists the stored installation mapping. Every read is
// forced to a tenant/workspace scope, so wiring cannot be read across tenants.
type InstallationStore struct {
	db *gorm.DB
}

// NewInstallationStore wires the installation store to a GORM handle.
func NewInstallationStore(db *gorm.DB) *InstallationStore {
	return &InstallationStore{db: db}
}

// Save reads first and then updates or creates, so one installation keeps a
// single row and its created_at never moves. Invalid configs are rejected
// before they can land.
func (s *InstallationStore) Save(ctx context.Context, inst ChatConnectorInstallation) (ChatConnectorInstallation, error) {
	if err := inst.Validate(); err != nil {
		return ChatConnectorInstallation{}, err
	}
	scope := InstallationScope{
		TenantID: inst.TenantID, WorkspaceID: inst.WorkspaceID,
		Platform: inst.Platform, InstallationID: inst.InstallationID,
	}
	var existing ChatConnectorInstallation
	if err := s.db.WithContext(ctx).Where(
		"tenant_id = ? AND workspace_id = ? AND platform = ? AND installation_id = ?",
		scope.TenantID, scope.WorkspaceID, scope.Platform, scope.InstallationID).
		Take(&existing).Error; err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return ChatConnectorInstallation{}, err
		}
		if err := s.db.WithContext(ctx).Create(&inst).Error; err != nil {
			return ChatConnectorInstallation{}, err
		}
		return inst, nil
	}
	inst.ID = existing.ID
	inst.CreatedAt = existing.CreatedAt
	// Column list, not a struct: GORM's Updates drops zero-valued fields,
	// which would make "turn the installation off" silently no-op.
	if err := s.db.WithContext(ctx).Model(&existing).Updates(map[string]interface{}{
		"conversation_platform_id": inst.ConversationPlatformID,
		"conversation_kind":        inst.ConversationKind,
		"conversation_is_group":    inst.ConversationIsGroup,
		"updates_endpoint":         inst.UpdatesEndpoint,
		"token_env_var":            inst.TokenEnvVar,
		"long_poll_ms":             inst.LongPollMS,
		"enabled":                  inst.Enabled,
	}).Error; err != nil {
		return ChatConnectorInstallation{}, err
	}
	return inst, nil
}

// List returns one tenant/workspace's stored installations; the scope filter
// is mandatory so a query cannot reach another tenant's wiring.
func (s *InstallationStore) List(ctx context.Context, tenantID, workspaceID string) ([]ChatConnectorInstallation, error) {
	var rows []ChatConnectorInstallation
	if err := s.db.WithContext(ctx).Where("tenant_id = ? AND workspace_id = ?", tenantID, workspaceID).
		Order("platform ASC, installation_id ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

// ListEnabled returns every enabled installation across all tenants/
// workspaces; it backs the service-startup poll loop, which must start exactly
// the rows an operator turned on (Enabled defaults false = fail closed) and
// read each row's scope from storage, never from its payload. The ordering is
// stable so restarts attach runners in a deterministic order.
func (s *InstallationStore) ListEnabled(ctx context.Context) ([]ChatConnectorInstallation, error) {
	var rows []ChatConnectorInstallation
	if err := s.db.WithContext(ctx).Where("enabled = ?", true).
		Order("tenant_id ASC, workspace_id ASC, platform ASC, installation_id ASC").
		Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

// Get returns one stored installation; a missing row yields (nil, nil) so a
// future manager can treat "nothing configured" as nothing to run, not an
// error.
func (s *InstallationStore) Get(ctx context.Context, scope InstallationScope) (*ChatConnectorInstallation, error) {
	var row ChatConnectorInstallation
	if err := s.db.WithContext(ctx).Where(
		"tenant_id = ? AND workspace_id = ? AND platform = ? AND installation_id = ?",
		scope.TenantID, scope.WorkspaceID, scope.Platform, scope.InstallationID).
		Take(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &row, nil
}
