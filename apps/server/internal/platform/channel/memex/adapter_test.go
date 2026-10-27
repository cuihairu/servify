package memex

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"servify/apps/server/internal/platform/channel"
)

var adapterDBSeq atomic.Uint64

func newAdapterStores(t *testing.T) (*channel.InboxStore, *channel.CapabilityStore, *gorm.DB) {
	t.Helper()
	dsn := "file:memex_" + strings.ReplaceAll(t.Name(), "/", "_") + "_" +
		strconv.FormatUint(adapterDBSeq.Add(1), 10) + "?mode=memory&cache=shared"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if sqlDB, err := db.DB(); err == nil {
		sqlDB.SetMaxOpenConns(1)
		t.Cleanup(func() { _ = sqlDB.Close() })
	}
	if err := db.AutoMigrate(&channel.ChatConnectorInbox{}, &channel.ChatConnectorCapability{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return channel.NewInboxStore(db), channel.NewCapabilityStore(db), db
}

func adapterConfig() Config {
	return Config{
		Platform:       "memex",
		TenantID:       "tenant-1",
		WorkspaceID:    "workspace-1",
		InstallationID: "inst-1",
		Conversation: channel.ConversationRef{
			PlatformID: "group:5",
			Kind:       "group",
			IsGroup:    true,
		},
	}
}

func enableIngestion(t *testing.T, caps *channel.CapabilityStore) {
	t.Helper()
	snapshot := DeclaredCapabilities(time.Now())
	if err := caps.Record(context.Background(), channel.CapabilityScope{
		TenantID:               "tenant-1",
		WorkspaceID:            "workspace-1",
		Platform:               "memex",
		InstallationID:         "inst-1",
		ConversationPlatformID: "group:5",
	}, snapshot); err != nil {
		t.Fatalf("record capabilities: %v", err)
	}
}

// memexFake is a scripted Memex webhook server: onPull answers the updates
// endpoint, ACKs are recorded so redelivery semantics stay observable.
type memexFake struct {
	t      *testing.T
	mu     sync.Mutex
	acked  []string
	onPull func(w http.ResponseWriter, r *http.Request)
}

func (f *memexFake) handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer tok" {
			f.t.Errorf("authorization = %q", got)
		}
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/bot/updates":
			f.onPull(w, r)
		case r.Method == http.MethodPost && r.URL.Path == "/bot/ack":
			var body struct {
				MsgID string `json:"msg_id"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.MsgID == "" {
				f.t.Errorf("ack body invalid: %v", err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			f.mu.Lock()
			f.acked = append(f.acked, body.MsgID)
			f.mu.Unlock()
			_, _ = w.Write([]byte(`{"ok":true}`))
		default:
			f.t.Errorf("unexpected call %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusBadRequest)
		}
	}
}

func (f *memexFake) ackedMsgs() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.acked...)
}

func TestDeclaredCapabilitiesHonestSurface(t *testing.T) {
	caps := DeclaredCapabilities(time.Unix(1000, 0))
	if !caps.IngestionEnabled() {
		t.Error("declared snapshot must enable ingestion")
	}
	if caps.Threads || caps.Replies || caps.Attachments || caps.Edits || caps.Deletes || caps.Reactions {
		t.Error("memex TEXT surface must declare no rich capabilities")
	}
	if len(caps.EventTypes) != 1 || caps.EventTypes[0] != channel.EventTypeMessageCreated {
		t.Errorf("event types = %v, want [message.created]", caps.EventTypes)
	}
	if caps.MessageScope != channel.MessageScopeAllAuthorizedGroups {
		t.Errorf("message scope = %s, want all_authorized_groups (membership = receive scope)", caps.MessageScope)
	}
	if caps.ReceiveMode != channel.ReceiveModePolling {
		t.Errorf("receive mode = %s, want polling", caps.ReceiveMode)
	}
}

func TestTranslatorBuildsContractEvent(t *testing.T) {
	ev := Translator{Config: adapterConfig()}.Event(
		time.Unix(1697000001, 0),
		Update{MsgID: "m1", From: "alice", To: "bot:echo", Text: "hi", TsMS: 1697000000000},
	)
	if err := ev.Validate(); err != nil {
		t.Fatalf("translated event invalid: %v", err)
	}
	if ev.EventID != "m1" || ev.EventType != channel.EventTypeMessageCreated {
		t.Errorf("event id/type = %s/%s", ev.EventID, ev.EventType)
	}
	if ev.IdempotencyKey != channel.InboundIdempotencyKey("inst-1", "m1") {
		t.Errorf("idempotency key = %q", ev.IdempotencyKey)
	}
	if ev.Conversation.PlatformID != "group:5" || ev.Sender.PlatformID != "alice" {
		t.Errorf("conversation/sender = %q/%q", ev.Conversation.PlatformID, ev.Sender.PlatformID)
	}
	if ev.Message == nil || ev.Message.Text != "hi" || ev.Message.Kind != "text" ||
		ev.Message.PlatformID != "m1" {
		t.Errorf("message content = %+v", ev.Message)
	}
	if !ev.OccurredAt.Equal(time.UnixMilli(1697000000000)) {
		t.Errorf("occurred at = %v", ev.OccurredAt)
	}
	if ev.PlatformExtensions["to"] != "bot:echo" {
		t.Errorf("extensions = %v, want to=bot:echo", ev.PlatformExtensions)
	}
}

func TestConnectorPollOnceAcceptsAndAcks(t *testing.T) {
	sink, caps, db := newAdapterStores(t)
	enableIngestion(t, caps)

	fake := &memexFake{t: t}
	fake.onPull = func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("timeout_ms"); got != "1500" {
			t.Errorf("timeout_ms = %q, want 1500", got)
		}
		_, _ = w.Write([]byte(`{"ok":true,"updates":[
			{"msg_id":"m1","from":"alice","to":"bot:echo","text":"hi","ts_ms":1697000000000},
			{"msg_id":"m2","from":"bob","to":"bot:echo","text":"yo","ts_ms":1697000001000}]}`))
	}
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	connector := NewConnector(NewClient(srv.URL, "tok", srv.Client()), caps, sink, adapterConfig())
	n, err := connector.PollOnce(context.Background(), 1500*time.Millisecond)
	if err != nil {
		t.Fatalf("poll once: %v", err)
	}
	if n != 2 {
		t.Errorf("processed = %d, want 2", n)
	}
	acked := fake.ackedMsgs()
	if len(acked) != 2 || acked[0] != "m1" || acked[1] != "m2" {
		t.Errorf("acked = %v, want [m1 m2]", acked)
	}
	var rows []channel.ChatConnectorInbox
	if err := db.Find(&rows).Error; err != nil {
		t.Fatalf("list inbox: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("inbox rows = %d, want 2", len(rows))
	}
	for _, row := range rows {
		if row.TenantID != "tenant-1" || row.WorkspaceID != "workspace-1" ||
			row.Platform != "memex" || row.InstallationID != "inst-1" ||
			row.ConversationPlatformID != "group:5" || row.Status != channel.InboxStatusCommitted {
			t.Errorf("scope/status drifted: %+v", row)
		}
	}
}

func TestConnectorPollOnceRedeliveryDeduplicates(t *testing.T) {
	sink, caps, db := newAdapterStores(t)
	enableIngestion(t, caps)

	payload := `{"ok":true,"updates":[
		{"msg_id":"m1","from":"alice","to":"bot:echo","text":"hi","ts_ms":1697000000000},
		{"msg_id":"m1","from":"alice","to":"bot:echo","text":"hi","ts_ms":1697000000000}]}`
	fake := &memexFake{t: t}
	fake.onPull = func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(payload))
	}
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	connector := NewConnector(NewClient(srv.URL, "tok", srv.Client()), caps, sink, adapterConfig())
	n, err := connector.PollOnce(context.Background(), 0)
	if err != nil {
		t.Fatalf("poll once: %v", err)
	}
	if n != 2 {
		t.Errorf("processed = %d, want 2 (duplicate still ACKs)", n)
	}
	if acked := fake.ackedMsgs(); len(acked) != 2 {
		t.Errorf("acked = %v, want both copies acknowledged", acked)
	}
	var count int64
	if err := db.Model(&channel.ChatConnectorInbox{}).Count(&count).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Errorf("inbox rows = %d, want 1 (dedup by idempotency key)", count)
	}
}

func TestConnectorPollOnceGatesOnCapabilities(t *testing.T) {
	sink, caps, _ := newAdapterStores(t)
	// no snapshot recorded: fail closed
	var pulled atomic.Bool
	fake := &memexFake{t: t}
	fake.onPull = func(w http.ResponseWriter, r *http.Request) {
		pulled.Store(true)
		_, _ = w.Write([]byte(`{"ok":true,"updates":[]}`))
	}
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	connector := NewConnector(NewClient(srv.URL, "tok", srv.Client()), caps, sink, adapterConfig())
	if _, err := connector.PollOnce(context.Background(), 0); !errors.Is(err, ErrIngestionDisabled) {
		t.Errorf("poll without snapshot = %v, want ErrIngestionDisabled", err)
	}
	if pulled.Load() {
		t.Error("disabled ingestion must not reach the platform")
	}

	// a snapshot with an unknown receive mode stays closed too
	revoked := DeclaredCapabilities(time.Now())
	revoked.ReceiveMode = channel.ReceiveModeUnknown
	if err := caps.Record(context.Background(), channel.CapabilityScope{
		TenantID:               "tenant-1",
		WorkspaceID:            "workspace-1",
		Platform:               "memex",
		InstallationID:         "inst-1",
		ConversationPlatformID: "group:5",
	}, revoked); err != nil {
		t.Fatalf("record revoked snapshot: %v", err)
	}
	if _, err := connector.PollOnce(context.Background(), 0); !errors.Is(err, ErrIngestionDisabled) {
		t.Errorf("poll with revoked snapshot = %v, want ErrIngestionDisabled", err)
	}

	enableIngestion(t, caps)
	if _, err := connector.PollOnce(context.Background(), 0); err != nil {
		t.Fatalf("poll after enabling: %v", err)
	}
}

func TestConnectorPollOnceClientError(t *testing.T) {
	sink, caps, _ := newAdapterStores(t)
	enableIngestion(t, caps)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	srv.Close() // dead endpoint: pull must fail before any accept/ack

	connector := NewConnector(NewClient(srv.URL, "tok", srv.Client()), caps, sink, adapterConfig())
	if _, err := connector.PollOnce(context.Background(), 0); err == nil {
		t.Error("poll once: expected client error")
	}
}

func TestConnectorPollOnceKeepsUnackedOnSinkFailure(t *testing.T) {
	sink, caps, db := newAdapterStores(t)
	enableIngestion(t, caps)
	if err := db.Callback().Create().Before("gorm:create").Register("memex_sink_error",
		func(tx *gorm.DB) { _ = tx.AddError(errors.New("boom")) }); err != nil {
		t.Fatalf("register callback: %v", err)
	}

	fake := &memexFake{t: t}
	fake.onPull = func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"ok":true,"updates":[
			{"msg_id":"m1","from":"alice","to":"bot:echo","text":"hi","ts_ms":1697000000000}]}`))
	}
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	connector := NewConnector(NewClient(srv.URL, "tok", srv.Client()), caps, sink, adapterConfig())
	if _, err := connector.PollOnce(context.Background(), 0); err == nil {
		t.Error("poll once: expected sink error")
	}
	if acked := fake.ackedMsgs(); len(acked) != 0 {
		t.Errorf("acked = %v, want none (ACK only after durable accept)", acked)
	}
}

