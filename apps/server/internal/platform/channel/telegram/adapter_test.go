package telegram

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
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

func newAdapterStores(t *testing.T) (*channel.InboxStore, *channel.CapabilityStore, *channel.CursorStore, *gorm.DB) {
	t.Helper()
	dsn := "file:tg_" + strings.ReplaceAll(t.Name(), "/", "_") + "_" +
		strconv.FormatUint(adapterDBSeq.Add(1), 10) + "?mode=memory&cache=shared"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if sqlDB, err := db.DB(); err == nil {
		sqlDB.SetMaxOpenConns(1)
		t.Cleanup(func() { _ = sqlDB.Close() })
	}
	if err := db.AutoMigrate(&channel.ChatConnectorInbox{}, &channel.ChatConnectorCapability{},
		&channel.ChatConnectorCursor{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return channel.NewInboxStore(db), channel.NewCapabilityStore(db), channel.NewCursorStore(db), db
}

func adapterConfig() Config {
	return Config{
		Platform:       "telegram",
		TenantID:       "tenant-1",
		WorkspaceID:    "workspace-1",
		InstallationID: "inst-1",
		Conversation: channel.ConversationRef{
			PlatformID: "-100123",
			Kind:       "supergroup",
			IsGroup:    true,
		},
	}
}

func capabilityScope() channel.CapabilityScope {
	return channel.CapabilityScope{
		TenantID:               "tenant-1",
		WorkspaceID:            "workspace-1",
		Platform:               "telegram",
		InstallationID:         "inst-1",
		ConversationPlatformID: "-100123",
	}
}

func enableIngestion(t *testing.T, caps *channel.CapabilityStore) {
	t.Helper()
	if err := caps.Record(context.Background(), capabilityScope(), DeclaredCapabilities(time.Now())); err != nil {
		t.Fatalf("record capabilities: %v", err)
	}
}

// botAPI is a scripted Bot API server: onPoll answers getUpdates (the
// observed offset frontier is recorded), onSend answers sendMessage.
type botAPI struct {
	t        *testing.T
	mu       sync.Mutex
	offsets  []string
	timeouts []string
	sends    []string
	replies  []string
	threads  []string
	onPoll   func(w http.ResponseWriter, r *http.Request)
	onSend   func(w http.ResponseWriter, r *http.Request)
}

func (f *botAPI) handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/bottok/") {
			f.t.Errorf("path = %q, want /bottok/... (token auth is the path)", r.URL.Path)
		}
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/bottok/getUpdates":
			f.mu.Lock()
			f.offsets = append(f.offsets, r.URL.Query().Get("offset"))
			f.timeouts = append(f.timeouts, r.URL.Query().Get("timeout"))
			f.mu.Unlock()
			f.onPoll(w, r)
		case r.Method == http.MethodPost && r.URL.Path == "/bottok/sendMessage":
			f.mu.Lock()
			f.sends = append(f.sends, r.URL.Query().Get("chat_id")+"|"+r.URL.Query().Get("text"))
			f.replies = append(f.replies, r.URL.Query().Get("reply_to_message_id"))
			f.threads = append(f.threads, r.URL.Query().Get("message_thread_id"))
			f.mu.Unlock()
			if f.onSend != nil {
				f.onSend(w, r)
				return
			}
			_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":77}}`))
		default:
			f.t.Errorf("unexpected call %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusBadRequest)
		}
	}
}

func (f *botAPI) observed() (offsets, timeouts, sends []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.offsets...), append([]string(nil), f.timeouts...),
		append([]string(nil), f.sends...)
}

func (f *botAPI) observedSendChain() (replies, threads []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.replies...), append([]string(nil), f.threads...)
}

func storedCursor(t *testing.T, cursors *channel.CursorStore) string {
	t.Helper()
	cur, err := cursors.Cursor(context.Background(), channel.CursorScope{
		TenantID: "tenant-1", WorkspaceID: "workspace-1", Platform: "telegram", InstallationID: "inst-1",
	})
	if err != nil {
		t.Fatalf("read cursor: %v", err)
	}
	return cur
}

func TestDeclaredCapabilitiesHonestSurface(t *testing.T) {
	caps := DeclaredCapabilities(time.Unix(1000, 0))
	if !caps.IngestionEnabled() {
		t.Error("declared snapshot must enable ingestion")
	}
	if caps.Threads || caps.Replies || caps.Attachments || caps.Edits || caps.Deletes || caps.Reactions {
		t.Error("telegram TEXT surface must declare no rich capabilities")
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
		Update{UpdateID: 41, Message: &ChatMessage{
			MessageID: 7,
			Chat:      &Chat{ID: -100123, Title: "ops", Type: "supergroup"},
			From:      &User{ID: 9001, Username: "alice"},
			Text:      "hi",
			Date:      1697000000,
		}},
	)
	if err := ev.Validate(); err != nil {
		t.Fatalf("translated event invalid: %v", err)
	}
	if ev.EventType != channel.EventTypeMessageCreated || ev.EventID != "7" {
		t.Errorf("event type/id = %s/%s", ev.EventType, ev.EventID)
	}
	if ev.IdempotencyKey != channel.InboundIdempotencyKey("inst-1", "7") {
		t.Errorf("idempotency key = %q", ev.IdempotencyKey)
	}
	if ev.Conversation.PlatformID != "-100123" || ev.Conversation.Kind != "supergroup" || !ev.Conversation.IsGroup {
		t.Errorf("conversation = %+v", ev.Conversation)
	}
	if ev.Sender.PlatformID != "9001" {
		t.Errorf("sender = %+v", ev.Sender)
	}
	if ev.Message == nil || ev.Message.PlatformID != "7" || ev.Message.Kind != "text" || ev.Message.Text != "hi" {
		t.Errorf("message content = %+v", ev.Message)
	}
	if !ev.OccurredAt.Equal(time.Unix(1697000000, 0)) {
		t.Errorf("occurred at = %v, want platform date", ev.OccurredAt)
	}
	if ev.Platform != "telegram" || ev.TenantID != "tenant-1" || ev.WorkspaceID != "workspace-1" ||
		ev.InstallationID != "inst-1" {
		t.Errorf("scope drifted: %+v", ev)
	}
}

func TestTranslatorPrivateChatIsNotGroup(t *testing.T) {
	ev := Translator{Config: adapterConfig()}.Event(time.Unix(1, 0), Update{
		Message: &ChatMessage{MessageID: 3, Chat: &Chat{ID: 42, Type: "private"}, Text: "dm"},
	})
	if ev.Conversation.IsGroup || ev.Conversation.PlatformID != "42" {
		t.Errorf("private conversation = %+v, want platform chat without group flag", ev.Conversation)
	}
	if ev.Sender.PlatformID != unknownSender {
		t.Errorf("sender without from = %q, want %q", ev.Sender.PlatformID, unknownSender)
	}
}

