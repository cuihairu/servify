package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"servify/apps/server/internal/platform/channel"
	"servify/apps/server/internal/platform/channel/memex"
	"servify/apps/server/internal/platform/channel/telegram"
)

// ChatInboxReplayer replays a committed-but-unforwarded inbox row back into
// the normal flow (contract: replay/dead-letter handling). The inbox row is
// deliberately content-free, so the event is reconstructed from the retained
// raw payload: open the object, decode it as the row's platform, retranslate
// with the stored installation's scope, verify it still matches the row, then
// drive it through the very forward the live poller used. The shared forward
// stamps forwarded on success, so a replayed row leaves the replay lane
// exactly like a first delivery would.
//
// Delivery stays at-least-once: a row whose forward succeeded but whose
// forward-stamp failed replays into a duplicate conversation message. That is
// the same window the live path has between ingest and stamp.
type ChatInboxReplayer struct {
	inbox    *channel.InboxStore
	installs *channel.InstallationStore
	payloads *channel.LocalPayloadStore
	forward  func(ctx context.Context, ev channel.ConnectorEvent) error
}

// ListUnforwarded returns the replay lane for the caller's scope (committed
// rows whose delivery never settled), oldest first.
func (r *ChatInboxReplayer) ListUnforwarded(ctx context.Context, tenantID, workspaceID string, limit int) ([]channel.ChatConnectorInbox, error) {
	if r == nil || r.inbox == nil {
		return nil, channel.ErrReplayPayloadUnavailable
	}
	return r.inbox.ListUnforwarded(ctx, tenantID, workspaceID, limit)
}

// ReplayInboxRow reconstructs one committed row's event from its retained
// payload and drives it through the normal flow. Every refusal is a sentinel
// (no payload content ever reaches the error surface).
func (r *ChatInboxReplayer) ReplayInboxRow(ctx context.Context, tenantID, workspaceID string, id uint) error {
	if r == nil || r.inbox == nil || r.forward == nil {
		return channel.ErrReplayPayloadUnavailable
	}
	row, err := r.inbox.CommittedByID(ctx, tenantID, workspaceID, id)
	if err != nil {
		return err
	}
	ev, err := r.reconstruct(ctx, tenantID, workspaceID, row)
	if err != nil {
		return err
	}
	return r.forward(ctx, ev)
}

// reconstruct rebuilds the event from the retained object. The scope argument
// comes from the authenticated caller, the row scope is asserted against the
// retranslated event, and the translator scope comes from the stored
// installation row — replay cannot widen a scope by replaying.
func (r *ChatInboxReplayer) reconstruct(ctx context.Context, tenantID, workspaceID string, row *channel.ChatConnectorInbox) (channel.ConnectorEvent, error) {
	if r.payloads == nil || row.RawPayloadStore == "" || row.RawPayloadObjectID == "" {
		return channel.ConnectorEvent{}, channel.ErrReplayPayloadUnavailable
	}
	data, err := r.payloads.Open(ctx, channel.RawPayloadRef{
		Store:    row.RawPayloadStore,
		ObjectID: row.RawPayloadObjectID,
		SHA256:   row.RawPayloadSHA256,
	})
	if err != nil {
		if errors.Is(err, channel.ErrPayloadNotFound) ||
			errors.Is(err, channel.ErrPayloadCorrupt) ||
			errors.Is(err, channel.ErrPayloadStoreMismatch) {
			return channel.ConnectorEvent{}, fmt.Errorf("%w: %v", channel.ErrReplayPayloadUnavailable, err)
		}
		return channel.ConnectorEvent{}, err
	}
	inst, err := r.installs.Get(ctx, channel.InstallationScope{
		TenantID:       tenantID,
		WorkspaceID:    workspaceID,
		Platform:       row.Platform,
		InstallationID: row.InstallationID,
	})
	if err != nil {
		return channel.ConnectorEvent{}, err
	}
	if inst == nil {
		return channel.ConnectorEvent{}, channel.ErrReplaySourceGone
	}
	ev, err := retranslate(row.Platform, *inst, row.ReceivedAt, data)
	if err != nil {
		return channel.ConnectorEvent{}, err
	}
	if ev.IdempotencyKey != row.IdempotencyKey ||
		ev.TenantID != row.TenantID ||
		ev.WorkspaceID != row.WorkspaceID {
		return channel.ConnectorEvent{}, channel.ErrReplayMismatch
	}
	if err := ev.Validate(); err != nil {
		return channel.ConnectorEvent{}, fmt.Errorf("%w: %v", channel.ErrReplayMalformed, err)
	}
	return ev, nil
}

// retranslate decodes the retained bytes as the row's platform and runs the
// platform translator with the installation's scope — the same translate step
// the live poll used, which is what makes the replay land in the normal flow.
func retranslate(platform string, inst channel.ChatConnectorInstallation, receivedAt time.Time, data []byte) (channel.ConnectorEvent, error) {
	switch platform {
	case platformTelegram:
		var u telegram.Update
		if err := json.Unmarshal(data, &u); err != nil {
			return channel.ConnectorEvent{}, fmt.Errorf("%w: decode telegram update: %v", channel.ErrReplayMalformed, err)
		}
		return telegram.Translator{Config: telegramConfigFromInstallation(inst)}.Event(receivedAt, u), nil
	case platformMemex:
		var u memex.Update
		if err := json.Unmarshal(data, &u); err != nil {
			return channel.ConnectorEvent{}, fmt.Errorf("%w: decode memex update: %v", channel.ErrReplayMalformed, err)
		}
		return memex.Translator{Config: memexConfigFromInstallation(inst)}.Event(receivedAt, u), nil
	default:
		return channel.ConnectorEvent{}, fmt.Errorf("%w: %q", channel.ErrReplayUnsupportedPlatform, platform)
	}
}
