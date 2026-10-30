package server

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"servify/apps/server/internal/config"
	"servify/apps/server/internal/models"
	conversationapp "servify/apps/server/internal/modules/conversation/application"
	conversationdomain "servify/apps/server/internal/modules/conversation/domain"
	conversationinfra "servify/apps/server/internal/modules/conversation/infra"
	"servify/apps/server/internal/platform/channel"
	"servify/apps/server/internal/platform/channel/chatconnector"
	"servify/apps/server/internal/platform/channel/memex"
	"servify/apps/server/internal/platform/channel/telegram"

	"github.com/glebarez/sqlite"
	"github.com/sirupsen/logrus"
	"github.com/sirupsen/logrus/hooks/test"
	"gorm.io/gorm"
)

func chatEvent(platform, installationID, tenantID, workspaceID, conversationID, msgID, text string) channel.ConnectorEvent {
	return channel.ConnectorEvent{
		Platform:       platform,
		InstallationID: installationID,
		TenantID:       tenantID,
		WorkspaceID:    workspaceID,
		EventID:        msgID,
		IdempotencyKey: channel.InboundIdempotencyKey(installationID, msgID),
		EventType:      channel.EventTypeMessageCreated,
		Conversation:   channel.ConversationRef{PlatformID: conversationID, Kind: "group", IsGroup: true},
		Sender:         channel.SenderRef{PlatformID: "alice"},
		Message:        &channel.MessageContent{PlatformID: msgID, Kind: "text", Text: text},
	}
}

// stubChatIngestor mirrors the email bridge test: ResumeConversation reports
// gorm.ErrRecordNotFound for unknown ids, CreateConversation registers them.
// resumeErr/createErr simulate storage failures that are not not-found.
type stubChatIngestor struct {
	existing  map[string]bool
	created   []conversationapp.CreateConversationCommand
	ingested  []conversationapp.IngestTextMessageCommand
	ingestErr error
	resumeErr error
	createErr error
}

func newStubChatIngestor() *stubChatIngestor {
	return &stubChatIngestor{existing: map[string]bool{}}
}

func (s *stubChatIngestor) ResumeConversation(ctx context.Context, query conversationapp.ResumeConversationQuery) (*conversationapp.ConversationDTO, error) {
	if s.resumeErr != nil {
		return nil, s.resumeErr
	}
	if s.existing[query.ConversationID] {
		return &conversationapp.ConversationDTO{ID: query.ConversationID}, nil
	}
	return nil, gorm.ErrRecordNotFound
}

func (s *stubChatIngestor) CreateConversation(ctx context.Context, cmd conversationapp.CreateConversationCommand) (*conversationapp.ConversationDTO, error) {
	if s.createErr != nil {
		return nil, s.createErr
	}
	s.created = append(s.created, cmd)
	s.existing[cmd.ConversationID] = true
	return &conversationapp.ConversationDTO{ID: cmd.ConversationID}, nil
}

func (s *stubChatIngestor) IngestTextMessage(ctx context.Context, cmd conversationapp.IngestTextMessageCommand) (*conversationapp.ConversationMessageDTO, error) {
	if s.ingestErr != nil {
		return nil, s.ingestErr
	}
	s.ingested = append(s.ingested, cmd)
	return &conversationapp.ConversationMessageDTO{}, nil
}

func TestChatConversationIDStableAndDistinct(t *testing.T) {
	a := ChatConversationID("tenant-1", "workspace-1", "memex", "group:5")
	b := ChatConversationID("tenant-1", "workspace-1", "memex", "group:5")
	if a != b || len(a) != len("chat-")+16 {
		t.Fatalf("mapping hash mismatch: %q vs %q", a, b)
	}
	if a == ChatConversationID("tenant-1", "workspace-1", "memex", "group:6") {
		t.Fatal("different conversations must not collide")
	}
	if a == ChatConversationID("tenant-1", "workspace-1", "telegram", "group:5") {
		t.Fatal("different platforms must not collide")
	}
	if a == ChatConversationID("tenant-2", "workspace-1", "memex", "group:5") {
		t.Fatal("different tenants must not collide (sessions.id is a primary key)")
	}
}