// Only TEXT messages are message.created: every other shape (edited_message,
// callback_query, media-only messages) is the quarantine lane, attributed to
// the configured conversation with an unknown sender and no payload reference.
func TestTranslatorQuarantinesNonMessageShapes(t *testing.T) {
	cases := map[string]Update{
		"non-message update": {UpdateID: 55},
		"message without chat": {UpdateID: 55, Message: &ChatMessage{
			MessageID: 7, Chat: nil, Text: "hi"}},
		"message without text": {UpdateID: 55, Message: &ChatMessage{
			MessageID: 7, Chat: &Chat{ID: -100123, Type: "supergroup"}, Text: ""}},
	}
	for name, u := range cases {
		ev := Translator{Config: adapterConfig()}.Event(time.Unix(1697000001, 0), u)
		if err := ev.Validate(); err != nil {
			t.Errorf("%s: unknown event invalid: %v", name, err)
		}
		if !ev.QuarantineRequired() || ev.EventType != channel.EventTypeUnknown {
			t.Errorf("%s: type = %s, want unknown (quarantine lane)", name, ev.EventType)
		}
		if ev.EventID != "update-55" ||
			ev.IdempotencyKey != channel.InboundIdempotencyKey("inst-1", "update-55") {
			t.Errorf("%s: event id = %s key = %s", name, ev.EventID, ev.IdempotencyKey)
		}
		if ev.Conversation.PlatformID != "-100123" || ev.Sender.PlatformID != unknownSender {
			t.Errorf("%s: conversation/sender = %q/%q", name, ev.Conversation.PlatformID, ev.Sender.PlatformID)
		}
		if ev.Message != nil {
			t.Errorf("%s: quarantined shape must carry no message content", name)
		}
		if !ev.OccurredAt.Equal(time.Unix(1697000001, 0)) {
			t.Errorf("%s: occurred at = %v, want received time (no platform timestamp)", name, ev.OccurredAt)
		}
	}
}

func TestConnectorPollOnceAdvancesOffsetAfterAccept(t *testing.T) {
	sink, caps, cursors, db := newAdapterStores(t)
	enableIngestion(t, caps)

	fake := &botAPI{t: t}
	fake.onPoll = func(w http.ResponseWriter, r *http.Request) {
		offsets, _, _ := fake.observed()
		if len(offsets) == 1 {
			_, _ = w.Write([]byte(`{"ok":true,"result":[
				{"update_id":41,"message":{"message_id":7,"chat":{"id":-100123,"type":"supergroup"},
					"from":{"id":9001},"text":"hi","date":1697000000}},
				{"update_id":42,"message":{"message_id":8,"chat":{"id":-100123,"type":"supergroup"},
					"from":{"id":9002},"text":"yo","date":1697000001}}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"ok":true,"result":null}`))
	}
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	connector := NewConnector(NewClient(srv.URL, "tok", srv.Client()), caps, sink, cursors, adapterConfig())
	n, err := connector.PollOnce(context.Background(), 1500*time.Millisecond)
	if err != nil || n != 2 {
		t.Fatalf("first poll = %d (%v), want 2", n, err)
	}
	// The stored cursor is the acknowledged frontier: last update_id + 1.
	if cur := storedCursor(t, cursors); cur != "43" {
		t.Errorf("cursor = %q, want 43 (last update_id + 1)", cur)
	}
	if n, err = connector.PollOnce(context.Background(), 0); err != nil || n != 0 {
		t.Fatalf("second poll = %d (%v), want 0", n, err)
	}
	offsets, timeouts, _ := fake.observed()
	if len(offsets) != 2 || offsets[0] != "0" || offsets[1] != "43" {
		t.Errorf("offsets = %v, want [0 43] (ack is offset progression)", offsets)
	}
	if timeouts[0] != "2" {
		t.Errorf("timeout = %q, want 2 (1500ms long poll rounds up to whole seconds)", timeouts[0])
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
			row.Platform != "telegram" || row.InstallationID != "inst-1" ||
			row.ConversationPlatformID != "-100123" || row.Status != channel.InboxStatusCommitted {
			t.Errorf("scope/status drifted: %+v", row)
		}
	}
}

func TestConnectorPollOnceQuarantinesUnknownShapes(t *testing.T) {
	sink, caps, cursors, db := newAdapterStores(t)
	enableIngestion(t, caps)

	fake := &botAPI{t: t}
	fake.onPoll = func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"ok":true,"result":[
			{"update_id":41,"message":{"message_id":7,"chat":{"id":-100123,"type":"supergroup"},
				"from":{"id":9001},"text":"hi","date":1697000000}},
			{"update_id":42,"edited_message":{"message_id":7,"text":"edited"}}]}`))
	}
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	obs := &recordingObserver{}
	connector := NewConnector(NewClient(srv.URL, "tok", srv.Client()), caps, sink, cursors, adapterConfig()).WithObserver(obs)
	if n, err := connector.PollOnce(context.Background(), 0); err != nil || n != 2 {
		t.Fatalf("poll = %d (%v), want 2 (both shapes accepted)", n, err)
	}
	var rows []channel.ChatConnectorInbox
	if err := db.Find(&rows).Error; err != nil {
		t.Fatalf("list inbox: %v", err)
	}
	committed, quarantined := 0, 0
	for _, row := range rows {
		switch row.Status {
		case channel.InboxStatusCommitted:
			committed++
		case channel.InboxStatusQuarantined:
			quarantined++
			if row.EventType != string(channel.EventTypeUnknown) || row.SenderPlatformID != unknownSender ||
				row.ConversationPlatformID != "-100123" || row.EventID != "update-42" {
				t.Errorf("quarantined row drifted: %+v", row)
			}
		default:
			t.Errorf("unexpected status %q", row.Status)
		}
	}
	if committed != 1 || quarantined != 1 {
		t.Errorf("committed/quarantined = %d/%d, want 1/1", committed, quarantined)
	}
	if got := obs.forwardedEvents(); len(got) != 1 || got[0] != "7" {
		t.Errorf("forwarded = %v, want exactly [7] (quarantined rows never reach conversations)", got)
	}
	if cur := storedCursor(t, cursors); cur != "43" {
		t.Errorf("cursor = %q, want 43 (quarantine is a durable verdict)", cur)
	}
}

func TestConnectorPollOnceRedeliveryDeduplicates(t *testing.T) {
	sink, caps, cursors, db := newAdapterStores(t)
	enableIngestion(t, caps)

	payload := `{"ok":true,"result":[
		{"update_id":41,"message":{"message_id":7,"chat":{"id":-100123,"type":"supergroup"},
			"from":{"id":9001},"text":"hi","date":1697000000}}]}`
	// The same update redelivered despite the advanced cursor (e.g. the bot
	// was restarted with a stale offset): the inbox absorbs it as duplicate.
	fake := &botAPI{t: t}
	fake.onPoll = func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(payload))
	}
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	obs := &recordingObserver{}
	connector := NewConnector(NewClient(srv.URL, "tok", srv.Client()), caps, sink, cursors, adapterConfig()).WithObserver(obs)
	for i := 0; i < 2; i++ {
		if n, err := connector.PollOnce(context.Background(), 0); err != nil || n != 1 {
			t.Fatalf("poll %d = %d (%v), want 1", i, n, err)
		}
	}
	var count int64
	if err := db.Model(&channel.ChatConnectorInbox{}).Count(&count).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Errorf("inbox rows = %d, want 1 (dedup by idempotency key)", count)
	}
	if got := obs.forwardedEvents(); len(got) != 1 {
		t.Errorf("forwarded = %v, want exactly once across redelivery", got)
	}
}

func TestConnectorPollOnceGatesOnCapabilities(t *testing.T) {
	sink, caps, cursors, _ := newAdapterStores(t)
	var pulled atomic.Bool
	fake := &botAPI{t: t}
	fake.onPoll = func(w http.ResponseWriter, r *http.Request) {
		pulled.Store(true)
		_, _ = w.Write([]byte(`{"ok":true,"result":null}`))
	}
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	connector := NewConnector(NewClient(srv.URL, "tok", srv.Client()), caps, sink, cursors, adapterConfig())
	// no snapshot recorded: fail closed
	if _, err := connector.PollOnce(context.Background(), 0); !errors.Is(err, ErrIngestionDisabled) {
		t.Errorf("poll without snapshot = %v, want ErrIngestionDisabled", err)
	}
	if pulled.Load() {
		t.Error("disabled ingestion must not reach the platform")
	}

	// a snapshot with an unknown receive mode stays closed too
	revoked := DeclaredCapabilities(time.Now())
	revoked.ReceiveMode = channel.ReceiveModeUnknown
	if err := caps.Record(context.Background(), capabilityScope(), revoked); err != nil {
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

func TestConnectorPollOnceClientErrorKeepsOffset(t *testing.T) {
	sink, caps, cursors, _ := newAdapterStores(t)
	enableIngestion(t, caps)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	srv.Close() // dead endpoint: pull fails before any accept/advance

	connector := NewConnector(NewClient(srv.URL, "tok", srv.Client()), caps, sink, cursors, adapterConfig())
	if _, err := connector.PollOnce(context.Background(), 0); err == nil {
		t.Error("poll once: expected client error")
	}
	if cur := storedCursor(t, cursors); cur != "" {
		t.Errorf("cursor = %q, want empty (failed pull advances nothing)", cur)
	}
}

func TestConnectorPollOnceKeepsOffsetOnSinkFailure(t *testing.T) {
	sink, caps, cursors, db := newAdapterStores(t)
	enableIngestion(t, caps)
	if err := db.Callback().Create().Before("gorm:create").Register("tg_sink_error",
		func(tx *gorm.DB) { _ = tx.AddError(errors.New("boom")) }); err != nil {
		t.Fatalf("register callback: %v", err)
	}

	fake := &botAPI{t: t}
	fake.onPoll = func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"ok":true,"result":[
			{"update_id":41,"message":{"message_id":7,"chat":{"id":-100123,"type":"supergroup"},
				"from":{"id":9001},"text":"hi","date":1697000000}}]}`))
	}
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	connector := NewConnector(NewClient(srv.URL, "tok", srv.Client()), caps, sink, cursors, adapterConfig())
	if _, err := connector.PollOnce(context.Background(), 0); err == nil {
		t.Error("poll once: expected sink error")
	}
	if cur := storedCursor(t, cursors); cur != "" {
		t.Errorf("cursor = %q, want empty (ack only after durable accept)", cur)
	}
}