func TestConnectorPollOnceStopsOnAckFailure(t *testing.T) {
	sink, caps, db := newAdapterStores(t)
	enableIngestion(t, caps)

	fake := &memexFake{t: t}
	fake.onPull = func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"ok":true,"updates":[
			{"msg_id":"m1","from":"alice","to":"bot:echo","text":"hi","ts_ms":1697000000000},
			{"msg_id":"m2","from":"bob","to":"bot:echo","text":"yo","ts_ms":1697000001000}]}`))
	}
	ackCalls := 0
	wrapped := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/bot/ack" {
			ackCalls++
			if ackCalls == 1 {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte(`{"ok":false,"error":"boom"}`))
				return
			}
		}
		fake.handler()(w, r)
	})
	srv := httptest.NewServer(wrapped)
	defer srv.Close()

	connector := NewConnector(NewClient(srv.URL, "tok", srv.Client()), caps, sink, adapterConfig())
	n, err := connector.PollOnce(context.Background(), 0)
	if err == nil {
		t.Error("poll once: expected ack error")
	}
	if n != 0 {
		t.Errorf("processed = %d, want 0 (stops at first failure)", n)
	}
	// m1 stays unacked for redelivery, m2 untouched.
	var count int64
	if err := db.Model(&channel.ChatConnectorInbox{}).Count(&count).Error; err != nil || count != 1 {
		t.Errorf("inbox rows = %d (%v), want 1", count, err)
	}
}

func TestConnectorNotifyGatesOnSendCapability(t *testing.T) {
	sink, caps, db := newAdapterStores(t)

	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		_, _ = w.Write([]byte(`{"ok":true,"msg_id":"out-1"}`))
	}))
	defer srv.Close()

	connector := NewConnector(NewClient(srv.URL, "tok", srv.Client()), caps, sink, adapterConfig())

	// no snapshot at all: fail closed without reaching the platform.
	if _, err := connector.Notify(context.Background(), "group:5", "hello"); !errors.Is(err, ErrSendDisabled) {
		t.Errorf("notify without snapshot = %v, want ErrSendDisabled", err)
	}
	if called {
		t.Error("send gate must not reach the platform")
	}

	enableIngestion(t, caps)
	// DeclaredCapabilities grants SendText: the send goes out.
	msgID, err := connector.Notify(context.Background(), "group:5", "hello")
	if err != nil {
		t.Fatalf("notify: %v", err)
	}
	if msgID != "out-1" {
		t.Errorf("notify msg id = %q, want out-1", msgID)
	}

	// capability flipped off afterwards: gate closes again.
	if err := db.Model(&channel.ChatConnectorCapability{}).Where("1=1").
		Update("send_text", false).Error; err != nil {
		t.Fatalf("flip send capability off: %v", err)
	}
	if _, err := connector.Notify(context.Background(), "group:5", "hello"); !errors.Is(err, ErrSendDisabled) {
		t.Errorf("notify with revoked send capability = %v, want ErrSendDisabled", err)
	}
}

func TestConnectorCapabilitiesReadError(t *testing.T) {
	sink, caps, db := newAdapterStores(t)
	enableIngestion(t, caps)
	if err := db.Callback().Query().Before("gorm:query").Register("memex_caps_error",
		func(tx *gorm.DB) { _ = tx.AddError(errors.New("boom")) }); err != nil {
		t.Fatalf("register callback: %v", err)
	}
	fake := &memexFake{t: t}
	fake.onPull = func(w http.ResponseWriter, r *http.Request) {}
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	connector := NewConnector(NewClient(srv.URL, "tok", srv.Client()), caps, sink, adapterConfig())
	if _, err := connector.PollOnce(context.Background(), 0); err == nil {
		t.Error("poll once: expected capabilities read error")
	}
	if _, err := connector.Notify(context.Background(), "group:5", "hello"); err == nil {
		t.Error("notify: expected capabilities read error")
	}
}

// recordingObserver plays the assembly ingress bridge: it records every
// committed event it is shown and can be scripted to fail.
type recordingObserver struct {
	mu        sync.Mutex
	forwarded []string
	err       error
}

func (o *recordingObserver) InboundCommitted(ctx context.Context, ev channel.ConnectorEvent) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.err != nil {
		return o.err
	}
	o.forwarded = append(o.forwarded, ev.EventID)
	return nil
}

func (o *recordingObserver) forwardedMsgs() []string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return append([]string(nil), o.forwarded...)
}

func TestConnectorPollOnceForwardsCommittedEventsOnce(t *testing.T) {
	sink, caps, _ := newAdapterStores(t)
	enableIngestion(t, caps)

	update := `{"ok":true,"updates":[
		{"msg_id":"m1","from":"alice","to":"bot:echo","text":"hi","ts_ms":1697000000000}]}`
	var pulls atomic.Int32
	fake := &memexFake{t: t}
	fake.onPull = func(w http.ResponseWriter, r *http.Request) {
		// both polls serve the same update: the second must land as a
		// duplicate that never re-fires the observer
		_, _ = w.Write([]byte(update))
		_ = pulls.Add(1)
	}
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	obs := &recordingObserver{}
	connector := NewConnector(NewClient(srv.URL, "tok", srv.Client()), caps, sink, adapterConfig()).WithObserver(obs)

	n1, err := connector.PollOnce(context.Background(), 1500*time.Millisecond)
	if err != nil || n1 != 1 {
		t.Fatalf("first poll = %d (%v), want 1", n1, err)
	}
	n2, err := connector.PollOnce(context.Background(), 1500*time.Millisecond)
	if err != nil || n2 != 1 {
		t.Fatalf("redelivery poll = %d (%v), want 1", n2, err)
	}
	if got := obs.forwardedMsgs(); len(got) != 1 || got[0] != "m1" {
		t.Errorf("forwarded = %v, want exactly [m1] (duplicates never re-fire)", got)
	}
	if got := fake.ackedMsgs(); len(got) != 2 || got[0] != "m1" || got[1] != "m1" {
		t.Errorf("acked = %v, want m1 twice (commit and duplicate both clear)", got)
	}
}

func TestConnectorPollOnceObserverErrorAbortsBeforeAck(t *testing.T) {
	sink, caps, db := newAdapterStores(t)
	enableIngestion(t, caps)

	fake := &memexFake{t: t}
	fake.onPull = func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"ok":true,"updates":[
			{"msg_id":"m1","from":"alice","to":"bot:echo","text":"hi","ts_ms":1697000000000}]}`))
	}
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	obs := &recordingObserver{err: errors.New("conversation store down")}
	connector := NewConnector(NewClient(srv.URL, "tok", srv.Client()), caps, sink, adapterConfig()).WithObserver(obs)

	n, err := connector.PollOnce(context.Background(), 1500*time.Millisecond)
	if err == nil {
		t.Fatal("observer failure must abort the poll")
	}
	if n != 0 {
		t.Errorf("processed = %d, want 0", n)
	}
	if got := fake.ackedMsgs(); len(got) != 0 {
		t.Errorf("acked = %v, want none (abort before ACK)", got)
	}
	var rows []channel.ChatConnectorInbox
	if err := db.Find(&rows).Error; err != nil {
		t.Fatalf("list inbox: %v", err)
	}
	if len(rows) != 1 || rows[0].Status != channel.InboxStatusCommitted {
		t.Errorf("inbox rows = %+v, want the committed row kept", rows)
	}
}

