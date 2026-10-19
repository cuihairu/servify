package channel

import (
	"context"
	"errors"
	"testing"
	"time"

	"gorm.io/gorm"
)

func capScope() CapabilityScope {
	return CapabilityScope{
		TenantID:               "tenant-1",
		WorkspaceID:            "workspace-1",
		Platform:               "memex",
		InstallationID:         "inst-1",
		ConversationPlatformID: "grp-1",
	}
}

func observedCaps() Capabilities {
	return Capabilities{
		ReceiveMode:  ReceiveModePolling,
		MessageScope: MessageScopeMentionOnly,
		HistoryRead:  true,
		EventTypes:   []EventType{EventTypeMessageCreated, EventTypeMessageEdited},
		Threads:      true,
		Replies:      true,
		Attachments:  true,
		Edits:        true,
		Deletes:      false,
		Reactions:    true,
		SendText:     true,
		SendRich:     false,
		RateLimit:    "30/min",
		VerifiedAt:   time.Date(2026, 10, 20, 8, 0, 0, 0, time.UTC),
	}
}

func TestCapabilityRecordAndLatest(t *testing.T) {
	store := NewCapabilityStore(newChannelTestDB(t))
	ctx := context.Background()

	got, err := store.Latest(ctx, capScope())
	if err != nil {
		t.Fatalf("latest before record: %v", err)
	}
	if got != nil {
		t.Errorf("latest before record = %+v, want nil (fail-closed)", got)
	}

	if err := store.Record(ctx, capScope(), observedCaps()); err != nil {
		t.Fatalf("record: %v", err)
	}
	got, err = store.Latest(ctx, capScope())
	if err != nil {
		t.Fatalf("latest: %v", err)
	}
	if got == nil {
		t.Fatal("latest after record = nil, want snapshot")
	}
	if got.ReceiveMode != ReceiveModePolling || got.MessageScope != MessageScopeMentionOnly ||
		!got.HistoryRead || !got.Threads || !got.Replies || !got.Attachments || !got.Edits ||
		got.Deletes || !got.Reactions || !got.SendText || got.SendRich || got.RateLimit != "30/min" {
		t.Errorf("snapshot fields drifted: %+v", got)
	}
	if len(got.EventTypes) != 2 || got.EventTypes[0] != EventTypeMessageCreated ||
		got.EventTypes[1] != EventTypeMessageEdited {
		t.Errorf("event types = %v, want [created edited]", got.EventTypes)
	}
	if !got.VerifiedAt.Equal(observedCaps().VerifiedAt) {
		t.Errorf("verified at = %v, want %v", got.VerifiedAt, observedCaps().VerifiedAt)
	}
	if !got.IngestionEnabled() {
		t.Error("recorded observed snapshot must pass IngestionEnabled")
	}
}

func TestCapabilityRecordOverwritesWithNewestObservation(t *testing.T) {
	store := NewCapabilityStore(newChannelTestDB(t))
	ctx := context.Background()

	if err := store.Record(ctx, capScope(), observedCaps()); err != nil {
		t.Fatalf("record first: %v", err)
	}
	revoked := observedCaps()
	revoked.ReceiveMode = ReceiveModeUnknown
	revoked.EventTypes = nil
	revoked.VerifiedAt = observedCaps().VerifiedAt.Add(time.Minute)
	if err := store.Record(ctx, capScope(), revoked); err != nil {
		t.Fatalf("record second: %v", err)
	}

	got, err := store.Latest(ctx, capScope())
	if err != nil {
		t.Fatalf("latest: %v", err)
	}
	if got == nil || got.ReceiveMode != ReceiveModeUnknown || len(got.EventTypes) != 0 {
		t.Errorf("latest observation did not win: %+v", got)
	}
	if got.IngestionEnabled() {
		t.Error("unknown receive mode must keep ingestion disabled")
	}
}

func TestCapabilityRecordReadError(t *testing.T) {
	db := newChannelTestDB(t)
	store := NewCapabilityStore(db)
	if err := db.Callback().Query().Before("gorm:query").Register("cap_read_error",
		func(tx *gorm.DB) { tx.AddError(errors.New("boom")) }); err != nil {
		t.Fatalf("register callback: %v", err)
	}
	if err := store.Record(context.Background(), capScope(), observedCaps()); err == nil {
		t.Error("record: expected read error")
	}
}

func TestCapabilityRecordCreateError(t *testing.T) {
	db := newChannelTestDB(t)
	store := NewCapabilityStore(db)
	if err := db.Callback().Create().Before("gorm:create").Register("cap_create_error",
		func(tx *gorm.DB) { tx.AddError(errors.New("boom")) }); err != nil {
		t.Fatalf("register callback: %v", err)
	}
	if err := store.Record(context.Background(), capScope(), observedCaps()); err == nil {
		t.Error("record: expected create error")
	}
}

func TestCapabilityRecordUpdateError(t *testing.T) {
	db := newChannelTestDB(t)
	store := NewCapabilityStore(db)
	ctx := context.Background()
	if err := store.Record(ctx, capScope(), observedCaps()); err != nil {
		t.Fatalf("record first: %v", err)
	}
	if err := db.Callback().Update().Before("gorm:update").Register("cap_update_error",
		func(tx *gorm.DB) { tx.AddError(errors.New("boom")) }); err != nil {
		t.Fatalf("register callback: %v", err)
	}
	if err := store.Record(ctx, capScope(), observedCaps()); err == nil {
		t.Error("record overwrite: expected update error")
	}
}

func TestCapabilityLatestCorruptEventTypes(t *testing.T) {
	db := newChannelTestDB(t)
	store := NewCapabilityStore(db)
	if err := db.Create(&ChatConnectorCapability{
		TenantID:               "tenant-1",
		WorkspaceID:            "workspace-1",
		Platform:               "memex",
		InstallationID:         "inst-1",
		ConversationPlatformID: "grp-1",
		ReceiveMode:            string(ReceiveModePolling),
		MessageScope:           string(MessageScopeMentionOnly),
		EventTypesJSON:         "{not json",
	}).Error; err != nil {
		t.Fatalf("seed corrupt row: %v", err)
	}
	if _, err := store.Latest(context.Background(), capScope()); err == nil {
		t.Error("latest: expected corrupt event types error")
	}
}

func TestCapabilityLatestQueryError(t *testing.T) {
	db := newChannelTestDB(t)
	store := NewCapabilityStore(db)
	if err := db.Callback().Query().Before("gorm:query").Register("cap_query_error",
		func(tx *gorm.DB) { tx.AddError(errors.New("boom")) }); err != nil {
		t.Fatalf("register callback: %v", err)
	}
	if _, err := store.Latest(context.Background(), capScope()); err == nil {
		t.Error("latest: expected query error")
	}
}
