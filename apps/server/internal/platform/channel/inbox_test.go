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
	if err := db.AutoMigrate(&ChatConnectorInbox{}, &ChatConnectorCapability{}, &ChatConnectorCursor{},
		&ChatConnectorInstallation{}); err != nil {
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
	if row.MessageText != "hi" {
		t.Errorf("message text snapshot = %q, want hi", row.MessageText)
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
	if quarantined.RawPayloadSHA256 != "deadbeef" {
		t.Errorf("raw sha = %q, want deadbeef (row-side integrity anchor)", quarantined.RawPayloadSHA256)
	}
	if quarantined.MessageText != "" {
		t.Errorf("unknown shape message text = %q, want empty (no normalized body)", quarantined.MessageText)
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
		func(tx *gorm.DB) { _ = tx.AddError(errors.New("boom")) }); err != nil {
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
		func(tx *gorm.DB) { _ = tx.AddError(errors.New("boom")) }); err != nil {
		t.Fatalf("register callback: %v", err)
	}
	if _, err := store.ListQuarantined(context.Background(), "tenant-1", "workspace-1", 10); err == nil {
		t.Error("list quarantined: expected query error")
	}
}

func TestInboxDismissQuarantinedRow(t *testing.T) {
	store := NewInboxStore(newChannelTestDB(t))
	if _, err := store.Accept(context.Background(), unknownEvent("q1")); err != nil {
		t.Fatalf("accept quarantined: %v", err)
	}
	if _, err := store.Accept(context.Background(), messageEvent("c1")); err != nil {
		t.Fatalf("accept committed: %v", err)
	}
	row := inboxByKey(t, store, unknownEvent("q1").IdempotencyKey)
	if err := store.Dismiss(context.Background(), "tenant-1", "workspace-1", row.ID); err != nil {
		t.Fatalf("dismiss: %v", err)
	}
	dismissed := inboxByKey(t, store, unknownEvent("q1").IdempotencyKey)
	if dismissed.Status != InboxStatusDismissed {
		t.Fatalf("status = %q, want dismissed", dismissed.Status)
	}
	// the row stays as evidence; only the lane filter changed
	if got := inboxCount(t, store); got != 2 {
		t.Fatalf("inbox rows after dismiss = %d, want 2 (row retained)", got)
	}
	rows, err := store.ListQuarantined(context.Background(), "tenant-1", "workspace-1", 10)
	if err != nil {
		t.Fatalf("list quarantined: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("quarantine lane = %d rows, want 0 after dismiss", len(rows))
	}

	// the lane transition is one-way from quarantined: re-dismissing an
	// already dismissed row or dismissing a committed row both reject
	if err := store.Dismiss(context.Background(), "tenant-1", "workspace-1", row.ID); !errors.Is(err, ErrInboxNotQuarantined) {
		t.Fatalf("re-dismiss = %v, want ErrInboxNotQuarantined", err)
	}
	committed := inboxByKey(t, store, messageEvent("c1").IdempotencyKey)
	if err := store.Dismiss(context.Background(), "tenant-1", "workspace-1", committed.ID); !errors.Is(err, ErrInboxNotQuarantined) {
		t.Fatalf("dismiss committed = %v, want ErrInboxNotQuarantined", err)
	}
}

func TestInboxDismissScopeIsolation(t *testing.T) {
	store := NewInboxStore(newChannelTestDB(t))
	if _, err := store.Accept(context.Background(), unknownEvent("q1")); err != nil {
		t.Fatalf("accept quarantined: %v", err)
	}
	row := inboxByKey(t, store, unknownEvent("q1").IdempotencyKey)
	// the scope filter is part of the statement: another scope's row reads
	// as missing and stays untouched
	if err := store.Dismiss(context.Background(), "tenant-2", "workspace-1", row.ID); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("cross-scope dismiss = %v, want record not found", err)
	}
	if got := inboxByKey(t, store, unknownEvent("q1").IdempotencyKey); got.Status != InboxStatusQuarantined {
		t.Fatalf("cross-scope dismiss must not touch the row, status = %q", got.Status)
	}
}

func TestInboxDismissUpdateError(t *testing.T) {
	store := NewInboxStore(newChannelTestDB(t))
	if _, err := store.Accept(context.Background(), unknownEvent("q1")); err != nil {
		t.Fatalf("accept quarantined: %v", err)
	}
	row := inboxByKey(t, store, unknownEvent("q1").IdempotencyKey)
	if err := store.db.Callback().Update().Before("gorm:update").Register("inbox_dismiss_error",
		func(tx *gorm.DB) { _ = tx.AddError(errors.New("boom")) }); err != nil {
		t.Fatalf("register callback: %v", err)
	}
	if err := store.Dismiss(context.Background(), "tenant-1", "workspace-1", row.ID); err == nil {
		t.Fatal("dismiss: expected update error")
	}
}

func TestInboxDismissProbeError(t *testing.T) {
	store := NewInboxStore(newChannelTestDB(t))
	// no rows: the update affects nothing and the store probes for the row;
	// a failing probe must surface as an error, not as not-quarantined
	if err := store.db.Callback().Query().After("gorm:query").Register("inbox_dismiss_probe_error",
		func(tx *gorm.DB) { _ = tx.AddError(errors.New("boom")) }); err != nil {
		t.Fatalf("register callback: %v", err)
	}
	if err := store.Dismiss(context.Background(), "tenant-1", "workspace-1", 12345); err == nil {
		t.Fatal("dismiss probe: expected query error")
	}
}

func TestInboxCommittedByID(t *testing.T) {
	store := NewInboxStore(newChannelTestDB(t))
	if _, err := store.Accept(context.Background(), messageEvent("c1")); err != nil {
		t.Fatalf("accept committed: %v", err)
	}
	if _, err := store.Accept(context.Background(), unknownEvent("q1")); err != nil {
		t.Fatalf("accept quarantined: %v", err)
	}
	committed := inboxByKey(t, store, messageEvent("c1").IdempotencyKey)
	quarantined := inboxByKey(t, store, unknownEvent("q1").IdempotencyKey)

	row, err := store.CommittedByID(context.Background(), "tenant-1", "workspace-1", committed.ID)
	if err != nil {
		t.Fatalf("committed by id: %v", err)
	}
	if row.Status != InboxStatusCommitted || row.ForwardedAt != nil {
		t.Errorf("row = %s forwarded=%v, want committed and unsettled", row.Status, row.ForwardedAt)
	}
	if _, err := store.CommittedByID(context.Background(), "tenant-1", "workspace-1", quarantined.ID); !errors.Is(err, ErrInboxNotCommitted) {
		t.Fatalf("quarantined by id = %v, want ErrInboxNotCommitted", err)
	}
	// the scope filter is part of the statement: another scope's row reads as
	// missing, and so does an unknown id
	if _, err := store.CommittedByID(context.Background(), "tenant-2", "workspace-1", committed.ID); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("cross-scope by id = %v, want record not found", err)
	}
	if _, err := store.CommittedByID(context.Background(), "tenant-1", "workspace-1", 12345); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("unknown id = %v, want record not found", err)
	}
}