func TestChatBridgeCreatesOnFirstEvent(t *testing.T) {
	ingestor := newStubChatIngestor()
	bridge := &chatConversationBridge{ingestor: ingestor}

	ev := chatEvent("memex", "inst-1", "tenant-1", "workspace-1", "group:5", "m1", "hello bot")
	if err := bridge.InboundCommitted(context.Background(), ev); err != nil {
		t.Fatalf("ingest: %v", err)
	}
	if len(ingestor.created) != 1 {
		t.Fatalf("expected 1 create, got %d", len(ingestor.created))
	}
	created := ingestor.created[0]
	wantID := ChatConversationID("tenant-1", "workspace-1", "memex", "group:5")
	if created.ConversationID != wantID {
		t.Fatalf("conversation id = %q, want %q", created.ConversationID, wantID)
	}
	if created.Channel.Channel != "memex" || created.Channel.ExternalID != "group:5" || created.Channel.SessionID != wantID {
		t.Fatalf("channel binding mismatch: %+v", created.Channel)
	}
	if len(ingestor.ingested) != 1 {
		t.Fatalf("expected 1 ingest, got %d", len(ingestor.ingested))
	}
	msg := ingestor.ingested[0]
	if msg.ConversationID != wantID || msg.Content != "hello bot" || msg.MessageID != "m1" {
		t.Fatalf("ingest mismatch: %+v", msg)
	}
	if msg.Sender != conversationdomain.ParticipantRoleCustomer {
		t.Fatalf("sender must be customer, got %v", msg.Sender)
	}
	if msg.Metadata["source"] != "memex" || msg.Metadata["installation_id"] != "inst-1" || msg.Metadata["sender"] != "alice" {
		t.Fatalf("metadata mismatch: %+v", msg.Metadata)
	}
}

func TestChatBridgeResumesExistingConversation(t *testing.T) {
	ingestor := newStubChatIngestor()
	bridge := &chatConversationBridge{ingestor: ingestor}
	id := ChatConversationID("tenant-1", "workspace-1", "memex", "group:5")
	ingestor.existing[id] = true

	if err := bridge.InboundCommitted(context.Background(),
		chatEvent("memex", "inst-1", "tenant-1", "workspace-1", "group:5", "m2", "again")); err != nil {
		t.Fatalf("ingest: %v", err)
	}
	if len(ingestor.created) != 0 {
		t.Fatalf("existing conversation must not be recreated, got %d creates", len(ingestor.created))
	}
	if len(ingestor.ingested) != 1 {
		t.Fatalf("expected 1 ingest, got %d", len(ingestor.ingested))
	}
}

func TestChatBridgeSwallowsIngestFailure(t *testing.T) {
	logger, hook := test.NewNullLogger()
	ingestor := newStubChatIngestor()
	ingestor.ingestErr = errors.New("conversation store down")
	bridge := &chatConversationBridge{ingestor: ingestor, logger: logger}

	// the inbox row is already durable; a failed forward must not take the
	// poll loop down (the observer error contract aborts it) — it is logged
	// and left for replay
	err := bridge.InboundCommitted(context.Background(),
		chatEvent("memex", "inst-1", "tenant-1", "workspace-1", "group:5", "m1", "hi"))
	if err != nil {
		t.Fatalf("ingest failure must be swallowed: %v", err)
	}
	if err := bridge.InboundCommitted(context.Background(),
		chatEvent("memex", "inst-1", "tenant-1", "workspace-1", "group:5", "m1", "hi")); err != nil {
		t.Fatalf("second ingest: %v", err)
	}
	found := false
	for _, e := range hook.AllEntries() {
		if strings.Contains(e.Message, "conversation ingest failed") {
			found = true
		}
	}
	if !found {
		t.Error("swallowed ingest failure must be logged")
	}
}

func TestChatBridgeSkipsMessagelessEvent(t *testing.T) {
	ingestor := newStubChatIngestor()
	bridge := &chatConversationBridge{ingestor: ingestor}
	ev := chatEvent("memex", "inst-1", "t", "w", "group:5", "m1", "x")
	ev.Message = nil
	if err := bridge.InboundCommitted(context.Background(), ev); err != nil {
		t.Fatalf("messageless event: %v", err)
	}
	if len(ingestor.created) != 0 || len(ingestor.ingested) != 0 {
		t.Fatalf("messageless event must not touch the conversation module: %+v", ingestor)
	}
}

