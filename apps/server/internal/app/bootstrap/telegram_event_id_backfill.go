package bootstrap

import (
	"fmt"

	"servify/apps/server/internal/platform/channel"

	"gorm.io/gorm"
)

// BackfillTelegramEventIDs rewrites inbox rows accepted before the Telegram
// EventID format change (docs/chat-platform-integration-contract.md). The
// adapter now composes event identity as chatID:messageID
// (platform/channel/telegram/adapter.go), but rows already committed under the
// legacy bare-messageID key keep the old shape: a crash-redelivered update
// recomputes the new key, is judged a new row and forwards again (at-least-once
// broken), and replay of a committed-but-unforwarded legacy row returns
// ErrReplayMismatch. This rewrites the legacy rows so the key matches what the
// adapter computes.
//
// Two execution paths keep identical semantics:
//   - PostgreSQL versioned: migrations/000031_...up.sql;
//   - sqlite / AutoMigrate path: this function (wired in RunStandalone and
//     cmd/migrate after AutoMigrate).
//
// Idempotent: only telegram message.created rows whose event_id is a bare
// message id (all digits) are touched; reruns change nothing and an empty
// database is a natural no-op. A legacy row whose target key is already
// occupied by a chat-scoped row (the same update landed once before and once
// after the format change) is deleted — the surviving chat-scoped row is the
// one that entered the normal flow, and its retained object is swept by
// retention. updated_at is not refreshed (backfill convention, matching
// 000017_scope_backfill).
func BackfillTelegramEventIDs(db *gorm.DB) (int64, error) {
	if db == nil {
		return 0, nil
	}
	var rows []channel.ChatConnectorInbox
	if err := db.Where("platform = ? AND event_type = ?", "telegram", string(channel.EventTypeMessageCreated)).
		Find(&rows).Error; err != nil {
		return 0, fmt.Errorf("backfill telegram event ids: %w", err)
	}

	// Occupied target keys, scoped by tenant+workspace+idempotency key to match
	// the unique index (uq_chat_connector_inboxes_scope_key). A legacy row whose
	// target is occupied is the stale duplicate of the same update (it landed
	// once before and once after the format change); the surviving scoped row
	// is the one that entered the normal flow. Only non-legacy rows count as
	// occupiers — a legacy row's target always contains ':' so it can never be
	// another legacy row's target.
	type scopeKey struct{ tenant, workspace, key string }
	occupied := make(map[scopeKey]bool, len(rows))
	for _, r := range rows {
		if !allDigits(r.EventID) {
			occupied[scopeKey{r.TenantID, r.WorkspaceID, r.IdempotencyKey}] = true
		}
	}

	var touched int64
	for _, row := range rows {
		if !allDigits(row.EventID) {
			continue
		}
		newEventID := row.ConversationPlatformID + ":" + row.EventID
		newKey := channel.InboundIdempotencyKey(row.InstallationID, newEventID)
		if occupied[scopeKey{row.TenantID, row.WorkspaceID, newKey}] {
			if err := db.Where("id = ?", row.ID).Delete(&channel.ChatConnectorInbox{}).Error; err != nil {
				return touched, fmt.Errorf("backfill telegram event ids: %w", err)
			}
			touched++
			continue
		}
		res := db.Model(&channel.ChatConnectorInbox{}).Where("id = ?", row.ID).
			UpdateColumns(map[string]interface{}{"event_id": newEventID, "idempotency_key": newKey})
		if res.Error != nil {
			return touched, fmt.Errorf("backfill telegram event ids: %w", res.Error)
		}
		touched += res.RowsAffected
	}
	return touched, nil
}

// allDigits reports whether s is a non-empty string of ASCII digits only. It
// matches the migration's ^[0-9]+$ predicate on both dialects without leaning
// on dialect-specific regex operators.
func allDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}