func TestInboxListUnforwarded(t *testing.T) {
	store := NewInboxStore(newChannelTestDB(t))
	for _, eventID := range []string{"c1", "c2", "c3"} {
		if _, err := store.Accept(context.Background(), messageEvent(eventID)); err != nil {
			t.Fatalf("accept %s: %v", eventID, err)
		}
	}
	if _, err := store.Accept(context.Background(), unknownEvent("q1")); err != nil {
		t.Fatalf("accept quarantined: %v", err)
	}
	otherTenant := messageEvent("other")
	otherTenant.TenantID = "tenant-2"
	if _, err := store.Accept(context.Background(), otherTenant); err != nil {
		t.Fatalf("accept other tenant: %v", err)
	}
	// settling one committed row takes it out of the lane; the quarantined row
	// was never in it
	if err := store.MarkForwardedByKey(context.Background(), "tenant-1", "workspace-1",
		messageEvent("c2").IdempotencyKey, time.Now()); err != nil {
		t.Fatalf("mark forwarded: %v", err)
	}

	rows, err := store.ListUnforwarded(context.Background(), "tenant-1", "workspace-1", 0)
	if err != nil {
		t.Fatalf("list unforwarded: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("list unforwarded = %d rows, want 2", len(rows))
	}
	if rows[0].ID >= rows[1].ID {
		t.Errorf("rows not in oldest-first order: %d, %d", rows[0].ID, rows[1].ID)
	}
	for _, row := range rows {
		if row.EventID == "c2" || row.EventID == "q1" || row.TenantID != "tenant-1" {
			t.Errorf("unexpected lane row %s %s %s", row.EventID, row.Status, row.TenantID)
		}
	}

	limited, err := store.ListUnforwarded(context.Background(), "tenant-1", "workspace-1", 1)
	if err != nil {
		t.Fatalf("list unforwarded limited: %v", err)
	}
	if len(limited) != 1 || limited[0].EventID != "c1" {
		t.Errorf("limited lane = %+v, want the oldest row c1", limited)
	}
	foreign, err := store.ListUnforwarded(context.Background(), "tenant-9", "workspace-9", 0)
	if err != nil {
		t.Fatalf("list unforwarded cross-scope: %v", err)
	}
	if len(foreign) != 0 {
		t.Errorf("cross-scope lane = %d rows, want 0", len(foreign))
	}
}

func TestInboxListUnforwardedQueryError(t *testing.T) {
	store := NewInboxStore(newChannelTestDB(t))
	if err := store.db.Callback().Query().After("gorm:query").Register("inbox_unforwarded_error",
		func(tx *gorm.DB) { _ = tx.AddError(errors.New("boom")) }); err != nil {
		t.Fatalf("register callback: %v", err)
	}
	if _, err := store.ListUnforwarded(context.Background(), "tenant-1", "workspace-1", 10); err == nil {
		t.Error("list unforwarded: expected query error")
	}
}

func TestInboxMarkForwardedByKey(t *testing.T) {
	store := NewInboxStore(newChannelTestDB(t))
	if _, err := store.Accept(context.Background(), messageEvent("c1")); err != nil {
		t.Fatalf("accept committed: %v", err)
	}
	key := messageEvent("c1").IdempotencyKey
	// a foreign scope's key is a silent no-op: the row stays unsettled
	if err := store.MarkForwardedByKey(context.Background(), "tenant-2", "workspace-1", key, time.Now()); err != nil {
		t.Fatalf("mark cross-scope: %v", err)
	}
	if row := inboxByKey(t, store, key); row.ForwardedAt != nil {
		t.Fatalf("cross-scope mark must not stamp the row, forwarded_at = %v", row.ForwardedAt)
	}

	stamp := time.Now().UTC()
	if err := store.MarkForwardedByKey(context.Background(), "tenant-1", "workspace-1", key, stamp); err != nil {
		t.Fatalf("mark forwarded: %v", err)
	}
	row := inboxByKey(t, store, key)
	if row.ForwardedAt == nil || row.ForwardedAt.Before(stamp) {
		t.Fatalf("forwarded_at = %v, want stamp at or after %v", row.ForwardedAt, stamp)
	}

	if err := store.db.Callback().Update().Before("gorm:update").Register("inbox_mark_error",
		func(tx *gorm.DB) { _ = tx.AddError(errors.New("boom")) }); err != nil {
		t.Fatalf("register callback: %v", err)
	}
	if err := store.MarkForwardedByKey(context.Background(), "tenant-1", "workspace-1", key, time.Now()); err == nil {
		t.Error("mark forwarded: expected update error")
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

func TestBoundedTextSnapshot(t *testing.T) {
	if got := boundedTextSnapshot(nil); got != "" {
		t.Errorf("nil message snapshot = %q, want empty", got)
	}
	if got := boundedTextSnapshot(&MessageContent{Text: "short"}); got != "short" {
		t.Errorf("short snapshot = %q, want short", got)
	}
	exact := strings.Repeat("界", maxMessageTextSnapshotRunes)
	if got := boundedTextSnapshot(&MessageContent{Text: exact}); got != exact {
		t.Errorf("exact-limit snapshot changed length = %d", len([]rune(got)))
	}
	over := exact + "overflow"
	got := boundedTextSnapshot(&MessageContent{Text: over})
	if len([]rune(got)) != maxMessageTextSnapshotRunes || got != exact {
		t.Errorf("over-limit snapshot = %d runes, want %d rune cap", len([]rune(got)), maxMessageTextSnapshotRunes)
	}
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