func TestConnectorWithoutObserverStillPolls(t *testing.T) {
	sink, caps, _ := newAdapterStores(t)
	enableIngestion(t, caps)

	fake := &memexFake{t: t}
	fake.onPull = func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"ok":true,"updates":[]}`))
	}
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	connector := NewConnector(NewClient(srv.URL, "tok", srv.Client()), caps, sink, adapterConfig())
	if n, err := connector.PollOnce(context.Background(), 0); err != nil || n != 0 {
		t.Fatalf("poll without observer = %d (%v), want 0 nil", n, err)
	}
}

func newTestPayloadStore(t *testing.T) (*channel.LocalPayloadStore, string) {
	t.Helper()
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatalf("read key: %v", err)
	}
	root := t.TempDir()
	store, err := channel.NewLocalPayloadStore(root, key, 0)
	if err != nil {
		t.Fatalf("new payload store: %v", err)
	}
	return store, root
}

func TestConnectorPollOnceRetainsRawPayloadOnRow(t *testing.T) {
	sink, caps, db := newAdapterStores(t)
	enableIngestion(t, caps)
	store, _ := newTestPayloadStore(t)

	rawUpdate := `{"msg_id":"m1","from":"alice","to":"bot:echo","text":"hello retention","ts_ms":1697000000000}`
	fake := &memexFake{t: t}
	fake.onPull = func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"ok":true,"updates":[` + rawUpdate + `]}`))
	}
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	connector := NewConnector(NewClient(srv.URL, "tok", srv.Client()), caps, sink, adapterConfig()).
		WithPayloadStore(store)
	n, err := connector.PollOnce(context.Background(), 1500*time.Millisecond)
	if err != nil || n != 1 {
		t.Fatalf("poll once = %d (%v), want 1 nil", n, err)
	}
	if acked := fake.ackedMsgs(); len(acked) != 1 || acked[0] != "m1" {
		t.Errorf("acked = %v, want [m1] (retention must not stall the ACK)", acked)
	}
	var row channel.ChatConnectorInbox
	if err := db.Take(&row).Error; err != nil {
		t.Fatalf("row: %v", err)
	}
	if row.RawPayloadStore != channel.LocalPayloadStoreName || len(row.RawPayloadObjectID) != 32 {
		t.Fatalf("row ref = %s/%q, want local-encrypted/32-hex", row.RawPayloadStore, row.RawPayloadObjectID)
	}
	// the retained object is the exact bytes the platform sent
	sum := sha256.Sum256([]byte(rawUpdate))
	ref := channel.RawPayloadRef{Store: row.RawPayloadStore, ObjectID: row.RawPayloadObjectID,
		SHA256: hex.EncodeToString(sum[:])}
	got, err := store.Open(context.Background(), ref)
	if err != nil || string(got) != rawUpdate {
		t.Errorf("open retained = %q (%v), want the exact platform bytes", got, err)
	}
	meta, err := store.Meta(context.Background(), channel.RawPayloadRef{Store: row.RawPayloadStore, ObjectID: row.RawPayloadObjectID})
	if err != nil || meta.TenantID != "tenant-1" || meta.Platform != "memex" ||
		meta.InstallationID != "inst-1" || meta.EventID != "m1" || meta.ContentType != channel.RawPayloadContentType {
		t.Errorf("retained meta = %+v (%v), want the row scope", meta, err)
	}
}