func TestConnectorPollOnceStopsOnCursorSaveFailure(t *testing.T) {
	sink, caps, cursors, db := newAdapterStores(t)
	enableIngestion(t, caps)
	// Seed a cursor so advance() takes the update path, then break updates.
	scope := channel.CursorScope{
		TenantID: "tenant-1", WorkspaceID: "workspace-1", Platform: "telegram", InstallationID: "inst-1",
	}
	if err := cursors.Save(context.Background(), scope, "1"); err != nil {
		t.Fatalf("seed cursor: %v", err)
	}
	if err := db.Callback().Update().Before("gorm:update").Register("tg_cursor_error",
		func(tx *gorm.DB) { _ = tx.AddError(errors.New("cursor store down")) }); err != nil {
		t.Fatalf("register callback: %v", err)
	}

	fake := &botAPI{t: t}
	fake.onPoll = func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"ok":true,"result":[
			{"update_id":41,"message":{"message_id":7,"chat":{"id":-100123,"type":"supergroup"},
				"from":{"id":9001},"text":"hi","date":1697000000}}]}`))
	}
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	obs := &recordingObserver{}
	connector := NewConnector(NewClient(srv.URL, "tok", srv.Client()), caps, sink, cursors, adapterConfig()).WithObserver(obs)
	n, err := connector.PollOnce(context.Background(), 0)
	if err == nil || !strings.Contains(err.Error(), "cursor after 7") {
		t.Errorf("poll err = %v, want cursor failure naming the event", err)
	}
	if n != 0 {
		t.Errorf("processed = %d, want 0 (stops at first failure)", n)
	}
	if cur := storedCursor(t, cursors); cur != "1" {
		t.Errorf("cursor = %q, want 1 (advance failed)", cur)
	}
	var rows []channel.ChatConnectorInbox
	if err := db.Find(&rows).Error; err != nil || len(rows) != 1 ||
		rows[0].Status != channel.InboxStatusCommitted {
		t.Errorf("inbox rows = %+v (%v), want the committed row kept for redelivery-dedup", rows, err)
	}
}

func TestConnectorPollOnceCorruptCursorFailsClosed(t *testing.T) {
	sink, caps, cursors, _ := newAdapterStores(t)
	enableIngestion(t, caps)
	scope := channel.CursorScope{
		TenantID: "tenant-1", WorkspaceID: "workspace-1", Platform: "telegram", InstallationID: "inst-1",
	}
	if err := cursors.Save(context.Background(), scope, "not-a-number"); err != nil {
		t.Fatalf("seed corrupt cursor: %v", err)
	}

	var pulled atomic.Bool
	fake := &botAPI{t: t}
	fake.onPoll = func(w http.ResponseWriter, r *http.Request) {
		pulled.Store(true)
		_, _ = w.Write([]byte(`{"ok":true,"result":null}`))
	}
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	connector := NewConnector(NewClient(srv.URL, "tok", srv.Client()), caps, sink, cursors, adapterConfig())
	_, err := connector.PollOnce(context.Background(), 0)
	if err == nil || !strings.Contains(err.Error(), "not an update offset") {
		t.Errorf("poll err = %v, want corrupt-cursor failure", err)
	}
	if pulled.Load() {
		t.Error("a corrupt cursor must not poll (an offset query would re-drain everything)")
	}
}

func TestConnectorPollOnceCursorReadError(t *testing.T) {
	sink, caps, cursors, db := newAdapterStores(t)
	enableIngestion(t, caps)
	if err := db.Callback().Query().Before("gorm:query").Register("tg_cursor_read_error",
		func(tx *gorm.DB) {
			if tx.Statement.Table == "chat_connector_cursor" {
				_ = tx.AddError(errors.New("cursor store down"))
			}
		}); err != nil {
		t.Fatalf("register callback: %v", err)
	}
	fake := &botAPI{t: t}
	fake.onPoll = func(w http.ResponseWriter, r *http.Request) {}
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	connector := NewConnector(NewClient(srv.URL, "tok", srv.Client()), caps, sink, cursors, adapterConfig())
	_, err := connector.PollOnce(context.Background(), 0)
	if err == nil {
		t.Error("poll once: expected cursor read error")
	}
}

func TestConnectorCapabilitiesReadError(t *testing.T) {
	sink, caps, cursors, db := newAdapterStores(t)
	enableIngestion(t, caps)
	if err := db.Callback().Query().Before("gorm:query").Register("tg_caps_error",
		func(tx *gorm.DB) { _ = tx.AddError(errors.New("boom")) }); err != nil {
		t.Fatalf("register callback: %v", err)
	}
	fake := &botAPI{t: t}
	fake.onPoll = func(w http.ResponseWriter, r *http.Request) {}
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	connector := NewConnector(NewClient(srv.URL, "tok", srv.Client()), caps, sink, cursors, adapterConfig())
	if _, err := connector.PollOnce(context.Background(), 0); err == nil {
		t.Error("poll once: expected capabilities read error")
	}
	if _, err := connector.Notify(context.Background(), notifyRequest("-100123", "hello")); err == nil {
		t.Error("notify: expected capabilities read error")
	}
}

func TestConnectorPollOnceLongPollRoundsUp(t *testing.T) {
	sink, caps, cursors, _ := newAdapterStores(t)
	enableIngestion(t, caps)
	fake := &botAPI{t: t}
	fake.onPoll = func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"ok":true,"result":null}`))
	}
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	connector := NewConnector(NewClient(srv.URL, "tok", srv.Client()), caps, sink, cursors, adapterConfig())
	if _, err := connector.PollOnce(context.Background(), 25*time.Second); err != nil {
		t.Fatalf("poll: %v", err)
	}
	if _, timeouts, _ := fake.observed(); timeouts[0] != "25" {
		t.Errorf("timeout = %q, want 25 (seconds, contract cap stays under Bot API 50)", timeouts[0])
	}
}

