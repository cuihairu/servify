package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"servify/apps/server/internal/models"
	conversationapp "servify/apps/server/internal/modules/conversation/application"
	conversationinfra "servify/apps/server/internal/modules/conversation/infra"
	"servify/apps/server/internal/platform/channel"
	"servify/apps/server/internal/platform/channel/memex"
	"servify/apps/server/internal/platform/channel/telegram"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

// replayHarness is the minimal wiring the replay slice exercises: real
// conversation module, real inbox/installation stores, a real payload store
// over a temp root, and the bridge whose forward the replayer shares.
type replayHarness struct {
	db       *gorm.DB
	inbox    *channel.InboxStore
	installs *channel.InstallationStore
	payloads *channel.LocalPayloadStore
	bridge   *chatConversationBridge
	replayer *ChatInboxReplayer
	root     string
}

func newReplayHarness(t *testing.T, seedInstallation bool) *replayHarness {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(uniqueMemDSN("file:chat_replay")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(
		&models.User{}, &models.Session{}, &models.Message{},
		&channel.ChatConnectorInbox{}, &channel.ChatConnectorInstallation{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	installs := channel.NewInstallationStore(db)
	if seedInstallation {
		if err := db.Create(&channel.ChatConnectorInstallation{
			TenantID: "tenant-1", WorkspaceID: "workspace-1", Platform: "telegram", InstallationID: "inst-tg-1",
			ConversationPlatformID: "-100123", ConversationKind: "supergroup", ConversationIsGroup: true,
			UpdatesEndpoint: "https://api.example.test", TokenEnvVar: "SERVIFY_TEST_REPLAY_TOKEN",
			LongPollMS: 50, Enabled: true,
		}).Error; err != nil {
			t.Fatalf("seed telegram row: %v", err)
		}
	}
	payloadRoot := t.TempDir()
	payloads, err := channel.NewLocalPayloadStore(payloadRoot, bytes.Repeat([]byte{9}, 32), 0)
	if err != nil {
		t.Fatalf("payload store: %v", err)
	}
	inbox := channel.NewInboxStore(db)
	bridge := &chatConversationBridge{
		ingestor: conversationapp.NewService(conversationinfra.NewGormRepository(db), nil),
		inbox:    inbox,
	}
	return &replayHarness{
		db:       db,
		inbox:    inbox,
		installs: installs,
		payloads: payloads,
		bridge:   bridge,
		replayer: &ChatInboxReplayer{inbox: inbox, installs: installs, payloads: payloads, forward: bridge.forward},
		root:     payloadRoot,
	}
}

// acceptTelegramEvent translates raw update bytes with the seeded
// installation's scope, retains the bytes, and commits the event — the exact
// first half of the live pipeline, leaving the row in the replay lane.
func (h *replayHarness) acceptTelegramEvent(t *testing.T, raw []byte) (channel.RawPayloadRef, channel.ConnectorEvent) {
	t.Helper()
	ctx := context.Background()
	var stored channel.ChatConnectorInstallation
	if err := h.db.Where("installation_id = ?", "inst-tg-1").First(&stored).Error; err != nil {
		t.Fatalf("load installation: %v", err)
	}
	var u telegram.Update
	if err := json.Unmarshal(raw, &u); err != nil {
		t.Fatalf("decode fixture: %v", err)
	}
	ev := telegram.Translator{Config: telegramConfigFromInstallation(stored)}.Event(time.Now().UTC(), u)
	ref, err := h.payloads.Save(ctx, channel.PayloadMeta{
		TenantID: ev.TenantID, WorkspaceID: ev.WorkspaceID,
		Platform: ev.Platform, InstallationID: ev.InstallationID, EventID: ev.EventID,
	}, raw)
	if err != nil {
		t.Fatalf("retain payload: %v", err)
	}
	ev.RawPayloadRef = &ref
	if _, err := h.inbox.Accept(ctx, ev); err != nil {
		t.Fatalf("accept: %v", err)
	}
	return ref, ev
}

func TestChatInboxReplayTelegramIntoNormalFlow(t *testing.T) {
	h := newReplayHarness(t, true)
	raw := []byte(`{"update_id":41,"message":{"message_id":7,"chat":{"id":-100123,"type":"supergroup","title":"ops"},"from":{"id":9001,"username":"alice"},"text":"replay me","date":1697000000}}`)
	ref, ev := h.acceptTelegramEvent(t, raw)

	lane, err := h.replayer.ListUnforwarded(context.Background(), "tenant-1", "workspace-1", 0)
	if err != nil {
		t.Fatalf("list lane: %v", err)
	}
	if len(lane) != 1 || lane[0].IdempotencyKey != ev.IdempotencyKey {
		t.Fatalf("lane = %+v, want the committed row", lane)
	}
	if lane[0].RawPayloadSHA256 == "" {
		t.Fatal("row must carry the plaintext digest for replay integrity")
	}
	if err := h.replayer.ReplayInboxRow(context.Background(), "tenant-1", "workspace-1", lane[0].ID); err != nil {
		t.Fatalf("replay: %v", err)
	}

	var msgs []models.Message
	if err := h.db.Find(&msgs).Error; err != nil {
		t.Fatalf("list messages: %v", err)
	}
	if len(msgs) != 1 || msgs[0].Content != "replay me" {
		t.Fatalf("messages = %+v, want exactly the replayed text", msgs)
	}
	row := h.inboxRowByRef(t, ref)
	if row.ForwardedAt == nil {
		t.Fatal("replayed row must be stamped forwarded")
	}
	if again, err := h.replayer.ListUnforwarded(context.Background(), "tenant-1", "workspace-1", 0); err != nil || len(again) != 0 {
		t.Fatalf("lane after replay = %v / %v, want empty", again, err)
	}
}

func TestChatInboxReplayMemexIntoNormalFlow(t *testing.T) {
	h := newReplayHarness(t, true)
	// memex rows pin the conversation in the installation row; seed one and
	// commit through the memex translator to exercise its replay arm
	if err := h.db.Model(&channel.ChatConnectorInstallation{}).
		Where("installation_id = ?", "inst-tg-1").
		Updates(map[string]interface{}{"platform": "memex"}).Error; err != nil {
		t.Fatalf("retarget platform: %v", err)
	}
	raw := []byte(`{"msg_id":"m1","from":"alice","to":"bot:echo","text":"hello replay","ts_ms":1697000000000}`)
	ctx := context.Background()
	var stored channel.ChatConnectorInstallation
	if err := h.db.Where("installation_id = ?", "inst-tg-1").First(&stored).Error; err != nil {
		t.Fatalf("load installation: %v", err)
	}
	var u memex.Update
	if err := json.Unmarshal(raw, &u); err != nil {
		t.Fatalf("decode fixture: %v", err)
	}
	ev := memex.Translator{Config: memexConfigFromInstallation(stored)}.Event(time.Now().UTC(), u)
	ref, err := h.payloads.Save(ctx, channel.PayloadMeta{
		TenantID: ev.TenantID, WorkspaceID: ev.WorkspaceID,
		Platform: ev.Platform, InstallationID: ev.InstallationID, EventID: ev.EventID,
	}, raw)
	if err != nil {
		t.Fatalf("retain payload: %v", err)
	}
	ev.RawPayloadRef = &ref
	if _, err := h.inbox.Accept(ctx, ev); err != nil {
		t.Fatalf("accept: %v", err)
	}
	lane, err := h.replayer.ListUnforwarded(ctx, "tenant-1", "workspace-1", 0)
	if err != nil || len(lane) != 1 {
		t.Fatalf("lane = %v / %v, want the memex row", lane, err)
	}
	if err := h.replayer.ReplayInboxRow(ctx, "tenant-1", "workspace-1", lane[0].ID); err != nil {
		t.Fatalf("replay: %v", err)
	}
	var msgs []models.Message
	if err := h.db.Find(&msgs).Error; err != nil {
		t.Fatalf("list messages: %v", err)
	}
	if len(msgs) != 1 || msgs[0].Content != "hello replay" {
		t.Fatalf("messages = %+v, want the memex replay text", msgs)
	}
}

func TestChatInboxReplayRefusals(t *testing.T) {
	ctx := context.Background()
	build := func(t *testing.T, seed bool) *replayHarness { return newReplayHarness(t, seed) }
	raw := []byte(`{"update_id":41,"message":{"message_id":7,"chat":{"id":-100123,"type":"supergroup","title":"ops"},"from":{"id":9001,"username":"alice"},"text":"replay me","date":1697000000}}`)

	t.Run("row without retained payload", func(t *testing.T) {
		h := build(t, true)
		ev, err := replayableEvent("telegram", "inst-tg-1")
		if err != nil {
			t.Fatalf("build event: %v", err)
		}
		if _, err := h.inbox.Accept(ctx, ev); err != nil {
			t.Fatalf("accept: %v", err)
		}
		row := h.laneRow(t)
		if err := h.replayer.ReplayInboxRow(ctx, "tenant-1", "workspace-1", row.ID); !errors.Is(err, channel.ErrReplayPayloadUnavailable) {
			t.Fatalf("replay without ref = %v, want ErrReplayPayloadUnavailable", err)
		}
	})

	t.Run("payload deleted", func(t *testing.T) {
		h := build(t, true)
		ref, _ := h.acceptTelegramEvent(t, raw)
		if err := h.payloads.Delete(ctx, ref); err != nil {
			t.Fatalf("delete payload: %v", err)
		}
		row := h.laneRow(t)
		if err := h.replayer.ReplayInboxRow(ctx, "tenant-1", "workspace-1", row.ID); !errors.Is(err, channel.ErrReplayPayloadUnavailable) {
			t.Fatalf("replay deleted = %v, want ErrReplayPayloadUnavailable", err)
		}
	})

	t.Run("payload corrupt", func(t *testing.T) {
		h := build(t, true)
		ref, _ := h.acceptTelegramEvent(t, raw)
		object := filepath.Join(h.root, ref.ObjectID)
		blob, err := os.ReadFile(object)
		if err != nil {
			t.Fatalf("read object: %v", err)
		}
		blob[0] ^= 0xff
		if err := os.WriteFile(object, blob, 0o600); err != nil {
			t.Fatalf("tamper object: %v", err)
		}
		row := h.laneRow(t)
		if err := h.replayer.ReplayInboxRow(ctx, "tenant-1", "workspace-1", row.ID); !errors.Is(err, channel.ErrReplayPayloadUnavailable) {
			t.Fatalf("replay corrupt = %v, want ErrReplayPayloadUnavailable", err)
		}
	})

	t.Run("installation gone", func(t *testing.T) {
		h := build(t, false)
		h.acceptTelegramEventRequiresConfig(t, raw)
		row := h.laneRow(t)
		if err := h.replayer.ReplayInboxRow(ctx, "tenant-1", "workspace-1", row.ID); !errors.Is(err, channel.ErrReplaySourceGone) {
			t.Fatalf("replay without installation = %v, want ErrReplaySourceGone", err)
		}
	})

	t.Run("payload no longer decodes", func(t *testing.T) {
		h := build(t, true)
		// commit from valid bytes but keep bytes under the ref that no longer
		// parse as the row's platform (simulated post-accept drift)
		ref, _ := h.acceptTelegramEvent(t, raw)
		if err := h.payloads.Delete(ctx, ref); err != nil {
			t.Fatalf("reset payload: %v", err)
		}
		drifted, err := h.payloads.Save(ctx, channel.PayloadMeta{Platform: "telegram"}, []byte(`{"update_id":"not-a-number"}`))
		if err != nil {
			t.Fatalf("store drifted payload: %v", err)
		}
		if err := h.db.Model(&channel.ChatConnectorInbox{}).
			Where("id = ?", h.laneRow(t).ID).
			Updates(map[string]interface{}{
				"raw_payload_object_id": drifted.ObjectID,
				"raw_payload_sha256":    drifted.SHA256,
			}).Error; err != nil {
			t.Fatalf("point row at drifted payload: %v", err)
		}
		if err := h.replayer.ReplayInboxRow(ctx, "tenant-1", "workspace-1", h.laneRow(t).ID); !errors.Is(err, channel.ErrReplayMalformed) {
			t.Fatalf("replay drifted = %v, want ErrReplayMalformed", err)
		}
	})

	t.Run("payload does not belong to row", func(t *testing.T) {
		h := build(t, true)
		// commit event A's row while pointing its ref at event B's object:
		// the digest still matches (both were stored through Save), so the
		// integrity check passes and only the row-versus-event comparison
		// catches the swap
		ref, err := h.payloads.Save(ctx, channel.PayloadMeta{Platform: "telegram"}, []byte(`{"update_id":42,"message":{"message_id":8,"chat":{"id":-100123,"type":"supergroup"},"from":{"id":9001},"text":"other event","date":1697000001}}`))
		if err != nil {
			t.Fatalf("store foreign payload: %v", err)
		}
		ev, err := replayableEvent("telegram", "inst-tg-1")
		if err != nil {
			t.Fatalf("build event: %v", err)
		}
		ev.RawPayloadRef = &ref
		if _, err := h.inbox.Accept(ctx, ev); err != nil {
			t.Fatalf("accept: %v", err)
		}
		row := h.laneRow(t)
		if err := h.replayer.ReplayInboxRow(ctx, "tenant-1", "workspace-1", row.ID); !errors.Is(err, channel.ErrReplayMismatch) {
			t.Fatalf("replay swapped = %v, want ErrReplayMismatch", err)
		}
	})

	t.Run("unsupported platform", func(t *testing.T) {
		h := build(t, true)
		if err := h.db.Model(&channel.ChatConnectorInstallation{}).
			Where("installation_id = ?", "inst-tg-1").
			Updates(map[string]interface{}{"platform": "weird"}).Error; err != nil {
			t.Fatalf("retarget platform: %v", err)
		}
		ev, err := replayableEvent("weird", "inst-tg-1")
		if err != nil {
			t.Fatalf("build event: %v", err)
		}
		ref, err := h.payloads.Save(ctx, channel.PayloadMeta{Platform: "weird"}, raw)
		if err != nil {
			t.Fatalf("retain payload: %v", err)
		}
		ev.RawPayloadRef = &ref
		if _, err := h.inbox.Accept(ctx, ev); err != nil {
			t.Fatalf("accept: %v", err)
		}
		row := h.laneRow(t)
		if err := h.replayer.ReplayInboxRow(ctx, "tenant-1", "workspace-1", row.ID); !errors.Is(err, channel.ErrReplayUnsupportedPlatform) {
			t.Fatalf("replay weird platform = %v, want ErrReplayUnsupportedPlatform", err)
		}
	})

	t.Run("payload unreadable on disk", func(t *testing.T) {
		h := build(t, true)
		ref, _ := h.acceptTelegramEvent(t, raw)
		object := filepath.Join(h.root, ref.ObjectID)
		if err := os.Remove(object); err != nil {
			t.Fatalf("remove object: %v", err)
		}
		if err := os.Mkdir(object, 0o700); err != nil {
			t.Fatalf("block object path: %v", err)
		}
		row := h.laneRow(t)
		err := h.replayer.ReplayInboxRow(ctx, "tenant-1", "workspace-1", row.ID)
		// not a sentinel: the raw storage error surfaces as a fault, not a
		// lane disposition
		if err == nil || errors.Is(err, channel.ErrReplayPayloadUnavailable) {
			t.Fatalf("replay unreadable = %v, want a non-sentinel storage error", err)
		}
	})

	t.Run("installation lookup fails", func(t *testing.T) {
		h := build(t, true)
		h.acceptTelegramEvent(t, raw)
		row := h.laneRow(t)
		if err := h.db.Callback().Query().Before("gorm:query").Register("replay_install_error",
			func(tx *gorm.DB) {
				if tx.Statement != nil && tx.Statement.Table == "chat_connector_installations" {
					tx.AddError(errors.New("boom"))
				}
			}); err != nil {
			t.Fatalf("register callback: %v", err)
		}
		if err := h.replayer.ReplayInboxRow(ctx, "tenant-1", "workspace-1", row.ID); err == nil ||
			!strings.Contains(err.Error(), "boom") {
			t.Fatalf("replay with failing lookup = %v, want the storage error", err)
		}
	})

	t.Run("row timestamp corrupted to zero", func(t *testing.T) {
		h := build(t, true)
		h.acceptTelegramEvent(t, raw)
		row := h.laneRow(t)
		if err := h.db.Model(&channel.ChatConnectorInbox{}).
			Where("id = ?", row.ID).
			Update("received_at", time.Time{}).Error; err != nil {
			t.Fatalf("corrupt received_at: %v", err)
		}
		if err := h.replayer.ReplayInboxRow(ctx, "tenant-1", "workspace-1", row.ID); !errors.Is(err, channel.ErrReplayMalformed) {
			t.Fatalf("replay zero received_at = %v, want ErrReplayMalformed", err)
		}
	})

	t.Run("memex payload no longer decodes", func(t *testing.T) {
		h := build(t, true)
		if err := h.db.Model(&channel.ChatConnectorInstallation{}).
			Where("installation_id = ?", "inst-tg-1").
			Updates(map[string]interface{}{"platform": "memex"}).Error; err != nil {
			t.Fatalf("retarget platform: %v", err)
		}
		ev, err := replayableEvent("memex", "inst-tg-1")
		if err != nil {
			t.Fatalf("build event: %v", err)
		}
		ref, err := h.payloads.Save(ctx, channel.PayloadMeta{Platform: "memex"}, []byte(`{"ts_ms":"bad"}`))
		if err != nil {
			t.Fatalf("retain payload: %v", err)
		}
		ev.RawPayloadRef = &ref
		if _, err := h.inbox.Accept(ctx, ev); err != nil {
			t.Fatalf("accept: %v", err)
		}
		row := h.laneRow(t)
		if err := h.replayer.ReplayInboxRow(ctx, "tenant-1", "workspace-1", row.ID); !errors.Is(err, channel.ErrReplayMalformed) {
			t.Fatalf("replay malformed memex = %v, want ErrReplayMalformed", err)
		}
	})
}

func TestChatInboxReplayerGuards(t *testing.T) {
	ctx := context.Background()
	// unconfigured replayer: both faces refuse instead of panicking
	var nilReplayer *ChatInboxReplayer
	if _, err := nilReplayer.ListUnforwarded(ctx, "tenant-1", "workspace-1", 0); !errors.Is(err, channel.ErrReplayPayloadUnavailable) {
		t.Fatalf("nil list = %v, want ErrReplayPayloadUnavailable", err)
	}
	if err := nilReplayer.ReplayInboxRow(ctx, "tenant-1", "workspace-1", 1); !errors.Is(err, channel.ErrReplayPayloadUnavailable) {
		t.Fatalf("nil replay = %v, want ErrReplayPayloadUnavailable", err)
	}
	empty := &ChatInboxReplayer{}
	if _, err := empty.ListUnforwarded(ctx, "tenant-1", "workspace-1", 0); !errors.Is(err, channel.ErrReplayPayloadUnavailable) {
		t.Fatalf("empty list = %v, want ErrReplayPayloadUnavailable", err)
	}
	if err := empty.ReplayInboxRow(ctx, "tenant-1", "workspace-1", 1); !errors.Is(err, channel.ErrReplayPayloadUnavailable) {
		t.Fatalf("empty replay = %v, want ErrReplayPayloadUnavailable", err)
	}

	// an unknown id surfaces the store's own not-found (the ops 404)
	h := newReplayHarness(t, true)
	if err := h.replayer.ReplayInboxRow(ctx, "tenant-1", "workspace-1", 99999); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("unknown id = %v, want record not found", err)
	}
}

func TestChatBridgeForwardFailurePaths(t *testing.T) {
	ctx := context.Background()
	ev := chatEvent("memex", "inst-1", "tenant-1", "workspace-1", "group:5", "m1", "hi")

	// no ingestor wired: nothing to forward, nothing to settle
	if err := (&chatConversationBridge{}).forward(ctx, ev); err != nil {
		t.Fatalf("forward without ingestor = %v, want nil", err)
	}

	// a resume failure that is not not-found surfaces from forward (the
	// live path logs it and leaves the row to replay)
	resumeBroken := newStubChatIngestor()
	resumeBroken.resumeErr = errors.New("resume broke")
	if err := (&chatConversationBridge{ingestor: resumeBroken}).forward(ctx, ev); err == nil {
		t.Fatal("forward must surface a non-not-found resume failure")
	}

	// a create failure after a genuine not-found resume surfaces too
	createBroken := newStubChatIngestor()
	createBroken.createErr = errors.New("create broke")
	if err := (&chatConversationBridge{ingestor: createBroken}).forward(ctx, ev); err == nil {
		t.Fatal("forward must surface a create failure")
	}

	// a failing ingest with no logger still logs through the standard
	// logger instead of panicking, and the observer contract stays nil
	ingestBroken := newStubChatIngestor()
	ingestBroken.ingestErr = errors.New("store down")
	if err := (&chatConversationBridge{ingestor: ingestBroken}).InboundCommitted(ctx, ev); err != nil {
		t.Fatalf("swallowed ingest failure = %v, want nil", err)
	}
}

func TestFirstNonEmptyFallsThroughWhenAllEmpty(t *testing.T) {
	if got := firstNonEmpty("", ""); got != "" {
		t.Fatalf("firstNonEmpty all-empty = %q, want \"\"", got)
	}
}

// replayableEvent builds a valid committed-lane event by hand for refusal
// cases that do not need a full translator round trip.
func replayableEvent(platform, installationID string) (channel.ConnectorEvent, error) {
	now := time.Now().UTC()
	ev := channel.ConnectorEvent{
		Platform:       platform,
		InstallationID: installationID,
		TenantID:       "tenant-1",
		WorkspaceID:    "workspace-1",
		EventID:        "evt-replay",
		IdempotencyKey: channel.InboundIdempotencyKey(installationID, "evt-replay"),
		EventType:      channel.EventTypeMessageCreated,
		OccurredAt:     now,
		ReceivedAt:     now,
		Conversation:   channel.ConversationRef{PlatformID: "-100123", Kind: "supergroup", IsGroup: true},
		Sender:         channel.SenderRef{PlatformID: "9001"},
		Message:        &channel.MessageContent{PlatformID: "7", Kind: "text", Text: "replay me"},
	}
	return ev, ev.Validate()
}

func (h *replayHarness) laneRow(t *testing.T) channel.ChatConnectorInbox {
	t.Helper()
	lane, err := h.replayer.ListUnforwarded(context.Background(), "tenant-1", "workspace-1", 0)
	if err != nil {
		t.Fatalf("list lane: %v", err)
	}
	if len(lane) != 1 {
		t.Fatalf("lane = %d rows, want 1", len(lane))
	}
	return lane[0]
}

func (h *replayHarness) inboxRowByRef(t *testing.T, ref channel.RawPayloadRef) channel.ChatConnectorInbox {
	t.Helper()
	var row channel.ChatConnectorInbox
	if err := h.db.Where("raw_payload_object_id = ?", ref.ObjectID).First(&row).Error; err != nil {
		t.Fatalf("find row by ref: %v", err)
	}
	return row
}

func (h *replayHarness) acceptTelegramEventRequiresConfig(t *testing.T, raw []byte) {
	t.Helper()
	ctx := context.Background()
	var u telegram.Update
	if err := json.Unmarshal(raw, &u); err != nil {
		t.Fatalf("decode fixture: %v", err)
	}
	cfg := telegram.Config{
		Platform: "telegram", TenantID: "tenant-1", WorkspaceID: "workspace-1",
		InstallationID: "inst-tg-1",
		Conversation:   channel.ConversationRef{PlatformID: "-100123", Kind: "supergroup", IsGroup: true},
	}
	ev := telegram.Translator{Config: cfg}.Event(time.Now().UTC(), u)
	ref, err := h.payloads.Save(ctx, channel.PayloadMeta{
		TenantID: ev.TenantID, WorkspaceID: ev.WorkspaceID,
		Platform: ev.Platform, InstallationID: ev.InstallationID, EventID: ev.EventID,
	}, raw)
	if err != nil {
		t.Fatalf("retain payload: %v", err)
	}
	ev.RawPayloadRef = &ref
	if _, err := h.inbox.Accept(ctx, ev); err != nil {
		t.Fatalf("accept: %v", err)
	}
}

// TestRuntimeChatInboxReplayerAccessor pins the wiring accessor: an unwired
// runtime exposes nil so the replay surface stays a 404 instead of panicking.
func TestRuntimeChatInboxReplayerAccessor(t *testing.T) {
	rt := &Runtime{}
	if rt.ChatInboxReplayer() != nil {
		t.Fatal("unwired runtime must expose a nil replayer")
	}
	replayer := &ChatInboxReplayer{}
	rt.chatReplay = replayer
	if got := rt.ChatInboxReplayer(); got != replayer {
		t.Fatalf("ChatInboxReplayer = %v, want the wired instance", got)
	}
}
