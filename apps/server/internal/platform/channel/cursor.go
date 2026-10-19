package channel

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
)

// ChatConnectorCursor stores the poll/stream cursor for one installation
// (contract Phase 1 "cursor/ACK state"): the opaque platform token that lets
// the connector resume exactly where the previous fetch stopped. An empty
// stored cursor means a fresh poll from the platform's head.
type ChatConnectorCursor struct {
	ID             uint      `gorm:"primaryKey" json:"id"`
	TenantID       string    `gorm:"not null;uniqueIndex:uq_chat_connector_cursors_scope" json:"tenant_id"`
	WorkspaceID    string    `gorm:"not null;uniqueIndex:uq_chat_connector_cursors_scope" json:"workspace_id"`
	Platform       string    `gorm:"not null;uniqueIndex:uq_chat_connector_cursors_scope" json:"platform"`
	InstallationID string    `gorm:"not null;uniqueIndex:uq_chat_connector_cursors_scope" json:"installation_id"`
	Cursor         string    `gorm:"not null" json:"cursor"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// CursorScope keys one installation's cursor.
type CursorScope struct {
	TenantID       string
	WorkspaceID    string
	Platform       string
	InstallationID string
}

// CursorStore persists the contract's per-installation cursors. Cursors move
// only after the events they cover are durably accepted by the inbox, which is
// what makes redelivery after a crash at-least-once instead of lossy.
type CursorStore struct {
	db *gorm.DB
}

// NewCursorStore wires the cursor store to a GORM handle.
func NewCursorStore(db *gorm.DB) *CursorStore {
	return &CursorStore{db: db}
}

// Save stores the cursor for one installation; an empty cursor clears the
// stored state so the next fetch starts fresh. Callers advance one installation
// at a time, so the read-first update-or-create needs no extra locking.
func (s *CursorStore) Save(ctx context.Context, scope CursorScope, cursor string) error {
	if cursor == "" {
		return s.db.WithContext(ctx).
			Where(cursorClause(), scope.TenantID, scope.WorkspaceID, scope.Platform, scope.InstallationID).
			Delete(&ChatConnectorCursor{}).Error
	}
	var row ChatConnectorCursor
	err := s.db.WithContext(ctx).
		Where(cursorClause(), scope.TenantID, scope.WorkspaceID, scope.Platform, scope.InstallationID).
		First(&row).Error
	switch {
	case err == nil:
		return s.db.WithContext(ctx).Model(&row).
			Updates(map[string]interface{}{"cursor": cursor}).Error
	case errors.Is(err, gorm.ErrRecordNotFound):
		return s.db.WithContext(ctx).Create(&ChatConnectorCursor{
			TenantID:       scope.TenantID,
			WorkspaceID:    scope.WorkspaceID,
			Platform:       scope.Platform,
			InstallationID: scope.InstallationID,
			Cursor:         cursor,
		}).Error
	default:
		return err
	}
}

// Cursor returns the stored cursor for one installation; an empty string when
// none was saved (fresh poll) and never another scope's value.
func (s *CursorStore) Cursor(ctx context.Context, scope CursorScope) (string, error) {
	var row ChatConnectorCursor
	err := s.db.WithContext(ctx).
		Where(cursorClause(), scope.TenantID, scope.WorkspaceID, scope.Platform, scope.InstallationID).
		First(&row).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", nil
		}
		return "", err
	}
	return row.Cursor, nil
}

func cursorClause() string {
	return "tenant_id = ? AND workspace_id = ? AND platform = ? AND installation_id = ?"
}