func TestConnectorWithoutObserverStillPolls(t *testing.T) {
	sink, caps, cursors, _ := newAdapterStores(t)
	enableIngestion(t, caps)
	fake := &botAPI{t: t}
	fake.onPoll = func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"ok":true,"result":null}`))
	}
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	connector := NewConnector(NewClient(srv.URL, "tok", srv.Client()), caps, sink, cursors, adapterConfig())
	if n, err := connector.PollOnce(context.Background(), 0); err != nil || n != 0 {
		t.Fatalf("poll without observer = %d (%v), want 0 nil", n, err)
	}
}

func TestConnectorNotifyGatesAndSends(t *testing.T) {
	sink, caps, cursors, db := newAdapterStores(t)

	fake := &botAPI{t: t}
	fake.onPoll = func(w http.ResponseWriter, r *http.Request) {}
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	connector := NewConnector(NewClient(srv.URL, "tok", srv.Client()), caps, sink, cursors, adapterConfig())

	// no snapshot at all: fail closed without reaching the platform.
	if _, err := connector.Notify(context.Background(), notifyRequest("-100123", "hello")); !errors.Is(err, ErrSendDisabled) {
		t.Errorf("notify without snapshot = %v, want ErrSendDisabled", err)
	}
	if _, _, sends := fake.observed(); len(sends) != 0 {
		t.Errorf("send gate must not reach the platform: %v", sends)
	}

	enableIngestion(t, caps)
	// DeclaredCapabilities grants SendText: the send goes out.
	res, err := connector.Notify(context.Background(), notifyRequest("-100123", "hello"))
	if err != nil {
		t.Fatalf("notify: %v", err)
	}
	if res.PlatformMessageID != "77" || res.Status != channel.SendResultAccepted || res.AcceptedAt.IsZero() {
		t.Errorf("notify result = %+v, want accepted 77 with observed accepted_at", res)
	}
	if err := res.Validate(); err != nil {
		t.Errorf("notify result invalid: %v", err)
	}
	// Target is the chat id: surrounding whitespace tolerated.
	if _, err = connector.Notify(context.Background(), notifyRequest(" -100123 ", "hello")); err != nil {
		t.Fatalf("notify trimmed target: %v", err)
	}
	if _, _, sends := fake.observed(); len(sends) != 2 || sends[0] != "-100123|hello" {
		t.Errorf("sends = %v", sends)
	}
	if replies, _ := fake.observedSendChain(); len(replies) != 2 || replies[0] != "" {
		t.Errorf("notify must not carry a reply chain: %v", replies)
	}

	// capability flipped off afterwards: gate closes again.
	if err := db.Model(&channel.ChatConnectorCapability{}).Where("1=1").
		Update("send_text", false).Error; err != nil {
		t.Fatalf("flip send capability off: %v", err)
	}
	if _, err = connector.Notify(context.Background(), notifyRequest("-100123", "hello")); !errors.Is(err, ErrSendDisabled) {
		t.Errorf("notify with revoked send capability = %v, want ErrSendDisabled", err)
	}
}

func TestConnectorNotifyPropagatesSendFailure(t *testing.T) {
	sink, caps, cursors, _ := newAdapterStores(t)
	enableIngestion(t, caps)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"ok":false,"error_code":403,"description":"bot was kicked"}`))
	}))
	defer srv.Close()

	connector := NewConnector(NewClient(srv.URL, "tok", srv.Client()), caps, sink, cursors, adapterConfig())
	res, err := connector.Notify(context.Background(), notifyRequest("-100123", "hello"))
	var apiErr *AdapterError
	if res.PlatformMessageID != "" || !errors.As(err, &apiErr) || apiErr.Kind != ErrPermission {
		t.Errorf("notify = %+v, %v; want empty result and permission AdapterError", res, err)
	}
}

// sendTransportResponse is one scripted sendMessage reply for an injected
// http.RoundTripper; err short-circuits before any response is parsed.
type sendTransportResponse struct {
	status int
	body   string
	err    error
}

// scriptedSendTransport implements http.RoundTripper without a network socket:
// it snapshots every request and returns the queued responses in order.
type scriptedSendTransport struct {
	mu        sync.Mutex
	responses []sendTransportResponse
	calls     int
	requests  []string
}

func (s *scriptedSendTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := req.Context().Err(); err != nil {
		return nil, err
	}
	i := s.calls
	s.calls++
	if i >= len(s.responses) {
		return nil, errors.New("unexpected send transport call")
	}
	s.requests = append(s.requests, req.URL.RawQuery)
	resp := s.responses[i]
	if resp.err != nil {
		return nil, resp.err
	}
	return &http.Response{
		StatusCode: resp.status,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(resp.body)),
		Request:    req,
	}, nil
}

