package channel

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

var inboxTestDBSeq atomic.Uint64

func newChannelTestDB(t *testing.T) *gorm.DB {
	dsn := "file:inbox_" + strings.ReplaceAll(t.Name(), "/", "_") + "_" +
		strconv.FormatUint(inboxTestDBSeq.Add(1), 10) + "?mode=memory&cache=shared"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if sqlDB, err := db.DB(); err == nil {
		sqlDB.SetMaxOpenConns(1)
		t.Cleanup(func() { _ = sqlDB.Close() })
	}
	if err := db.AutoMigrate(&ChatConnectorInbox{}, &ChatConnectorCapability{}, &ChatConnectorCursor{}); err != nil {
		t.Fatalf("migrate connector tables: %v", err)
	}
	return db
}

func TestInboxAccept(t *testing.T) {
	store := NewInboxStore(newChannelTestDB(t))
	stored, err := store.Accept(context.Background(), messageEvent("evt-1"))
	if err != nil {
		t.Fatalf("accept committed: %v", err)
	}
	if stored != AckCommit {
		t.Errorf("accept committed = %v, want %v", stored, AckCommit)
	}
	if got := inboxCount(t, store); got != 1 {
		t.Errorf("inbox rows = %d, want 1", got)
	}
	row := inboxByKey(t, store, messageEvent("evt-1").IdempotencyKey)
	if row.Status != InboxStatusCommitted {
		t.Errorf("status = %q, want committed", row.Status)
	}
	if row.ConversationPlatformID != "grp-1" || row.SenderPlatformID != "user-1" ||
		row.MessagePlatformID != "msg-1" {
		t.Errorf("source refs = %q/%q/%q, want grp-1/user-1/msg-1",
			row.ConversationPlatformID, row.SenderPlatformID, row.MessagePlatformID)
	}

	stored, err = store.Accept(context.Background(), unknownEvent("evt-2"))
	if err != nil {
		t.Fatalf("accept unknown: %v", err)
	}
	if stored != AckQuarantine {
		t.Errorf("accept unknown = %v, want %v", stored, AckQuarantine)
	}
	quarantined := inboxByKey(t, store, unknownEvent("evt-2").IdempotencyKey)
	if quarantined.Status != InboxStatusQuarantined {
		t.Errorf("unknown status = %q, want quarantined", quarantined.Status)
	}
	if quarantined.RawPayloadStore != "local" || quarantined.RawPayloadObjectID != "obj-evt-2" {
		t.Errorf("raw ref = %q/%q, want local/obj-evt-2", quarantined.RawPayloadStore, quarantined.RawPayloadObjectID)
	}

	stored, err = store.Accept(context.Background(), messageEvent("evt-1"))
	if err != nil {
		t.Fatalf("accept duplicate: %v", err)
	}
	if stored != AckDuplicate {
		t.Errorf("accept duplicate = %v, want %v", stored, AckDuplicate)
	}
	if got := inboxCount(t, store); got != 2 {
		t.Errorf("inbox rows after duplicate = %d, want 2", got)
	}

	bad := messageEvent("evt-3")
	bad.IdempotencyKey = "tampered"
	if _, err = store.Accept(context.Background(), bad); err == nil {
		t.Error("accept invalid event: expected validation error")
	}
	if got := inboxCount(t, store); got != 2 {
		t.Errorf("inbox rows after invalid = %d, want 2", got)
	}
}

func TestInboxAcceptCreateError(t *testing.T) {
	store := NewInboxStore(newChannelTestDB(t))
	if err := store.db.Callback().Create().Before("gorm:create").Register("inbox_accept_error",
		func(tx *gorm.DB) { tx.AddError(errors.New("boom")) }); err != nil {
		t.Fatalf("register callback: %v", err)
	}
	if _, err := store.Accept(context.Background(), messageEvent("evt-1")); err == nil {
		t.Error("accept: expected create error")
	}
	if got := inboxCount(t, store); got != 0 {
		t.Errorf("inbox rows after failed create = %d, want 0", got)
	}
}