func TestConnectorPollOnceDuplicateRedeliveryDropsOrphan(t *testing.T) {
	sink, caps, db := newAdapterStores(t)
	enableIngestion(t, caps)
	store, root := newTestPayloadStore(t)

	rawUpdate := `{"msg_id":"m1","from":"alice","to":"bot:echo","text":"again","ts_ms":1697000000000}`
	fake := &memexFake{t: t}
	fake.onPull = func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"ok":true,"updates":[` + rawUpdate + `]}`))
	}
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	connector := NewConnector(NewClient(srv.URL, "tok", srv.Client()), caps, sink, adapterConfig()).
		WithPayloadStore(store)
	for i := 0; i < 2; i++ {
		if n, err := connector.PollOnce(context.Background(), 1500*time.Millisecond); err != nil || n != 1 {
			t.Fatalf("poll %d = %d (%v), want 1 nil", i+1, n, err)
		}
	}
	var row channel.ChatConnectorInbox
	if err := db.Take(&row).Error; err != nil {
		t.Fatalf("row: %v", err)
	}
	if len(row.RawPayloadObjectID) != 32 {
		t.Fatalf("row ref object = %q, want retained", row.RawPayloadObjectID)
	}
	// only the first delivery's pair survives: the redelivery wrote a second
	// object, saw the duplicate verdict and removed its own copy
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("read root: %v", err)
	}
	if len(entries) != 2 {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("retained files = %v, want exactly the surviving object+meta pair", names)
	}
	for _, e := range entries {
		if strings.TrimSuffix(e.Name(), ".meta.json") != row.RawPayloadObjectID {
			t.Errorf("stray file %q: the row must point at the only retained object", e.Name())
		}
	}
}

func TestConnectorPollOncePayloadSaveFailureAborts(t *testing.T) {
	sink, caps, db := newAdapterStores(t)
	enableIngestion(t, caps)
	// a root under an existing file cannot be created: every save fails
	blocked := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(blocked, []byte("x"), 0o600); err != nil {
		t.Fatalf("seed file: %v", err)
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatalf("read key: %v", err)
	}
	badStore, err := channel.NewLocalPayloadStore(filepath.Join(blocked, "payloads"), key, 0)
	if err != nil {
		t.Fatalf("new payload store: %v", err)
	}

	fake := &memexFake{t: t}
	fake.onPull = func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"ok":true,"updates":[{"msg_id":"m1","from":"alice","to":"bot:echo","text":"hi","ts_ms":1697000000000}]}`))
	}
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	connector := NewConnector(NewClient(srv.URL, "tok", srv.Client()), caps, sink, adapterConfig()).
		WithPayloadStore(badStore)
	n, err := connector.PollOnce(context.Background(), 1500*time.Millisecond)
	if err == nil || n != 0 {
		t.Fatalf("poll = %d (%v), want 0 with a retention error", n, err)
	}
	if acked := fake.ackedMsgs(); len(acked) != 0 {
		t.Errorf("acked = %v, want none (the message stays for redelivery)", acked)
	}
	var rows []channel.ChatConnectorInbox
	if err := db.Find(&rows).Error; err != nil || len(rows) != 0 {
		t.Errorf("inbox rows = %d (%v), want none", len(rows), err)
	}
}

func TestConnectorWithoutPayloadStoreKeepsNoRef(t *testing.T) {
	sink, caps, db := newAdapterStores(t)
	enableIngestion(t, caps)
	fake := &memexFake{t: t}
	fake.onPull = func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"ok":true,"updates":[{"msg_id":"m1","from":"alice","to":"bot:echo","text":"hi","ts_ms":1697000000000}]}`))
	}
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	connector := NewConnector(NewClient(srv.URL, "tok", srv.Client()), caps, sink, adapterConfig())
	if _, err := connector.PollOnce(context.Background(), 1500*time.Millisecond); err != nil {
		t.Fatalf("poll once: %v", err)
	}
	var row channel.ChatConnectorInbox
	if err := db.Take(&row).Error; err != nil {
		t.Fatalf("row: %v", err)
	}
	if row.RawPayloadStore != "" || row.RawPayloadObjectID != "" {
		t.Errorf("ref = %s/%q, want empty (retention off is observable)", row.RawPayloadStore, row.RawPayloadObjectID)
	}
}