func (s *scriptedSendTransport) observed() (calls int, requests []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls, append([]string(nil), s.requests...)
}

func TestConnectorSendMapsPlatformRefusal(t *testing.T) {
	sink, caps, cursors, _ := newAdapterStores(t)
	enableIngestion(t, caps)
	st := &scriptedSendTransport{responses: []sendTransportResponse{{
		status: http.StatusOK,
		body:   `{"ok":false,"error_code":403,"description":"bot was kicked"}`,
	}}}
	connector := NewConnector(NewClient("", "tok", &http.Client{Transport: st}), caps, sink, cursors, adapterConfig())
	res, err := connector.Notify(context.Background(), notifyRequest("-100123", "hello"))
	var apiErr *AdapterError
	if res.Status != channel.SendResultRejected || res.PlatformMessageID != "" {
		t.Errorf("refusal result = %+v, want rejected without platform id", res)
	}
	if !errors.As(err, &apiErr) || apiErr.Kind != ErrPermission {
		t.Errorf("refusal err = %v, want permission AdapterError", err)
	}
	if calls, _ := st.observed(); calls != 1 {
		t.Errorf("send calls = %d, want 1 (permission is never retried)", calls)
	}
}

func TestConnectorSendTransportUncertaintyIsUnknown(t *testing.T) {
	sink, caps, cursors, _ := newAdapterStores(t)
	enableIngestion(t, caps)
	st := &scriptedSendTransport{responses: []sendTransportResponse{{err: io.ErrUnexpectedEOF}}}
	connector := NewConnector(NewClient("", "tok", &http.Client{Transport: st}), caps, sink, cursors, adapterConfig())
	res, err := connector.Send(context.Background(), sendRequest("-100123", "hello"))
	var apiErr *AdapterError
	if res.Status != channel.SendResultUnknown || res.PlatformMessageID != "" || !res.AcceptedAt.IsZero() {
		t.Errorf("transport uncertainty result = %+v, want unknown without delivery evidence", res)
	}
	if !errors.As(err, &apiErr) || apiErr.Kind != ErrTransient || apiErr.Status != 0 {
		t.Errorf("transport uncertainty err = %v, want transient status 0", err)
	}
	if calls, _ := st.observed(); calls != 1 {
		t.Errorf("send calls = %d, want 1 (ambiguous delivery is never blindly retried)", calls)
	}
}

func TestConnectorSendRetriesEnvelopeRetryAfter(t *testing.T) {
	sink, caps, cursors, _ := newAdapterStores(t)
	enableIngestion(t, caps)
	st := &scriptedSendTransport{responses: []sendTransportResponse{
		{status: http.StatusOK, body: `{"ok":false,"error_code":429,"parameters":{"retry_after":2}}`},
		{status: http.StatusOK, body: `{"ok":true,"result":{"message_id":91}}`},
	}}
	connector := NewConnector(NewClient("", "tok", &http.Client{Transport: st}), caps, sink, cursors, adapterConfig())
	connector.retry.MaxAttempts = 2
	connector.retry.MaxBackoff = 30 * time.Second
	var waits []time.Duration
	connector.retry.Sleep = func(ctx context.Context, d time.Duration) error {
		waits = append(waits, d)
		return nil
	}
	res, err := connector.Notify(context.Background(), notifyRequest("-100123", "hello"))
	if err != nil {
		t.Fatalf("notify after retry: %v", err)
	}
	if res.Status != channel.SendResultAccepted || res.PlatformMessageID != "91" || res.AcceptedAt.IsZero() {
		t.Errorf("retried result = %+v, want accepted 91", res)
	}
	if calls, _ := st.observed(); calls != 2 {
		t.Errorf("send calls = %d, want 2", calls)
	}
	if len(waits) != 1 || waits[0] != 2*time.Second {
		t.Errorf("retry waits = %v, want exactly [2s] (server Retry-After wins)", waits)
	}
}

func TestConnectorSendRetriesWithoutRetryAfterWithBackoff(t *testing.T) {
	sink, caps, cursors, _ := newAdapterStores(t)
	enableIngestion(t, caps)
	st := &scriptedSendTransport{responses: []sendTransportResponse{
		{status: http.StatusOK, body: `{"ok":false,"error_code":429}`},
		{status: http.StatusOK, body: `{"ok":true,"result":{"message_id":92}}`},
	}}
	connector := NewConnector(NewClient("", "tok", &http.Client{Transport: st}), caps, sink, cursors, adapterConfig())
	connector.retry.MaxAttempts = 2
	connector.retry.MinBackoff = 2 * time.Second
	connector.retry.MaxBackoff = 4 * time.Second
	var waits []time.Duration
	connector.retry.Sleep = func(ctx context.Context, d time.Duration) error {
		waits = append(waits, d)
		return nil
	}
	res, err := connector.Notify(context.Background(), notifyRequest("-100123", "hello"))
	if err != nil || res.Status != channel.SendResultAccepted || res.PlatformMessageID != "92" {
		t.Fatalf("backoff retry = %+v (%v), want accepted 92", res, err)
	}
	if len(waits) != 1 || waits[0] < time.Second || waits[0] > 2*time.Second {
		t.Errorf("backoff wait = %v, want full-jitter inside [1s,2s]", waits)
	}
}

func TestConnectorSendRetryBudgetStops(t *testing.T) {
	sink, caps, cursors, _ := newAdapterStores(t)
	enableIngestion(t, caps)
	rateLimited := sendTransportResponse{status: http.StatusOK, body: `{"ok":false,"error_code":429}`}
	st := &scriptedSendTransport{responses: []sendTransportResponse{rateLimited, rateLimited, rateLimited}}
	connector := NewConnector(NewClient("", "tok", &http.Client{Transport: st}), caps, sink, cursors, adapterConfig())
	connector.retry.MaxAttempts = 3
	connector.retry.MinBackoff = time.Millisecond
	connector.retry.MaxBackoff = 2 * time.Millisecond
	var waits []time.Duration
	connector.retry.Sleep = func(ctx context.Context, d time.Duration) error {
		waits = append(waits, d)
		return nil
	}
	res, err := connector.Notify(context.Background(), notifyRequest("-100123", "hello"))
	var apiErr *AdapterError
	if res.Status != channel.SendResultRejected || !errors.As(err, &apiErr) || apiErr.Kind != ErrRateLimited {
		t.Errorf("budget result = %+v, %v; want rejected rate_limited", res, err)
	}
	if !strings.Contains(err.Error(), "after 3 attempts") || !strings.Contains(err.Error(), "notify-1") {
		t.Errorf("budget err = %v, want attempt count and caller idempotency key", err)
	}
	if calls, _ := st.observed(); calls != 3 {
		t.Errorf("send calls = %d, want 3 (budget is attempts, no extra call)", calls)
	}
	if len(waits) != 2 {
		t.Errorf("backoff waits = %v, want 2 pauses before the final refusal", waits)
	}
}