func TestInboxListQuarantined(t *testing.T) {
	store := NewInboxStore(newChannelTestDB(t))
	for _, eventID := range []string{"q1", "q2", "q3"} {
		if _, err := store.Accept(context.Background(), unknownEvent(eventID)); err != nil {
			t.Fatalf("accept %s: %v", eventID, err)
		}
	}
	if _, err := store.Accept(context.Background(), messageEvent("committed")); err != nil {
		t.Fatalf("accept committed: %v", err)
	}
	otherTenant := unknownEvent("other-tenant")
	otherTenant.TenantID = "tenant-2"
	if _, err := store.Accept(context.Background(), otherTenant); err != nil {
		t.Fatalf("accept other tenant: %v", err)
	}
	otherWorkspace := unknownEvent("other-workspace")
	otherWorkspace.WorkspaceID = "workspace-2"
	if _, err := store.Accept(context.Background(), otherWorkspace); err != nil {
		t.Fatalf("accept other workspace: %v", err)
	}

	rows, err := store.ListQuarantined(context.Background(), "tenant-1", "workspace-1", 2)
	if err != nil {
		t.Fatalf("list quarantined: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("list quarantined = %d rows, want 2 (limit)", len(rows))
	}
	for _, row := range rows {
		if row.Status != InboxStatusQuarantined || row.TenantID != "tenant-1" ||
			row.WorkspaceID != "workspace-1" {
			t.Errorf("unexpected row %s %s %s %s", row.EventID, row.Status, row.TenantID, row.WorkspaceID)
		}
	}
	if rows[0].ID <= rows[1].ID {
		t.Errorf("rows not in newest-first order: %d, %d", rows[0].ID, rows[1].ID)
	}

	all, err := store.ListQuarantined(context.Background(), "tenant-1", "workspace-1", 0)
	if err != nil {
		t.Fatalf("list all: %v", err)
	}
	if len(all) != 3 {
		t.Errorf("list all quarantined = %d rows, want 3", len(all))
	}
}

func TestInboxListQuarantinedQueryError(t *testing.T) {
	store := NewInboxStore(newChannelTestDB(t))
	if err := store.db.Callback().Query().After("gorm:query").Register("inbox_query_error",
		func(tx *gorm.DB) { tx.AddError(errors.New("boom")) }); err != nil {
		t.Fatalf("register callback: %v", err)
	}
	if _, err := store.ListQuarantined(context.Background(), "tenant-1", "workspace-1", 10); err == nil {
		t.Error("list quarantined: expected query error")
	}
}

func inboxCount(t *testing.T, store *InboxStore) int {
	t.Helper()
	var count int64
	if err := store.db.Model(&ChatConnectorInbox{}).Count(&count).Error; err != nil {
		t.Fatalf("count inbox: %v", err)
	}
	return int(count)
}

func inboxByKey(t *testing.T, store *InboxStore, key string) ChatConnectorInbox {
	t.Helper()
	var row ChatConnectorInbox
	if err := store.db.Where("idempotency_key = ?", key).First(&row).Error; err != nil {
		t.Fatalf("find inbox row %q: %v", key, err)
	}
	return row
}

// messageEvent builds a valid message.created event with a message payload and
// no retained raw payload reference.
func messageEvent(eventID string) ConnectorEvent {
	ev := validBaseEvent()
	ev.EventID = eventID
	ev.IdempotencyKey = InboundIdempotencyKey("inst-1", eventID)
	return ev
}

// unknownEvent builds a valid quarantined unknown event with a retained raw
// payload reference and no message payload.
func unknownEvent(eventID string) ConnectorEvent {
	ev := validBaseEvent()
	ev.EventID = eventID
	ev.EventType = EventTypeUnknown
	ev.IdempotencyKey = InboundIdempotencyKey("inst-1", eventID)
	ev.Message = nil
	ev.RawPayloadRef = &RawPayloadRef{
		Store:     "local",
		ObjectID:  "obj-" + eventID,
		SHA256:    "deadbeef",
		ExpiresAt: time.Now().Add(24 * time.Hour),
	}
	return ev
}