func TestChatBridgeScopeStampsAndIsolates(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(uniqueMemDSN("file:chat_bridge_scope")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&models.User{}, &models.Session{}, &models.Message{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	service := conversationapp.NewService(conversationinfra.NewGormRepository(db), nil)
	bridge := &chatConversationBridge{ingestor: service}

	if err := bridge.InboundCommitted(context.Background(),
		chatEvent("memex", "inst-1", "tenant-1", "workspace-1", "group:5", "m1", "hello")); err != nil {
		t.Fatalf("ingest: %v", err)
	}
	var sessions []models.Session
	if err := db.Find(&sessions).Error; err != nil {
		t.Fatalf("list sessions: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("sessions = %d, want 1", len(sessions))
	}
	if sessions[0].TenantID != "tenant-1" || sessions[0].WorkspaceID != "workspace-1" {
		t.Fatalf("session scope drifted: %+v", sessions[0])
	}
	if sessions[0].Platform != "memex" {
		t.Fatalf("session platform = %q, want memex", sessions[0].Platform)
	}
	var msgs []models.Message
	if err := db.Find(&msgs).Error; err != nil {
		t.Fatalf("list messages: %v", err)
	}
	if len(msgs) != 1 || msgs[0].Content != "hello" ||
		msgs[0].TenantID != "tenant-1" || msgs[0].SessionID != sessions[0].ID {
		t.Fatalf("message rows drifted: %+v", msgs)
	}

	// the same platform conversation under another tenant is a different
	// conversation: scope comes from the installation row, not the payload
	if err := bridge.InboundCommitted(context.Background(),
		chatEvent("memex", "inst-2", "tenant-2", "workspace-2", "group:5", "m1", "hello")); err != nil {
		t.Fatalf("second-scope ingest: %v", err)
	}
	if err := db.Find(&sessions).Error; err != nil {
		t.Fatalf("relist sessions: %v", err)
	}
	if len(sessions) != 2 {
		t.Fatalf("sessions = %d, want 2 (tenant-2 must not resume tenant-1's)", len(sessions))
	}
}

func TestWireChatConnectorsBuildsSupervisor(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(uniqueMemDSN("file:chat_wire")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	rt := &Runtime{
		Config: config.GetDefaultConfig(),
		Logger: func() *logrus.Logger { l := logrus.New(); l.SetOutput(io.Discard); return l }(),
		DB:     db,
	}
	if rt.ChatConnectorsForWorker() != nil {
		t.Fatal("chat connectors must start nil before wiring")
	}
	wireChatConnectors(rt, nil)
	if rt.ChatConnectorsForWorker() == nil {
		t.Fatal("wireChatConnectors must build the supervisor")
	}
	if rt.chatNotifier == nil {
		t.Fatal("wireChatConnectors must build the notify dispatcher")
	}

	// a store-less runtime wires nothing
	rt2 := &Runtime{Config: config.GetDefaultConfig()}
	wireChatConnectors(rt2, nil)
	if rt2.ChatConnectorsForWorker() != nil {
		t.Fatal("nil DB must wire nothing")
	}
	if rt2.chatNotifier != nil {
		t.Fatal("nil DB must wire no notifier")
	}
}

func TestChatConnectorTokenGateSkipsRow(t *testing.T) {
	logger, hook := test.NewNullLogger()
	db, err := gorm.Open(sqlite.Open(uniqueMemDSN("file:chat_token_gate")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&channel.ChatConnectorInstallation{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	row := channel.ChatConnectorInstallation{
		TenantID: "tenant-1", WorkspaceID: "workspace-1", Platform: "memex", InstallationID: "inst-1",
		ConversationPlatformID: "group:5", ConversationKind: "group", ConversationIsGroup: true,
		UpdatesEndpoint: "http://127.0.0.1:1/bot", TokenEnvVar: "SERVIFY_TEST_MISSING_TOKEN",
		LongPollMS: 1000, Enabled: true,
	}
	if err := row.Validate(); err != nil {
		t.Fatalf("seed row invalid: %v", err)
	}
	if err := db.Create(&row).Error; err != nil {
		t.Fatalf("seed row: %v", err)
	}
	// the env var must be absent (empty counts as unset in the gate)
	t.Setenv("SERVIFY_TEST_MISSING_TOKEN", "")

	rt := &Runtime{Config: config.GetDefaultConfig(), Logger: logger, DB: db}
	wireChatConnectors(rt, nil)
	sup := rt.ChatConnectorsForWorker()
	if sup == nil {
		t.Fatal("supervisor must be wired")
	}
	if err := sup.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v (a token-less row skips only itself)", err)
	}
	entry := waitForLogEntry(t, hook, "chat connector supervisor: build runner failed")
	if entry == nil {
		t.Fatal("missing build-failure log for the token-less row")
	}
	stopCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := sup.Stop(stopCtx); err != nil {
		t.Fatalf("Stop: %v", err)
	}
}

func waitForLogEntry(t *testing.T, hook *test.Hook, message string) *logrus.Entry {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		for _, e := range hook.AllEntries() {
			if e.Message == message {
				return e
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	return nil
}

func TestChatConnectorEndToEndLoop(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(uniqueMemDSN("file:chat_e2e")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(
		&models.User{}, &models.Session{}, &models.Message{},
		&channel.ChatConnectorInstallation{}, &channel.ChatConnectorInbox{},
		&channel.ChatConnectorCapability{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	// a scripted memex platform: one update on the first pull, then drained
	var pulled atomic.Bool
	mux := http.NewServeMux()
	mux.HandleFunc("/bot/updates", func(w http.ResponseWriter, r *http.Request) {
		if pulled.CompareAndSwap(false, true) {
			_, _ = io.WriteString(w, `{"ok":true,"updates":[
				{"msg_id":"m1","from":"alice","to":"bot:echo","text":"hello chat","ts_ms":1697000000000}]}`)
			return
		}
		_, _ = io.WriteString(w, `{"ok":true,"updates":[]}`)
	})
	mux.HandleFunc("/bot/ack", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		_, _ = io.WriteString(w, `{"ok":true}`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	// the operator's two fail-closed gates: an enabled row and a recorded
	// capability grant for its scope
	row := channel.ChatConnectorInstallation{
		TenantID: "tenant-1", WorkspaceID: "workspace-1", Platform: "memex", InstallationID: "inst-1",
		ConversationPlatformID: "group:5", ConversationKind: "group", ConversationIsGroup: true,
		UpdatesEndpoint: srv.URL, TokenEnvVar: "SERVIFY_TEST_CHAT_E2E_TOKEN",
		LongPollMS: 50, Enabled: true,
	}
	if err := db.Create(&row).Error; err != nil {
		t.Fatalf("seed row: %v", err)
	}
	if err := channel.NewCapabilityStore(db).Record(context.Background(), channel.CapabilityScope{
		TenantID: "tenant-1", WorkspaceID: "workspace-1", Platform: "memex",
		InstallationID: "inst-1", ConversationPlatformID: "group:5",
	}, memex.DeclaredCapabilities(time.Now())); err != nil {
		t.Fatalf("record capabilities: %v", err)
	}
	t.Setenv("SERVIFY_TEST_CHAT_E2E_TOKEN", "tok")
	t.Setenv(payloadKeyEnv, hex.EncodeToString(bytes.Repeat([]byte{3}, 32)))
	t.Setenv(payloadDirEnv, t.TempDir())

	rt := &Runtime{Config: config.GetDefaultConfig(), DB: db}
	wireChatConnectors(rt, conversationapp.NewService(conversationinfra.NewGormRepository(db), nil))
	sup := rt.ChatConnectorsForWorker()
	if sup == nil {
		t.Fatal("supervisor must be wired")
	}
	if err := sup.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := sup.Stop(stopCtx); err != nil {
			t.Fatalf("Stop: %v", err)
		}
	}()

	// the message must land in the conversation module exactly once
	deadline := time.Now().Add(3 * time.Second)
	var msgs []models.Message
	for time.Now().Before(deadline) {
		if err := db.Find(&msgs).Error; err != nil {
			t.Fatalf("list messages: %v", err)
		}
		if len(msgs) >= 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(msgs) != 1 {
		t.Fatalf("messages = %d, want 1 (end-to-end forward)", len(msgs))
	}
	if msgs[0].Content != "hello chat" || msgs[0].TenantID != "tenant-1" {
		t.Fatalf("message drifted: %+v", msgs[0])
	}
	var sessions []models.Session
	if err := db.Find(&sessions).Error; err != nil {
		t.Fatalf("list sessions: %v", err)
	}
	if len(sessions) != 1 || sessions[0].TenantID != "tenant-1" ||
		sessions[0].WorkspaceID != "workspace-1" || sessions[0].Platform != "memex" {
		t.Fatalf("session drifted: %+v", sessions)
	}
	var inboxRows []channel.ChatConnectorInbox
	if err := db.Find(&inboxRows).Error; err != nil {
		t.Fatalf("list inbox: %v", err)
	}
	if len(inboxRows) != 1 || inboxRows[0].Status != channel.InboxStatusCommitted {
		t.Fatalf("inbox rows drifted: %+v", inboxRows)
	}
	if inboxRows[0].RawPayloadStore != channel.LocalPayloadStoreName || inboxRows[0].RawPayloadObjectID == "" {
		t.Fatalf("retained ref drifted: %+v", inboxRows)
	}
	if !pulled.Load() {
		t.Fatal("platform was never pulled")
	}
}

// TestChatConnectorTelegramEndToEndLoop walks the Phase 3 pilot end to end:
// Bot API long poll → offset cursor → inbox commit → conversation ingest. It
// doubles as the adapter-selection proof: the same installation table drives
// both platform factories off the row's platform key.
func TestChatConnectorTelegramEndToEndLoop(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(uniqueMemDSN("file:chat_tg_e2e")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(
		&models.User{}, &models.Session{}, &models.Message{},
		&channel.ChatConnectorInstallation{}, &channel.ChatConnectorInbox{},
		&channel.ChatConnectorCapability{}, &channel.ChatConnectorCursor{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	// the update bytes exactly as the Bot API sends them — the retention copy
	rawUpdate := `{"update_id":41,"message":{"message_id":7,"chat":{"id":-100123,"type":"supergroup","title":"ops"},"from":{"id":9001,"username":"alice"},"text":"hello tg","date":1697000000}}`
	var pulled atomic.Bool
	mux := http.NewServeMux()
	mux.HandleFunc("/bottok/getUpdates", func(w http.ResponseWriter, r *http.Request) {
		if pulled.CompareAndSwap(false, true) {
			_, _ = io.WriteString(w, `{"ok":true,"result":[`+rawUpdate+`]}`)
			return
		}
		_, _ = io.WriteString(w, `{"ok":true,"result":null}`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	row := channel.ChatConnectorInstallation{
		TenantID: "tenant-1", WorkspaceID: "workspace-1", Platform: "telegram", InstallationID: "inst-tg-1",
		ConversationPlatformID: "-100123", ConversationKind: "supergroup", ConversationIsGroup: true,
		UpdatesEndpoint: srv.URL, TokenEnvVar: "SERVIFY_TEST_CHAT_TG_TOKEN",
		LongPollMS: 50, Enabled: true,
	}
	if err := db.Create(&row).Error; err != nil {
		t.Fatalf("seed row: %v", err)
	}
	if err := channel.NewCapabilityStore(db).Record(context.Background(), channel.CapabilityScope{
		TenantID: "tenant-1", WorkspaceID: "workspace-1", Platform: "telegram",
		InstallationID: "inst-tg-1", ConversationPlatformID: "-100123",
	}, telegram.DeclaredCapabilities(time.Now())); err != nil {
		t.Fatalf("record capabilities: %v", err)
	}
	t.Setenv("SERVIFY_TEST_CHAT_TG_TOKEN", "tok")
	// retention on for this loop: the env-built store reaches the adapters
	t.Setenv(payloadKeyEnv, hex.EncodeToString(bytes.Repeat([]byte{4}, 32)))
	t.Setenv(payloadDirEnv, t.TempDir())

	rt := &Runtime{Config: config.GetDefaultConfig(), DB: db}
	wireChatConnectors(rt, conversationapp.NewService(conversationinfra.NewGormRepository(db), nil))
	sup := rt.ChatConnectorsForWorker()
	if sup == nil {
		t.Fatal("supervisor must be wired")
	}
	if err := sup.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := sup.Stop(stopCtx); err != nil {
			t.Fatalf("Stop: %v", err)
		}
	}()

	deadline := time.Now().Add(3 * time.Second)
	var msgs []models.Message
	for time.Now().Before(deadline) {
		if err := db.Find(&msgs).Error; err != nil {
			t.Fatalf("list messages: %v", err)
		}
		if len(msgs) >= 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(msgs) != 1 {
		t.Fatalf("messages = %d, want 1 (telegram end-to-end forward)", len(msgs))
	}
	if msgs[0].Content != "hello tg" || msgs[0].TenantID != "tenant-1" {
		t.Fatalf("message drifted: %+v", msgs[0])
	}
	var sessions []models.Session
	if err := db.Find(&sessions).Error; err != nil {
		t.Fatalf("list sessions: %v", err)
	}
	if len(sessions) != 1 || sessions[0].Platform != "telegram" ||
		sessions[0].TenantID != "tenant-1" || sessions[0].WorkspaceID != "workspace-1" {
		t.Fatalf("session drifted: %+v", sessions)
	}
	// The runner advances the offset cursor only after the forward settles
	// (settleForwarded, then advance), so the cursor row legitimately lags the
	// message row by whatever the trailing writes take — the loop above exits the
	// moment the message is visible. On a loaded runner that gap outran a single
	// immediate read (run 39407482038 attempt 1: message and inbox rows landed,
	// cursor table empty at read time; the cursor INSERT died later as the
	// deferred Stop canceled the runner while it was still in flight). Wait for
	// the cursor the way the message is waited for; a missing cursor still fails,
	// only with the at-least-once window the contract allows.
	cursorDeadline := time.Now().Add(3 * time.Second)
	var cursors []channel.ChatConnectorCursor
	for time.Now().Before(cursorDeadline) {
		cursors = nil
		if err := db.Find(&cursors).Error; err != nil {
			t.Fatalf("list cursors: %v", err)
		}
		if len(cursors) == 1 && cursors[0].Cursor == "42" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(cursors) != 1 || cursors[0].Cursor != "42" {
		t.Fatalf("cursor drifted: %+v (want 42 = last update_id + 1)", cursors)
	}
	var inboxRows []channel.ChatConnectorInbox
	if err := db.Find(&inboxRows).Error; err != nil {
		t.Fatalf("list inbox: %v", err)
	}
	if len(inboxRows) != 1 || inboxRows[0].Status != channel.InboxStatusCommitted ||
		inboxRows[0].Platform != "telegram" || inboxRows[0].ConversationPlatformID != "-100123" {
		t.Fatalf("inbox rows drifted: %+v", inboxRows)
	}
	// the retained copy: row ref + exact platform bytes through the store
	if rt.ChatPayloadStoreForWorker() == nil {
		t.Fatal("wireChatConnectors must expose the env-built retention store")
	}
	if inboxRows[0].RawPayloadStore != channel.LocalPayloadStoreName || inboxRows[0].RawPayloadObjectID == "" {
		t.Fatalf("retained ref drifted: %+v", inboxRows)
	}
	sum := sha256.Sum256([]byte(rawUpdate))
	ref := channel.RawPayloadRef{Store: inboxRows[0].RawPayloadStore,
		ObjectID: inboxRows[0].RawPayloadObjectID, SHA256: hex.EncodeToString(sum[:])}
	got, err := rt.ChatPayloadStoreForWorker().Open(context.Background(), ref)
	if err != nil || string(got) != rawUpdate {
		t.Fatalf("open retained = %q (%v), want the exact platform bytes", got, err)
	}
	if !pulled.Load() {
		t.Fatal("platform was never pulled")
	}
}

// TestChatPayloadStoreFromEnv pins the retention knobs: an unset key keeps
// retention off, a valid key builds the store (default or explicit directory),
// and an invalid key disables retention loudly without panicking.
func TestChatPayloadStoreFromEnv(t *testing.T) {
	validKey := hex.EncodeToString(bytes.Repeat([]byte{9}, 32))

	if got := chatPayloadStoreFromEnv(func(string) string { return "" }, nil); got != nil {
		t.Error("unset key must keep retention off")
	}
	if got := chatPayloadStoreFromEnv(func(string) string { return "   " }, nil); got != nil {
		t.Error("blank key must keep retention off")
	}
	if got := chatPayloadStoreFromEnv(func(k string) string {
		if k == payloadKeyEnv {
			return validKey
		}
		return ""
	}, nil); got == nil {
		t.Fatal("valid key must build the store with the default directory")
	}
	if got := chatPayloadStoreFromEnv(func(k string) string {
		switch k {
		case payloadKeyEnv:
			return validKey
		case payloadDirEnv:
			return t.TempDir()
		}
		return ""
	}, nil); got == nil {
		t.Fatal("valid key + explicit directory must build the store")
	}

	logger, hook := test.NewNullLogger()
	if got := chatPayloadStoreFromEnv(func(string) string { return "zz-not-hex" }, logger); got != nil {
		t.Error("non-hex key must disable retention")
	}
	warned := false
	for _, e := range hook.AllEntries() {
		if e.Level == logrus.WarnLevel && strings.Contains(e.Message, payloadKeyEnv) {
			warned = true
		}
	}
	if !warned {
		t.Error("invalid key must warn")
	}

	logger2, hook2 := test.NewNullLogger()
	shortKey := hex.EncodeToString(bytes.Repeat([]byte{1}, 16)) // AES-256 wants 32 bytes
	if got := chatPayloadStoreFromEnv(func(string) string { return shortKey }, logger2); got != nil {
		t.Error("wrong-length key must disable retention")
	}
	if len(hook2.AllEntries()) == 0 {
		t.Error("constructor rejection must warn")
	}
}

// TestChatConnectorUnsupportedPlatformFailsOwnRow proves the adapter switch has
// no silent fallback: an unknown platform key fails only that installation's
// attachment and cannot shadow a healthy row.
func TestChatConnectorUnsupportedPlatformFailsOwnRow(t *testing.T) {
	logger, hook := test.NewNullLogger()
	db, err := gorm.Open(sqlite.Open(uniqueMemDSN("file:chat_bad_platform")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&channel.ChatConnectorInstallation{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	rows := []channel.ChatConnectorInstallation{
		{TenantID: "tenant-1", WorkspaceID: "workspace-1", Platform: "memex", InstallationID: "inst-memex",
			ConversationPlatformID: "group:5", ConversationKind: "group", ConversationIsGroup: true,
			UpdatesEndpoint: "http://127.0.0.1:1/bot", TokenEnvVar: "SERVIFY_TEST_CHAT_MEMEX_TOKEN",
			LongPollMS: 1000, Enabled: true},
		{TenantID: "tenant-1", WorkspaceID: "workspace-1", Platform: "slack", InstallationID: "inst-slack",
			ConversationPlatformID: "C123", ConversationKind: "channel", ConversationIsGroup: true,
			UpdatesEndpoint: "http://127.0.0.1:1/bot", TokenEnvVar: "SERVIFY_TEST_CHAT_SLACK_TOKEN",
			LongPollMS: 1000, Enabled: true},
	}
	for i := range rows {
		if err := rows[i].Validate(); err != nil {
			t.Fatalf("seed row invalid: %v", err)
		}
		if err := db.Create(&rows[i]).Error; err != nil {
			t.Fatalf("seed row: %v", err)
		}
	}
	t.Setenv("SERVIFY_TEST_CHAT_MEMEX_TOKEN", "tok")
	t.Setenv("SERVIFY_TEST_CHAT_SLACK_TOKEN", "tok")

	rt := &Runtime{Config: config.GetDefaultConfig(), Logger: logger, DB: db}
	wireChatConnectors(rt, nil)
	sup := rt.ChatConnectorsForWorker()
	if sup == nil {
		t.Fatal("supervisor must be wired")
	}
	if err := sup.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v (only the unsupported row may fail)", err)
	}
	defer func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = sup.Stop(stopCtx)
	}()
	if entry := waitForLogEntry(t, hook, "chat connector supervisor: build runner failed"); entry == nil {
		t.Fatal("missing build-failure log for the unsupported platform row")
	}
}

func TestChatConnectorOpsRoutesRegistered(t *testing.T) {
	cfg := config.GetDefaultConfig()
	want := []string{
		"GET /api/chat_connectors/runners",
		"GET /api/chat_connectors/quarantine",
		"GET /api/chat_connectors/quarantine/export",
		"POST /api/chat_connectors/quarantine/:id/dismiss",
		"POST /api/chat_connectors/reload",
		"POST /api/chat_connectors/installations",
		"POST /api/chat_connectors/capabilities",
		"POST /api/chat_connectors/notify",
	}
	// 已装配：监督器注入后路由全量在册
	router := BuildRouter(Dependencies{Config: cfg, ChatConnectors: chatconnector.NewSupervisor(nil, nil, nil)})
	registered := map[string]bool{}
	for _, ri := range router.Routes() {
		registered[ri.Method+" "+ri.Path] = true
	}
	for _, path := range want {
		if !registered[path] {
			t.Fatalf("route %s missing", path)
		}
	}

	// 未装配（无 DB、无监督器）：路由仍在册（请求期返回 404），装配不 panic
	bare := BuildRouter(Dependencies{Config: cfg})
	bareRegistered := map[string]bool{}
	for _, ri := range bare.Routes() {
		bareRegistered[ri.Method+" "+ri.Path] = true
	}
	for _, path := range want {
		if !bareRegistered[path] {
			t.Fatalf("route %s missing on bare runtime", path)
		}
	}
}

// newChatNotifierRuntime 装配一个带单条 memex/指定平台安装行的 runtime（供
// notify dispatcher 测试）。caps=true 时按行的会话映射记录 SendText 授权；
// enabled=false 时落一个关停安装行（默认安全态）；endpoint 是假平台基址；
// tokenEnv 是行引用的环境变量名。
func newChatNotifierRuntime(t *testing.T, endpoint, platform, tokenEnv string, caps, enabled bool) *Runtime {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(uniqueMemDSN("file:chat_notifier")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&channel.ChatConnectorInstallation{}, &channel.ChatConnectorCapability{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	row := channel.ChatConnectorInstallation{
		TenantID: "tenant-1", WorkspaceID: "workspace-1", Platform: platform, InstallationID: "inst-1",
		ConversationPlatformID: "group:5", ConversationKind: "group", ConversationIsGroup: true,
		UpdatesEndpoint: endpoint, TokenEnvVar: tokenEnv, LongPollMS: 50, Enabled: enabled,
	}
	if err := db.Create(&row).Error; err != nil {
		t.Fatalf("seed row: %v", err)
	}
	if caps {
		if err := channel.NewCapabilityStore(db).Record(context.Background(), channel.CapabilityScope{
			TenantID: "tenant-1", WorkspaceID: "workspace-1", Platform: platform,
			InstallationID: "inst-1", ConversationPlatformID: "group:5",
		}, memex.DeclaredCapabilities(time.Now())); err != nil {
			t.Fatalf("record capabilities: %v", err)
		}
	}
	rt := &Runtime{Config: config.GetDefaultConfig(), DB: db}
	wireChatConnectors(rt, nil)
	if rt.chatNotifier == nil {
		t.Fatal("wireChatConnectors must build the notifier")
	}
	return rt
}

func TestChatConnectorNotifierDispatchesAndPinsTarget(t *testing.T) {
	var received atomic.Value // last /bot/send request body
	mux := http.NewServeMux()
	mux.HandleFunc("/bot/send", func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		received.Store(string(b))
		_, _ = io.WriteString(w, `{"ok":true,"msg_id":"mid-1"}`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	tokenEnv := "SERVIFY_TEST_NOTIFY_TOKEN"
	rt := newChatNotifierRuntime(t, srv.URL, "memex", tokenEnv, true, true)
	t.Setenv(tokenEnv, "tok")

	// 请求体绝不携带收件人：dispatcher 用行映射的 group:5 覆盖任何传入 target
	res, err := rt.chatNotifier.Notify(context.Background(), "tenant-1", "workspace-1", "memex", "inst-1", channel.NotifyRequest{
		Target:         "group:999",
		Reason:         "maintenance",
		IdempotencyKey: "op-1",
		Content:        channel.SendContent{Text: "system going down"},
	})
	if err != nil {
		t.Fatalf("Notify: %v", err)
	}
	if res.Status != channel.SendResultAccepted || res.PlatformMessageID != "mid-1" || res.AcceptedAt.IsZero() {
		t.Fatalf("result = %+v, want accepted/mid-1 with accepted_at", res)
	}
	body, _ := received.Load().(string)
	var sent struct {
		Target string `json:"target"`
		Text   string `json:"text"`
	}
	if err := json.Unmarshal([]byte(body), &sent); err != nil {
		t.Fatalf("decode send body %q: %v", body, err)
	}
	if sent.Target != "group:5" {
		t.Fatalf("target = %q, want row-pinned group:5 (body %q)", sent.Target, body)
	}
	if sent.Text != "system going down" {
		t.Fatalf("text = %q, want passthrough", sent.Text)
	}
}

func TestChatConnectorNotifierErrors(t *testing.T) {
	cases := []struct {
		name      string
		platform  string
		caps      bool
		setToken  bool
		enabled   bool
		notifyID  string
		wantCheck func(t *testing.T, err error)
	}{
		{"installation missing", "memex", true, true, true, "inst-missing", func(t *testing.T, err error) {
			if !errors.Is(err, channel.ErrNotifierInstallationNotFound) {
				t.Fatalf("err = %v, want ErrNotifierInstallationNotFound", err)
			}
		}},
		{"installation disabled", "memex", true, true, false, "inst-1", func(t *testing.T, err error) {
			if !errors.Is(err, channel.ErrNotifierDisabled) {
				t.Fatalf("err = %v, want ErrNotifierDisabled", err)
			}
		}},
		{"token env unset", "memex", true, false, true, "inst-1", func(t *testing.T, err error) {
			if !strings.Contains(err.Error(), "token env var") {
				t.Fatalf("err = %v, want token env failure", err)
			}
		}},
		{"no adapter for platform", "slack", true, true, true, "inst-1", func(t *testing.T, err error) {
			if !strings.Contains(err.Error(), `platform "slack" has no adapter`) {
				t.Fatalf("err = %v, want no-adapter failure", err)
			}
		}},
		{"send text not granted", "memex", false, true, true, "inst-1", func(t *testing.T, err error) {
			if !errors.Is(err, channel.ErrSendDisabled) {
				t.Fatalf("err = %v, want ErrSendDisabled", err)
			}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tokenEnv := "SERVIFY_TEST_NOTIFY_ERR_TOKEN"
			rt := newChatNotifierRuntime(t, "http://127.0.0.1:1", tc.platform, tokenEnv, tc.caps, tc.enabled)
			if tc.setToken {
				t.Setenv(tokenEnv, "tok")
			} else {
				t.Setenv(tokenEnv, "")
			}
			_, err := rt.chatNotifier.Notify(context.Background(), "tenant-1", "workspace-1", tc.platform, tc.notifyID, channel.NotifyRequest{
				Reason:         "maintenance",
				IdempotencyKey: "op-1",
				Content:        channel.SendContent{Text: "system going down"},
			})
			if err == nil {
				t.Fatal("Notify must fail")
			}
			tc.wantCheck(t, err)
		})
	}
}