func TestConnectorSendDeadlineBoundsAttempt(t *testing.T) {
	sink, caps, cursors, _ := newAdapterStores(t)
	enableIngestion(t, caps)
	st := &scriptedSendTransport{responses: []sendTransportResponse{{
		status: http.StatusOK,
		body:   `{"ok":true,"result":{"message_id":93}}`,
	}}}
	connector := NewConnector(NewClient("", "tok", &http.Client{Transport: st}), caps, sink, cursors, adapterConfig())
	req := sendRequest("-100123", "hello")
	req.Deadline = time.Now().Add(-time.Second)
	res, err := connector.Send(context.Background(), req)
	var apiErr *AdapterError
	if res.Status != channel.SendResultUnknown || !errors.As(err, &apiErr) || apiErr.Kind != ErrTransient {
		t.Errorf("deadline result = %+v, %v; want unknown transient", res, err)
	}
}

func TestConnectorSendSleepCancellationIsUnknown(t *testing.T) {
	sink, caps, cursors, _ := newAdapterStores(t)
	enableIngestion(t, caps)
	st := &scriptedSendTransport{responses: []sendTransportResponse{{
		status: http.StatusOK,
		body:   `{"ok":false,"error_code":429}`,
	}}}
	connector := NewConnector(NewClient("", "tok", &http.Client{Transport: st}), caps, sink, cursors, adapterConfig())
	connector.retry.Sleep = func(ctx context.Context, d time.Duration) error { return context.Canceled }
	res, err := connector.Notify(context.Background(), notifyRequest("-100123", "hello"))
	if res.Status != channel.SendResultUnknown || !errors.Is(err, context.Canceled) {
		t.Errorf("cancelled backoff result = %+v, %v; want unknown context cancellation", res, err)
	}
}

func TestConnectorNotifyRejectsNonChatTargets(t *testing.T) {
	sink, caps, cursors, _ := newAdapterStores(t)
	enableIngestion(t, caps)
	fake := &botAPI{t: t}
	fake.onPoll = func(w http.ResponseWriter, r *http.Request) {}
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	connector := NewConnector(NewClient(srv.URL, "tok", srv.Client()), caps, sink, cursors, adapterConfig())
	for _, target := range []string{"", "group:5", "not-a-chat", "0", "  "} {
		_, err := connector.Notify(context.Background(), notifyRequest(target, "hello"))
		invalidEnvelope := strings.TrimSpace(target) == ""
		want := "notify request requires target"
		if !invalidEnvelope {
			want = "not an integer"
		}
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("notify target %q: err = %v, want rejection containing %q", target, err, want)
		}
	}
	if _, _, sends := fake.observed(); len(sends) != 0 {
		t.Errorf("rejected targets must not reach the platform: %v", sends)
	}
}

func notifyRequest(target, text string) channel.NotifyRequest {
	return channel.NotifyRequest{
		Target:         target,
		Reason:         "test announcement",
		IdempotencyKey: "notify-1",
		Content:        channel.SendContent{Text: text},
	}
}

func sendRequest(conversationID, text string) channel.SendRequest {
	return channel.SendRequest{
		ConversationID: conversationID,
		IdempotencyKey: "send-1",
		Content:        channel.SendContent{Text: text},
	}
}

func TestConnectorSendReplyChain(t *testing.T) {
	sink, caps, cursors, _ := newAdapterStores(t)
	enableIngestion(t, caps)
	fake := &botAPI{t: t}
	fake.onPoll = func(w http.ResponseWriter, r *http.Request) {}
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	connector := NewConnector(NewClient(srv.URL, "tok", srv.Client()), caps, sink, cursors, adapterConfig())
	req := sendRequest("-100123", "replying")
	req.ReplyToMessageID = "42"
	req.ThreadID = "7"
	res, err := connector.Send(context.Background(), req)
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if res.PlatformMessageID != "77" || res.Status != channel.SendResultAccepted || res.AcceptedAt.IsZero() {
		t.Errorf("send result = %+v, want accepted 77 with observed accepted_at", res)
	}
	if err := res.Validate(); err != nil {
		t.Errorf("send result invalid: %v", err)
	}
	replies, threads := fake.observedSendChain()
	if len(replies) != 1 || replies[0] != "42" {
		t.Errorf("reply chain = %v, want [42] (reply_to_message_id rides sendMessage)", replies)
	}
	if len(threads) != 1 || threads[0] != "7" {
		t.Errorf("thread chain = %v, want [7]", threads)
	}
	if _, _, sends := fake.observed(); len(sends) != 1 || sends[0] != "-100123|replying" {
		t.Errorf("sends = %v, want the single reply to the conversation", sends)
	}
}

func TestConnectorSendRejectsInvalidBeforePlatform(t *testing.T) {
	sink, caps, cursors, _ := newAdapterStores(t)
	enableIngestion(t, caps)
	fake := &botAPI{t: t}
	fake.onPoll = func(w http.ResponseWriter, r *http.Request) {}
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	connector := NewConnector(NewClient(srv.URL, "tok", srv.Client()), caps, sink, cursors, adapterConfig())
	invalid := []struct {
		name string
		req  channel.SendRequest
		want string
	}{
		{"missing conversation", channel.SendRequest{IdempotencyKey: "k", Content: channel.SendContent{Text: "x"}}, "conversation"},
		{"missing idempotency key", channel.SendRequest{ConversationID: "-100123", Content: channel.SendContent{Text: "x"}}, "idempotency"},
		{"empty content", channel.SendRequest{ConversationID: "-100123", IdempotencyKey: "k"}, "content"},
		{"non-integer conversation", channel.SendRequest{ConversationID: "group:5", IdempotencyKey: "k", Content: channel.SendContent{Text: "x"}}, "not an integer"},
		{"non-positive reply id", func() channel.SendRequest {
			r := sendRequest("-100123", "x")
			r.ReplyToMessageID = "0"
			return r
		}(), "reply_to_message_id"},
		{"non-positive thread id", func() channel.SendRequest {
			r := sendRequest("-100123", "x")
			r.ThreadID = "-1"
			return r
		}(), "message_thread_id"},
	}
	for _, tc := range invalid {
		if _, err := connector.Send(context.Background(), tc.req); err == nil ||
			!strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v, want rejection containing %q", tc.name, err, tc.want)
		}
	}
	if _, _, sends := fake.observed(); len(sends) != 0 {
		t.Errorf("invalid sends must not reach the platform: %v", sends)
	}
}

func TestConnectorSendGatesAndRejectsRichContent(t *testing.T) {
	sink, caps, cursors, db := newAdapterStores(t)
	fake := &botAPI{t: t}
	fake.onPoll = func(w http.ResponseWriter, r *http.Request) {}
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	connector := NewConnector(NewClient(srv.URL, "tok", srv.Client()), caps, sink, cursors, adapterConfig())

	// No observed snapshot: the outbound gate is fail-closed.
	if _, err := connector.Send(context.Background(), sendRequest("-100123", "x")); !errors.Is(err, ErrSendDisabled) {
		t.Errorf("send without snapshot = %v, want ErrSendDisabled", err)
	}

	enableIngestion(t, caps)
	// A rich body on the TEXT surface is refused rather than silently dropped.
	rich := sendRequest("-100123", "")
	rich.Content = channel.SendContent{ContentParts: []channel.ContentPart{{Kind: "file", Text: "not-real"}}}
	if _, err := connector.Send(context.Background(), rich); !errors.Is(err, ErrRichSendUnsupported) {
		t.Errorf("rich send = %v, want ErrRichSendUnsupported", err)
	}

	// Capability revoked afterwards: the gate closes again.
	if err := db.Model(&channel.ChatConnectorCapability{}).Where("1=1").
		Update("send_text", false).Error; err != nil {
		t.Fatalf("flip send capability off: %v", err)
	}
	if _, err := connector.Send(context.Background(), sendRequest("-100123", "x")); !errors.Is(err, ErrSendDisabled) {
		t.Errorf("send with revoked capability = %v, want ErrSendDisabled", err)
	}
	if _, _, sends := fake.observed(); len(sends) != 0 {
		t.Errorf("gated/refused sends must not reach the platform: %v", sends)
	}
}

// recordingObserver plays the assembly ingress bridge: it records every
// committed event id it is shown and can be scripted to fail.
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

func (o *recordingObserver) forwardedEvents() []string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return append([]string(nil), o.forwarded...)
}

func TestConnectorPollOnceObserverErrorAbortsBeforeCursorSave(t *testing.T) {
	sink, caps, cursors, db := newAdapterStores(t)
	enableIngestion(t, caps)

	fake := &botAPI{t: t}
	fake.onPoll = func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"ok":true,"result":[
			{"update_id":41,"message":{"message_id":7,"chat":{"id":-100123,"type":"supergroup"},
				"from":{"id":9001},"text":"hi","date":1697000000}}]}`))
	}
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	obs := &recordingObserver{err: errors.New("conversation store down")}
	connector := NewConnector(NewClient(srv.URL, "tok", srv.Client()), caps, sink, cursors, adapterConfig()).WithObserver(obs)

	n, err := connector.PollOnce(context.Background(), 0)
	if err == nil {
		t.Fatal("observer failure must abort the poll")
	}
	if n != 0 {
		t.Errorf("processed = %d, want 0", n)
	}
	if cur := storedCursor(t, cursors); cur != "" {
		t.Errorf("cursor = %q, want empty (abort before the offset ack)", cur)
	}
	var rows []channel.ChatConnectorInbox
	if err := db.Find(&rows).Error; err != nil {
		t.Fatalf("list inbox: %v", err)
	}
	if len(rows) != 1 || rows[0].Status != channel.InboxStatusCommitted {
		t.Errorf("inbox rows = %+v, want the committed row kept", rows)
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

func TestConnectorPollOnceRetainsRawPayloadOnRows(t *testing.T) {
	inbox, caps, cursors, db := newAdapterStores(t)
	enableIngestion(t, caps)
	store, _ := newTestPayloadStore(t)

	rawMessage := `{"update_id":41,"message":{"message_id":7,"chat":{"id":-100123,"type":"supergroup"},"from":{"id":9001},"text":"hello retention","date":1697000000}}`
	rawQuarantine := `{"update_id":42}`
	fake := &botAPI{t: t, onPoll: func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"ok":true,"result":[` + rawMessage + `,` + rawQuarantine + `]}`))
	}}
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	conn := NewConnector(NewClient(srv.URL, "tok", srv.Client()), caps, inbox, cursors, adapterConfig()).
		WithPayloadStore(store)
	n, err := conn.PollOnce(context.Background(), 1500*time.Millisecond)
	if err != nil || n != 2 {
		t.Fatalf("poll once = %d (%v), want 2 nil", n, err)
	}
	if cur := storedCursor(t, cursors); cur != "43" {
		t.Errorf("cursor = %q, want 43 (retention must not stall the frontier)", cur)
	}

	// the committed row points at the exact bytes the platform sent
	var row channel.ChatConnectorInbox
	if err := db.Where("tenant_id = ? AND message_platform_id = ?", "tenant-1", "7").
		Take(&row).Error; err != nil {
		t.Fatalf("committed row: %v", err)
	}
	if row.RawPayloadStore != channel.LocalPayloadStoreName || len(row.RawPayloadObjectID) != 32 {
		t.Errorf("row ref = %s/%q, want local-encrypted/32-hex", row.RawPayloadStore, row.RawPayloadObjectID)
	}
	sum := sha256.Sum256([]byte(rawMessage))
	ref := channel.RawPayloadRef{Store: row.RawPayloadStore, ObjectID: row.RawPayloadObjectID,
		SHA256: hex.EncodeToString(sum[:])}
	got, err := store.Open(context.Background(), ref)
	if err != nil || string(got) != rawMessage {
		t.Errorf("open retained = %q (%v), want the exact platform bytes", got, err)
	}
	meta, err := store.Meta(context.Background(), channel.RawPayloadRef{Store: row.RawPayloadStore, ObjectID: row.RawPayloadObjectID})
	if err != nil || meta.TenantID != "tenant-1" || meta.Platform != "telegram" ||
		meta.InstallationID != "inst-1" || meta.EventID != "7" || meta.ContentType != channel.RawPayloadContentType {
		t.Errorf("retained meta = %+v (%v), want the row scope", meta, err)
	}

	// the quarantined shape keeps its bytes too: that lane is what triage reads
	var qrow channel.ChatConnectorInbox
	if err := db.Where("event_id = ?", "update-42").Take(&qrow).Error; err != nil {
		t.Fatalf("quarantine row: %v", err)
	}
	if qrow.RawPayloadStore != channel.LocalPayloadStoreName || qrow.RawPayloadObjectID == "" {
		t.Errorf("quarantine ref = %s/%q, want retained", qrow.RawPayloadStore, qrow.RawPayloadObjectID)
	}
	if qrow.RawPayloadObjectID == row.RawPayloadObjectID {
		t.Error("distinct updates must not share one object")
	}
}

func TestConnectorPollOnceDuplicateRedeliveryDropsOrphan(t *testing.T) {
	inbox, caps, cursors, db := newAdapterStores(t)
	enableIngestion(t, caps)
	store, root := newTestPayloadStore(t)

	rawUpdate := `{"update_id":41,"message":{"message_id":7,"chat":{"id":-100123,"type":"supergroup"},"text":"again","date":1697000000}}`
	polls := 0
	fake := &botAPI{t: t, onPoll: func(w http.ResponseWriter, r *http.Request) {
		polls++
		_, _ = w.Write([]byte(`{"ok":true,"result":[` + rawUpdate + `]}`))
	}}
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	conn := NewConnector(NewClient(srv.URL, "tok", srv.Client()), caps, inbox, cursors, adapterConfig()).
		WithPayloadStore(store)
	for i := 0; i < 2; i++ {
		if n, err := conn.PollOnce(context.Background(), 1500*time.Millisecond); err != nil || n != 1 {
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
	inbox, caps, cursors, db := newAdapterStores(t)
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

	fake := &botAPI{t: t, onPoll: func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"ok":true,"result":[{"update_id":41,"message":{"message_id":7,"chat":{"id":-100123,"type":"supergroup"},"text":"hi","date":1697000000}}]}`))
	}}
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	conn := NewConnector(NewClient(srv.URL, "tok", srv.Client()), caps, inbox, cursors, adapterConfig()).
		WithPayloadStore(badStore)
	n, err := conn.PollOnce(context.Background(), 1500*time.Millisecond)
	if err == nil || n != 0 {
		t.Fatalf("poll = %d (%v), want 0 with a retention error", n, err)
	}
	if cur := storedCursor(t, cursors); cur != "" {
		t.Errorf("cursor = %q, want empty (the update stays for redelivery)", cur)
	}
	var rows []channel.ChatConnectorInbox
	if err := db.Find(&rows).Error; err != nil || len(rows) != 0 {
		t.Errorf("inbox rows = %d (%v), want none", len(rows), err)
	}
}

func TestConnectorWithoutPayloadStoreKeepsNoRef(t *testing.T) {
	inbox, caps, cursors, db := newAdapterStores(t)
	enableIngestion(t, caps)
	fake := &botAPI{t: t, onPoll: func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"ok":true,"result":[{"update_id":41,"message":{"message_id":7,"chat":{"id":-100123,"type":"supergroup"},"text":"hi","date":1697000000}}]}`))
	}}
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	conn := NewConnector(NewClient(srv.URL, "tok", srv.Client()), caps, inbox, cursors, adapterConfig())
	if _, err := conn.PollOnce(context.Background(), 1500*time.Millisecond); err != nil {
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

func TestConnectorCapabilitiesObservesOwnInstallation(t *testing.T) {
	_, caps, _, _ := newAdapterStores(t)
	enableIngestion(t, caps)
	fake := &botAPI{t: t}
	fake.onPoll = func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"ok":true,"result":[]}`))
	}
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	inbox := channel.NewInboxStore(nil)
	cursors := channel.NewCursorStore(nil)
	connector := NewConnector(NewClient(srv.URL, "tok", srv.Client()), caps, inbox, cursors, adapterConfig())
	got, err := connector.Capabilities(context.Background(), "inst-1")
	if err != nil {
		t.Fatalf("capabilities: %v", err)
	}
	if !got.IngestionEnabled() || !got.SendText {
		t.Errorf("snapshot = %+v, want recorded ingest+send grant", got)
	}
	if _, err := connector.Capabilities(context.Background(), "inst-2"); err == nil {
		t.Error("capabilities for another installation: want error (one connector observes one installation)")
	}
}

func TestConnectorCapabilitiesFailClosedWithoutSnapshot(t *testing.T) {
	_, caps, _, _ := newAdapterStores(t)
	fake := &botAPI{t: t}
	fake.onPoll = func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"ok":true,"result":[]}`))
	}
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	inbox := channel.NewInboxStore(nil)
	cursors := channel.NewCursorStore(nil)
	connector := NewConnector(NewClient(srv.URL, "tok", srv.Client()), caps, inbox, cursors, adapterConfig())
	got, err := connector.Capabilities(context.Background(), "inst-1")
	if err != nil {
		t.Fatalf("capabilities: %v", err)
	}
	if got.IngestionEnabled() {
		t.Errorf("unrecorded snapshot must surface fail-closed, got %+v", got)
	}
}

func TestConnectorCapabilitiesStoreErrorSurfaces(t *testing.T) {
	_, caps, _, db := newAdapterStores(t)
	// Close the DB so the capability store read fails: the interface must
	// surface the store error, never fabricate a grant.
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("sql db: %v", err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatalf("close db: %v", err)
	}
	fake := &botAPI{t: t}
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	connector := NewConnector(NewClient(srv.URL, "tok", srv.Client()), caps, channel.NewInboxStore(nil),
		channel.NewCursorStore(nil), adapterConfig())
	if _, err := connector.Capabilities(context.Background(), "inst-1"); err == nil {
		t.Error("closed store: want error from Capabilities")
	}
}

func TestConnectorReceiveDrainsThroughSuppliedSink(t *testing.T) {
	boundSink, caps, cursors, db := newAdapterStores(t)
	enableIngestion(t, caps)
	// A distinct sink the caller hands to Receive, so we can prove the drain
	// uses the interface argument rather than the connector's bound store.
	callerSink, _, callerCursors, callerDB := newAdapterStores(t)

	fake := &botAPI{t: t}
	fake.onPoll = func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("timeout"); got != "25" {
			t.Errorf("timeout = %q, want 25s (contract default)", got)
		}
		_, _ = w.Write([]byte(`{"ok":true,"result":[{"update_id":41,"message":{"message_id":7,"chat":{"id":-100123,"type":"supergroup"},"text":"hi","date":1697000000}}]}`))
	}
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	connector := NewConnector(NewClient(srv.URL, "tok", srv.Client()), caps, boundSink, cursors, adapterConfig())
	if err := connector.Receive(context.Background(), callerSink); err != nil {
		t.Fatalf("receive: %v", err)
	}
	var bound []channel.ChatConnectorInbox
	if err := db.Find(&bound).Error; err != nil || len(bound) != 0 {
		t.Errorf("bound sink rows = %d (%v), want none (drain must use the supplied sink)", len(bound), err)
	}
	var caller []channel.ChatConnectorInbox
	if err := callerDB.Find(&caller).Error; err != nil || len(caller) != 1 {
		t.Errorf("caller sink rows = %d (%v), want 1", len(caller), err)
	}
	// The cursor is connector-owned state (not the sink argument): Receive advances
	// the bound connector's frontier, so the caller-supplied cursor store stays empty.
	if cur := storedCursor(t, callerCursors); cur != "" {
		t.Errorf("caller cursor = %q, want empty (cursor is connector-owned)", cur)
	}
	if cur := storedCursor(t, cursors); cur != "42" {
		t.Errorf("bound cursor = %q, want 42", cur)
	}
}

func TestConnectorReceiveGatesOnCapabilities(t *testing.T) {
	boundSink, caps, cursors, _ := newAdapterStores(t)
	fake := &botAPI{t: t}
	fake.onPoll = func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"ok":true,"result":[]}`))
	}
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	connector := NewConnector(NewClient(srv.URL, "tok", srv.Client()), caps, boundSink, cursors, adapterConfig())
	if err := connector.Receive(context.Background(), channel.NewInboxStore(nil)); !errors.Is(err, ErrIngestionDisabled) {
		t.Errorf("receive without snapshot = %v, want ErrIngestionDisabled", err)
	}
}
